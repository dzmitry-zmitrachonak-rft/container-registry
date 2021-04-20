package handlers

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"database/sql"
	"errors"
	"expvar"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/docker/distribution"
	"github.com/docker/distribution/configuration"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/health"
	"github.com/docker/distribution/health/checks"
	prometheus "github.com/docker/distribution/metrics"
	"github.com/docker/distribution/migrations"
	"github.com/docker/distribution/notifications"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/docker/distribution/registry/auth"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/gc"
	"github.com/docker/distribution/registry/gc/worker"
	"github.com/docker/distribution/registry/internal"
	registrymiddleware "github.com/docker/distribution/registry/middleware/registry"
	repositorymiddleware "github.com/docker/distribution/registry/middleware/repository"
	"github.com/docker/distribution/registry/proxy"
	"github.com/docker/distribution/registry/storage"
	memorycache "github.com/docker/distribution/registry/storage/cache/memory"
	rediscache "github.com/docker/distribution/registry/storage/cache/redis"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/driver/factory"
	storagemiddleware "github.com/docker/distribution/registry/storage/driver/middleware"
	"github.com/docker/distribution/registry/storage/validation"
	"github.com/docker/distribution/version"
	"github.com/getsentry/sentry-go"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"gitlab.com/gitlab-org/labkit/errortracking"
	metricskit "gitlab.com/gitlab-org/labkit/metrics"
	"gitlab.com/gitlab-org/labkit/metrics/sqlmetrics"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 32

// defaultCheckInterval is the default time in between health checks
const defaultCheckInterval = 10 * time.Second

// App is a global registry application object. Shared resources can be placed
// on this object that will be accessible from all requests. Any writable
// fields should be protected.
type App struct {
	context.Context

	Config *configuration.Configuration

	router            *mux.Router                    // main application router, configured with dispatchers
	driver            storagedriver.StorageDriver    // driver maintains the app global storage driver instance.
	db                *datastore.DB                  // db is the global database handle used across the app.
	registry          distribution.Namespace         // registry is the primary registry backend for the app instance.
	migrationRegistry distribution.Namespace         // migrationRegistry is the secondary registry backend for migration.
	repoRemover       distribution.RepositoryRemover // repoRemover provides ability to delete repos
	accessController  auth.AccessController          // main access controller for application

	// httpHost is a parsed representation of the http.host parameter from
	// the configuration. Only the Scheme and Host fields are used.
	httpHost url.URL

	// events contains notification related configuration.
	events struct {
		sink   notifications.Sink
		source notifications.SourceRecord
	}

	redis redis.UniversalClient

	// isCache is true if this registry is configured as a pull through cache
	isCache bool

	// readOnly is true if the registry is in a read-only maintenance mode
	readOnly bool

	manifestURLs validation.ManifestURLs
}

// NewApp takes a configuration and returns a configured app, ready to serve
// requests. The app only implements ServeHTTP and can be wrapped in other
// handlers accordingly.
func NewApp(ctx context.Context, config *configuration.Configuration) *App {
	app := &App{
		Config:  config,
		Context: ctx,
		router:  v2.RouterWithPrefix(config.HTTP.Prefix),
		isCache: config.Proxy.RemoteURL != "",
	}

	// Register the handler dispatchers.
	app.register(v2.RouteNameBase, func(ctx *Context, r *http.Request) http.Handler {
		return http.HandlerFunc(apiBase)
	})
	app.register(v2.RouteNameManifest, manifestDispatcher)
	app.register(v2.RouteNameCatalog, catalogDispatcher)
	app.register(v2.RouteNameTags, tagsDispatcher)
	app.register(v2.RouteNameTag, tagDispatcher)
	app.register(v2.RouteNameBlob, blobDispatcher)
	app.register(v2.RouteNameBlobUpload, blobUploadDispatcher)
	app.register(v2.RouteNameBlobUploadChunk, blobUploadDispatcher)

	storageParams := config.Storage.Parameters()
	if storageParams == nil {
		storageParams = make(configuration.Parameters)
	}

	var err error
	app.driver, err = factory.Create(config.Storage.Type(), storageParams)
	if err != nil {
		// TODO(stevvooe): Move the creation of a service into a protected
		// method, where this is created lazily. Its status can be queried via
		// a health check.
		panic(err)
	}

	purgeConfig := uploadPurgeDefaultConfig()
	if mc, ok := config.Storage["maintenance"]; ok {
		if v, ok := mc["uploadpurging"]; ok {
			purgeConfig, ok = v.(map[interface{}]interface{})
			if !ok {
				panic("uploadpurging config key must contain additional keys")
			}
		}
		if v, ok := mc["readonly"]; ok {
			readOnly, ok := v.(map[interface{}]interface{})
			if !ok {
				panic("readonly config key must contain additional keys")
			}
			if readOnlyEnabled, ok := readOnly["enabled"]; ok {
				app.readOnly, ok = readOnlyEnabled.(bool)
				if !ok {
					panic("readonly's enabled config key must have a boolean value")
				}
			}
		}
	}

	log := dcontext.GetLogger(app)

	startUploadPurger(app, app.driver, log, purgeConfig)

	app.driver, err = applyStorageMiddleware(app.driver, config.Middleware["storage"])
	if err != nil {
		panic(err)
	}

	app.configureSecret(config)
	app.configureEvents(config)
	app.configureRedis(config)

	options := registrymiddleware.GetRegistryOptions()

	// TODO: Once schema1 code is removed throughout the registry, we will not
	// need to explicitly configure this.
	options = append(options, storage.DisableSchema1Pulls)

	if config.HTTP.Host != "" {
		u, err := url.Parse(config.HTTP.Host)
		if err != nil {
			panic(fmt.Sprintf(`could not parse http "host" parameter: %v`, err))
		}
		app.httpHost = *u
	}

	if app.isCache {
		options = append(options, storage.DisableDigestResumption)
	}

	// configure deletion
	if d, ok := config.Storage["delete"]; ok {
		e, ok := d["enabled"]
		if ok {
			if deleteEnabled, ok := e.(bool); ok && deleteEnabled {
				options = append(options, storage.EnableDelete)
			}
		}
	}

	// configure redirects
	var redirectDisabled bool
	if redirectConfig, ok := config.Storage["redirect"]; ok {
		v := redirectConfig["disable"]
		switch v := v.(type) {
		case bool:
			redirectDisabled = v
		default:
			panic(fmt.Sprintf("invalid type %T for 'storage.redirect.disable' (boolean)", v))
		}
	}
	if redirectDisabled {
		log.Info("backend redirection disabled")
	} else {
		exceptions := config.Storage["redirect"]["exceptions"]
		if exceptions, ok := exceptions.([]interface{}); ok && len(exceptions) > 0 {
			s := make([]string, len(exceptions))
			for i, v := range exceptions {
				s[i] = fmt.Sprint(v)
			}

			log.WithField("exceptions", s).Info("backend redirection enabled with exceptions")

			options = append(options, storage.EnableRedirectWithExceptions(s))
		} else {
			options = append(options, storage.EnableRedirect)
		}
	}

	if !config.Validation.Enabled {
		config.Validation.Enabled = !config.Validation.Disabled
	}

	// configure validation
	if config.Validation.Enabled {
		if len(config.Validation.Manifests.URLs.Allow) == 0 && len(config.Validation.Manifests.URLs.Deny) == 0 {
			// If Allow and Deny are empty, allow nothing.
			app.manifestURLs.Allow = regexp.MustCompile("^$")
			options = append(options, storage.ManifestURLsAllowRegexp(app.manifestURLs.Allow))
		} else {
			if len(config.Validation.Manifests.URLs.Allow) > 0 {
				for i, s := range config.Validation.Manifests.URLs.Allow {
					// Validate via compilation.
					if _, err := regexp.Compile(s); err != nil {
						panic(fmt.Sprintf("validation.manifests.urls.allow: %s", err))
					}
					// Wrap with non-capturing group.
					config.Validation.Manifests.URLs.Allow[i] = fmt.Sprintf("(?:%s)", s)
				}
				app.manifestURLs.Allow = regexp.MustCompile(strings.Join(config.Validation.Manifests.URLs.Allow, "|"))
				options = append(options, storage.ManifestURLsAllowRegexp(app.manifestURLs.Allow))
			}
			if len(config.Validation.Manifests.URLs.Deny) > 0 {
				for i, s := range config.Validation.Manifests.URLs.Deny {
					// Validate via compilation.
					if _, err := regexp.Compile(s); err != nil {
						panic(fmt.Sprintf("validation.manifests.urls.deny: %s", err))
					}
					// Wrap with non-capturing group.
					config.Validation.Manifests.URLs.Deny[i] = fmt.Sprintf("(?:%s)", s)
				}
				app.manifestURLs.Deny = regexp.MustCompile(strings.Join(config.Validation.Manifests.URLs.Deny, "|"))
				options = append(options, storage.ManifestURLsDenyRegexp(app.manifestURLs.Deny))
			}
		}
	}

	// Connect to the metadata database, if enabled.
	if config.Database.Enabled {
		log.Warn("the metadata database is an experimental feature, please do not enable it in production")

		db, err := datastore.Open(&datastore.DSN{
			Host:           config.Database.Host,
			Port:           config.Database.Port,
			User:           config.Database.User,
			Password:       config.Database.Password,
			DBName:         config.Database.DBName,
			SSLMode:        config.Database.SSLMode,
			SSLCert:        config.Database.SSLCert,
			SSLKey:         config.Database.SSLKey,
			SSLRootCert:    config.Database.SSLRootCert,
			ConnectTimeout: config.Database.ConnectTimeout,
		},
			datastore.WithLogger(log.WithFields(logrus.Fields{"database": config.Database.DBName})),
			datastore.WithLogLevel(config.Log.Level),
			datastore.WithPreparedStatements(config.Database.PreparedStatements),
			datastore.WithPoolConfig(&datastore.PoolConfig{
				MaxIdle:     config.Database.Pool.MaxIdle,
				MaxOpen:     config.Database.Pool.MaxOpen,
				MaxLifetime: config.Database.Pool.MaxLifetime,
			}),
		)
		if err != nil {
			panic(fmt.Sprintf("failed to construct database connection: %v", err))
		}

		// Skip postdeployment migrations to prevent pending post deployment
		// migrations from preventing the registry from starting.
		m := migrations.NewMigrator(db.DB, migrations.SkipPostDeployment)
		pending, err := m.HasPending()
		if err != nil {
			panic(fmt.Sprintf("failed to check database migrations status: %v", err))
		}
		if pending {
			log.Fatalf("there are pending database migrations, use the 'registry database migrate' CLI " +
				"command to check and apply them")
		}

		app.db = db
		options = append(options, storage.Database(app.db))

		if config.HTTP.Debug.Prometheus.Enabled {
			// Expose database metrics to prometheus.
			collector := sqlmetrics.NewDBStatsCollector(config.Database.DBName, db)
			promclient.MustRegister(collector)
		}

		// update online GC settings (if needed) in the background to avoid delaying the app start
		go func() {
			if err := updateOnlineGCSettings(app.Context, app.db, config); err != nil {
				errortracking.Capture(err, errortracking.WithContext(app.Context))
				log.WithError(err).Error("failed to update online GC settings")
			}
		}()
		startOnlineGC(app.Context, app.db, app.driver, config)
	}

	// configure storage caches
	// It's possible that the metadata database will fill the same original need
	// as the blob descriptor cache (avoiding slow and/or expensive calls to
	// external storage) and enable the cache to be removed long term.
	//
	// For now, disabling concurrent use of the metadata database and cache
	// decreases the surface area which we are testing during database development.
	if cc, ok := config.Storage["cache"]; ok && !config.Database.Enabled {
		v, ok := cc["blobdescriptor"]
		if !ok {
			// Backwards compatible: "layerinfo" == "blobdescriptor"
			v = cc["layerinfo"]
		}

		switch v {
		case "redis":
			if app.redis == nil {
				panic("redis configuration required to use for layerinfo cache")
			}
			cacheProvider := rediscache.NewRedisBlobDescriptorCacheProvider(app.redis)
			localOptions := append(options, storage.BlobDescriptorCacheProvider(cacheProvider))
			app.registry, err = storage.NewRegistry(app, app.driver, localOptions...)
			if err != nil {
				panic("could not create registry: " + err.Error())
			}
			log.Info("using redis blob descriptor cache")
		case "inmemory":
			cacheProvider := memorycache.NewInMemoryBlobDescriptorCacheProvider()
			localOptions := append(options, storage.BlobDescriptorCacheProvider(cacheProvider))
			app.registry, err = storage.NewRegistry(app, app.driver, localOptions...)
			if err != nil {
				panic("could not create registry: " + err.Error())
			}
			log.Info("using inmemory blob descriptor cache")
		default:
			if v != "" {
				log.WithField("type", config.Storage["cache"]).Warn("unknown cache type, caching disabled")
			}
		}
	} else if ok && config.Database.Enabled {
		log.Warn("blob descriptor cache is not compatible with metadata database, caching disabled")
	}

	if app.registry == nil {
		// configure the registry if no cache section is available.
		app.registry, err = storage.NewRegistry(app.Context, app.driver, options...)
		if err != nil {
			panic("could not create registry: " + err.Error())
		}
	}

	app.registry, err = applyRegistryMiddleware(app, app.registry, config.Middleware["registry"])
	if err != nil {
		panic(err)
	}

	if config.Migration.Enabled {
		app.migrationRegistry = migrationRegistry(app.Context, config, options...)

		app.migrationRegistry, err = applyRegistryMiddleware(app, app.migrationRegistry, config.Middleware["registry"])
		if err != nil {
			panic(err)
		}
	}

	authType := config.Auth.Type()

	if authType != "" && !strings.EqualFold(authType, "none") {
		accessController, err := auth.GetAccessController(config.Auth.Type(), config.Auth.Parameters())
		if err != nil {
			panic(fmt.Sprintf("unable to configure authorization (%s): %v", authType, err))
		}
		app.accessController = accessController
		log.WithField("auth_type", authType).Debug("configured access controller")
	}

	// configure as a pull through cache
	if config.Proxy.RemoteURL != "" {
		app.registry, err = proxy.NewRegistryPullThroughCache(ctx, app.registry, app.driver, config.Proxy)
		if err != nil {
			panic(err.Error())
		}
		app.isCache = true
		log.WithField("remote", config.Proxy.RemoteURL).Info("registry configured as a proxy cache")
	}
	var ok bool
	app.repoRemover, ok = app.registry.(distribution.RepositoryRemover)
	if !ok {
		log.Warn("registry does not implement RepositoryRemover. Will not be able to delete repos and tags")
	}

	if config.Migration.Enabled && (len(config.Migration.Include) > 0 || len(config.Migration.Exclude) > 0) {
		include := make([]string, len(config.Migration.Include))
		for _, r := range config.Migration.Include {
			include = append(include, r.String())
		}
		exclude := make([]string, len(config.Migration.Exclude))
		for _, r := range config.Migration.Exclude {
			exclude = append(exclude, r.String())
		}
		log.WithFields(logrus.Fields{"include": include, "exclude": exclude}).Info("migration proxy enabled with filters")
	}

	return app
}

func migrationRegistry(ctx context.Context, config *configuration.Configuration, options ...storage.RegistryOption) distribution.Namespace {
	storageParams := config.Storage.Parameters()
	if storageParams == nil {
		storageParams = make(configuration.Parameters)
	}

	storageParams["rootdirectory"] = config.Migration.AlternativeRootDirectory

	driver, err := factory.Create(config.Storage.Type(), storageParams)
	if err != nil {
		panic(err)
	}

	if config.Migration.DisableMirrorFS {
		options = append(options, storage.DisableMirrorFS)
	}

	registry, err := storage.NewRegistry(ctx, driver, options...)
	if err != nil {
		panic(err)
	}

	return registry
}

func (app *App) shouldMigrate(repo distribution.Repository) (bool, error) {
	if !(app.Config.Database.Enabled && app.Config.Migration.Enabled) {
		return false, nil
	}

	validator, ok := repo.(storage.RepositoryValidator)
	if !ok {
		return false, errors.New("repository does not implement RepositoryValidator interface")
	}

	// check if repository exists in this instance's storage backend, proxy to target registry if not
	exists, err := validator.Exists(app.Context)
	if err != nil {
		return false, fmt.Errorf("unable to determine if repository exists: %w", err)
	}
	log := dcontext.GetLogger(app.Context)
	if exists {
		log.Warn("repository will be served via the filesystem")
		return false, nil
	}

	// evaluate inclusion filters, if any
	if len(app.Config.Migration.Include) > 0 {
		var proxy bool
		for _, r := range app.Config.Migration.Include {
			if r.MatchString(repo.Named().String()) {
				proxy = true
			}
		}
		if !proxy {
			log.Warn("repository name does not match any inclusion filter, request will be served via the filesystem")
			return false, nil
		}
	}
	// evaluate exclusion filters, if any
	if len(app.Config.Migration.Exclude) > 0 {
		for _, r := range app.Config.Migration.Exclude {
			if r.MatchString(repo.Named().String()) {
				log.WithField("filter", r.String()).Debug("repository name matches an exclusion filter, request will be served via the filesystem")
				return false, nil
			}
		}
	}

	log.Warn("repository will be served via the database")
	return true, nil
}

var (
	onlineGCUpdateJitterMaxSeconds = 60
	onlineGCUpdateTimeout          = 2 * time.Second
	// for testing purposes (mocks)
	systemClock                internal.Clock = clock.New()
	gcSettingsStoreConstructor                = datastore.NewGCSettingsStore
)

func updateOnlineGCSettings(ctx context.Context, db datastore.Queryer, config *configuration.Configuration) error {
	if !config.Database.Enabled || config.GC.Disabled || (config.GC.Blobs.Disabled && config.GC.Manifests.Disabled) {
		return nil
	}
	if config.GC.ReviewAfter == 0 {
		return nil
	}

	d := config.GC.ReviewAfter
	// -1 means no review delay, so set it to 0 here
	if d == -1 {
		d = 0
	}

	log := dcontext.GetLogger(ctx)

	// execute DB update after a randomized jitter of up to 60 seconds to ease concurrency in clustered environments
	rand.Seed(systemClock.Now().UnixNano())
	jitter := time.Duration(rand.Intn(onlineGCUpdateJitterMaxSeconds)) * time.Second

	log.WithField("jitter_s", jitter.Seconds()).Info("preparing to update online GC settings")
	systemClock.Sleep(jitter)

	// set a tight timeout to avoid delaying the app start for too long, another instance is likely to succeed
	start := systemClock.Now()
	ctx2, cancel := context.WithDeadline(ctx, start.Add(onlineGCUpdateTimeout))
	defer cancel()

	// for now we use the same value for all events, so we simply update all rows in `gc_review_after_defaults`
	s := gcSettingsStoreConstructor(db)
	updated, err := s.UpdateAllReviewAfterDefaults(ctx2, d)
	if err != nil {
		return err
	}

	elapsed := systemClock.Since(start).Seconds()
	if updated {
		log.WithField("duration_s", elapsed).Info("online GC settings updated successfully")
	} else {
		log.WithField("duration_s", elapsed).Info("online GC settings are up to date")
	}

	return nil
}

func startOnlineGC(ctx context.Context, db *datastore.DB, storageDriver storagedriver.StorageDriver, config *configuration.Configuration) {
	if !config.Database.Enabled || config.GC.Disabled || (config.GC.Blobs.Disabled && config.GC.Manifests.Disabled) {
		return
	}

	log := dcontext.GetLogger(ctx)

	aOpts := []gc.AgentOption{
		gc.WithLogger(log),
	}
	if config.GC.NoIdleBackoff {
		aOpts = append(aOpts, gc.WithoutIdleBackoff())
	}
	if config.GC.MaxBackoff > 0 {
		aOpts = append(aOpts, gc.WithMaxBackoff(config.GC.MaxBackoff))
	}

	var agents []*gc.Agent

	if !config.GC.Blobs.Disabled {
		bwOpts := []worker.BlobWorkerOption{
			worker.WithBlobLogger(log),
		}
		if config.GC.TransactionTimeout > 0 {
			bwOpts = append(bwOpts, worker.WithBlobTxTimeout(config.GC.TransactionTimeout))
		}
		if config.GC.Blobs.StorageTimeout > 0 {
			bwOpts = append(bwOpts, worker.WithBlobStorageTimeout(config.GC.Blobs.StorageTimeout))
		}
		bw := worker.NewBlobWorker(db, storageDriver, bwOpts...)

		baOpts := aOpts
		if config.GC.Blobs.Interval > 0 {
			baOpts = append(baOpts, gc.WithInitialInterval(config.GC.Blobs.Interval))
		}
		ba := gc.NewAgent(bw, baOpts...)
		agents = append(agents, ba)
	}

	if !config.GC.Manifests.Disabled {
		mwOpts := []worker.ManifestWorkerOption{
			worker.WithManifestLogger(log),
		}
		if config.GC.TransactionTimeout > 0 {
			mwOpts = append(mwOpts, worker.WithManifestTxTimeout(config.GC.TransactionTimeout))
		}
		mw := worker.NewManifestWorker(db, mwOpts...)

		maOpts := aOpts
		if config.GC.Manifests.Interval > 0 {
			maOpts = append(maOpts, gc.WithInitialInterval(config.GC.Manifests.Interval))
		}
		ma := gc.NewAgent(mw, maOpts...)
		agents = append(agents, ma)
	}

	for _, a := range agents {
		go func(a *gc.Agent) {
			// This function can only end in two situations: panic or context cancellation. If a panic occurs we should
			// log, report to Sentry and then re-panic, as the instance would be in an inconsistent/unknown state. In
			// case of context cancellation, the app is shutting down, so there is nothing to worry about.
			defer func() {
				if err := recover(); err != nil {
					log.WithField("error", err).Error("online GC agent stopped with panic")
					sentry.CurrentHub().Recover(err)
					sentry.Flush(5 * time.Second)
					panic(err)
				}
			}()
			if err := a.Start(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					// leaving this here for now for additional confidence and improved observability
					log.Warn("shutting down online GC agent due due to context cancellation")
				} else {
					// this should never happen, but leaving it here for future proofing against bugs within Agent.Start
					errortracking.Capture(fmt.Errorf("online GC agent stopped with error: %w", err))
					log.WithError(err).Error("online GC agent stopped")
				}
			}
		}(a)
	}
}

// RegisterHealthChecks is an awful hack to defer health check registration
// control to callers. This should only ever be called once per registry
// process, typically in a main function. The correct way would be register
// health checks outside of app, since multiple apps may exist in the same
// process. Because the configuration and app are tightly coupled,
// implementing this properly will require a refactor. This method may panic
// if called twice in the same process.
func (app *App) RegisterHealthChecks(healthRegistries ...*health.Registry) {
	if len(healthRegistries) > 1 {
		panic("RegisterHealthChecks called with more than one registry")
	}
	healthRegistry := health.DefaultRegistry
	if len(healthRegistries) == 1 {
		healthRegistry = healthRegistries[0]
	}

	if app.Config.Health.StorageDriver.Enabled {
		interval := app.Config.Health.StorageDriver.Interval
		if interval == 0 {
			interval = defaultCheckInterval
		}

		storageDriverCheck := func() error {
			_, err := app.driver.Stat(app, "/") // "/" should always exist
			if _, ok := err.(storagedriver.PathNotFoundError); ok {
				err = nil // pass this through, backend is responding, but this path doesn't exist.
			}
			return err
		}

		if app.Config.Health.StorageDriver.Threshold != 0 {
			healthRegistry.RegisterPeriodicThresholdFunc("storagedriver_"+app.Config.Storage.Type(), interval, app.Config.Health.StorageDriver.Threshold, storageDriverCheck)
		} else {
			healthRegistry.RegisterPeriodicFunc("storagedriver_"+app.Config.Storage.Type(), interval, storageDriverCheck)
		}
	}

	for _, fileChecker := range app.Config.Health.FileCheckers {
		interval := fileChecker.Interval
		if interval == 0 {
			interval = defaultCheckInterval
		}
		dcontext.GetLogger(app).Infof("configuring file health check path=%s, interval=%d", fileChecker.File, interval/time.Second)
		healthRegistry.Register(fileChecker.File, health.PeriodicChecker(checks.FileChecker(fileChecker.File), interval))
	}

	for _, httpChecker := range app.Config.Health.HTTPCheckers {
		interval := httpChecker.Interval
		if interval == 0 {
			interval = defaultCheckInterval
		}

		statusCode := httpChecker.StatusCode
		if statusCode == 0 {
			statusCode = 200
		}

		checker := checks.HTTPChecker(httpChecker.URI, statusCode, httpChecker.Timeout, httpChecker.Headers)

		if httpChecker.Threshold != 0 {
			dcontext.GetLogger(app).Infof("configuring HTTP health check uri=%s, interval=%d, threshold=%d", httpChecker.URI, interval/time.Second, httpChecker.Threshold)
			healthRegistry.Register(httpChecker.URI, health.PeriodicThresholdChecker(checker, interval, httpChecker.Threshold))
		} else {
			dcontext.GetLogger(app).Infof("configuring HTTP health check uri=%s, interval=%d", httpChecker.URI, interval/time.Second)
			healthRegistry.Register(httpChecker.URI, health.PeriodicChecker(checker, interval))
		}
	}

	for _, tcpChecker := range app.Config.Health.TCPCheckers {
		interval := tcpChecker.Interval
		if interval == 0 {
			interval = defaultCheckInterval
		}

		checker := checks.TCPChecker(tcpChecker.Addr, tcpChecker.Timeout)

		if tcpChecker.Threshold != 0 {
			dcontext.GetLogger(app).Infof("configuring TCP health check addr=%s, interval=%d, threshold=%d", tcpChecker.Addr, interval/time.Second, tcpChecker.Threshold)
			healthRegistry.Register(tcpChecker.Addr, health.PeriodicThresholdChecker(checker, interval, tcpChecker.Threshold))
		} else {
			dcontext.GetLogger(app).Infof("configuring TCP health check addr=%s, interval=%d", tcpChecker.Addr, interval/time.Second)
			healthRegistry.Register(tcpChecker.Addr, health.PeriodicChecker(checker, interval))
		}
	}
}

var routeMetricsMiddleware = metricskit.NewHandlerFactory(
	metricskit.WithNamespace(prometheus.NamespacePrefix),
	metricskit.WithLabels("route"),
	// Keeping the same buckets used before LabKit, as defined in
	// https://github.com/docker/go-metrics/blob/b619b3592b65de4f087d9f16863a7e6ff905973c/handler.go#L31:L32
	metricskit.WithRequestDurationBuckets([]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 25, 60}),
	metricskit.WithByteSizeBuckets(promclient.ExponentialBuckets(1024, 2, 22)), //1K to 4G
)

// register a handler with the application, by route name. The handler will be
// passed through the application filters and context will be constructed at
// request time.
func (app *App) register(routeName string, dispatch dispatchFunc) {
	handler := app.dispatcher(dispatch)

	// Chain the handler with prometheus instrumented handler
	if app.Config.HTTP.Debug.Prometheus.Enabled {
		handler = routeMetricsMiddleware(
			handler,
			metricskit.WithLabelValues(map[string]string{"route": v2.RoutePath(routeName)}),
		)
	}

	// TODO(stevvooe): This odd dispatcher/route registration is by-product of
	// some limitations in the gorilla/mux router. We are using it to keep
	// routing consistent between the client and server, but we may want to
	// replace it with manual routing and structure-based dispatch for better
	// control over the request execution.

	app.router.GetRoute(routeName).Handler(handler)
}

// configureEvents prepares the event sink for action.
func (app *App) configureEvents(configuration *configuration.Configuration) {
	// Configure all of the endpoint sinks.
	var sinks []notifications.Sink
	for _, endpoint := range configuration.Notifications.Endpoints {
		if endpoint.Disabled {
			dcontext.GetLogger(app).Infof("endpoint %s disabled, skipping", endpoint.Name)
			continue
		}

		dcontext.GetLogger(app).Infof("configuring endpoint %v (%v), timeout=%s, headers=%v", endpoint.Name, endpoint.URL, endpoint.Timeout, endpoint.Headers)
		endpoint := notifications.NewEndpoint(endpoint.Name, endpoint.URL, notifications.EndpointConfig{
			Timeout:           endpoint.Timeout,
			Threshold:         endpoint.Threshold,
			Backoff:           endpoint.Backoff,
			Headers:           endpoint.Headers,
			IgnoredMediaTypes: endpoint.IgnoredMediaTypes,
			Ignore:            endpoint.Ignore,
		})

		sinks = append(sinks, endpoint)
	}

	// NOTE(stevvooe): Moving to a new queuing implementation is as easy as
	// replacing broadcaster with a rabbitmq implementation. It's recommended
	// that the registry instances also act as the workers to keep deployment
	// simple.
	app.events.sink = notifications.NewBroadcaster(sinks...)

	// Populate registry event source
	hostname, err := os.Hostname()
	if err != nil {
		hostname = configuration.HTTP.Addr
	} else {
		// try to pick the port off the config
		_, port, err := net.SplitHostPort(configuration.HTTP.Addr)
		if err == nil {
			hostname = net.JoinHostPort(hostname, port)
		}
	}

	app.events.source = notifications.SourceRecord{
		Addr:       hostname,
		InstanceID: dcontext.GetStringValue(app, "instance.id"),
	}
}

func (app *App) configureRedis(configuration *configuration.Configuration) {
	if configuration.Redis.Addr == "" {
		dcontext.GetLogger(app).Infof("redis not configured")
		return
	}

	opts := &redis.UniversalOptions{
		Addrs:        strings.Split(configuration.Redis.Addr, ","),
		DB:           configuration.Redis.DB,
		Password:     configuration.Redis.Password,
		DialTimeout:  configuration.Redis.DialTimeout,
		ReadTimeout:  configuration.Redis.ReadTimeout,
		WriteTimeout: configuration.Redis.WriteTimeout,
		PoolSize:     configuration.Redis.Pool.Size,
		MaxConnAge:   configuration.Redis.Pool.MaxLifetime,
		MasterName:   configuration.Redis.MainName,
	}
	if configuration.Redis.TLS.Enabled {
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: configuration.Redis.TLS.Insecure,
		}
	}
	if configuration.Redis.Pool.IdleTimeout > 0 {
		opts.IdleTimeout = configuration.Redis.Pool.IdleTimeout
	}
	// NewUniversalClient will take care of returning the appropriate client type (simple or sentinel) depending on the
	// configuration options. See https://pkg.go.dev/github.com/go-redis/redis/v8#NewUniversalClient.
	app.redis = redis.NewUniversalClient(opts)

	// setup expvar
	registry := expvar.Get("registry")
	if registry == nil {
		registry = expvar.NewMap("registry")
	}

	registry.(*expvar.Map).Set("redis", expvar.Func(func() interface{} {
		poolStats := app.redis.PoolStats()
		return map[string]interface{}{
			"Config": configuration.Redis,
			"Active": poolStats.TotalConns - poolStats.IdleConns,
		}
	}))
}

// configureSecret creates a random secret if a secret wasn't included in the
// configuration.
func (app *App) configureSecret(configuration *configuration.Configuration) {
	if configuration.HTTP.Secret == "" {
		var secretBytes [randomSecretSize]byte
		if _, err := cryptorand.Read(secretBytes[:]); err != nil {
			panic(fmt.Sprintf("could not generate random bytes for HTTP secret: %v", err))
		}
		configuration.HTTP.Secret = string(secretBytes[:])
		dcontext.GetLogger(app).Warn("No HTTP secret provided - generated random secret. This may cause problems with uploads if multiple registries are behind a load-balancer. To provide a shared secret, fill in http.secret in the configuration file or set the REGISTRY_HTTP_SECRET environment variable.")
	}
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() // ensure that request body is always closed.

	// Prepare the context with our own little decorations.
	ctx := r.Context()
	ctx = dcontext.WithRequest(ctx, r)
	ctx, w = dcontext.WithResponseWriter(ctx, w)
	ctx = dcontext.WithLogger(ctx, dcontext.GetRequestCorrelationLogger(ctx))
	r = r.WithContext(ctx)

	if app.Config.Log.AccessLog.Disabled {
		defer func() {
			status, ok := ctx.Value("http.response.status").(int)
			if ok && status >= 200 && status <= 399 {
				dcontext.GetResponseLogger(r.Context()).Infof("response completed")
			}
		}()
	}

	// Set a header with the Docker Distribution API Version for all responses.
	w.Header().Add("Docker-Distribution-API-Version", "registry/2.0")
	app.router.ServeHTTP(w, r)
}

// dispatchFunc takes a context and request and returns a constructed handler
// for the route. The dispatcher will use this to dynamically create request
// specific handlers for each endpoint without creating a new router for each
// request.
type dispatchFunc func(ctx *Context, r *http.Request) http.Handler

// TODO(stevvooe): dispatchers should probably have some validation error
// chain with proper error reporting.

// dispatcher returns a handler that constructs a request specific context and
// handler, using the dispatch factory function.
func (app *App) dispatcher(dispatch dispatchFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for headerName, headerValues := range app.Config.HTTP.Headers {
			for _, value := range headerValues {
				w.Header().Add(headerName, value)
			}
		}

		context := app.context(w, r)

		if err := app.authorized(w, r, context); err != nil {
			dcontext.GetLogger(context).Warnf("error authorizing context: %v", err)
			return
		}

		// Add username to request logging
		context.Context = dcontext.WithLogger(context.Context, dcontext.GetLogger(context.Context, auth.UserNameKey))

		// sync up context on the request.
		// TODO (Hayley): We'll need to determine now to handle requests from the
		// v2/_catalog endpoint with this single registry approach as there's no
		// repository information. We'll need to favor one side or the other, or
		// merge the results from both into a single set.
		r = r.WithContext(context)

		if app.nameRequired(r) {
			nameRef, err := reference.WithName(getName(context))
			if err != nil {
				dcontext.GetLogger(context).Errorf("error parsing reference from context: %v", err)
				context.Errors = append(context.Errors, distribution.ErrRepositoryNameInvalid{
					Name:   getName(context),
					Reason: err,
				})
				if err := errcode.ServeJSON(w, context.Errors); err != nil {
					dcontext.GetLogger(context).Errorf("error serving error json: %v (from %v)", err, context.Errors)
				}
				return
			}

			bp, ok := app.registry.Blobs().(distribution.BlobProvider)
			if !ok {
				panic(fmt.Errorf("unable to convert BlobEnumerator into BlobProvider"))
			}
			context.blobProvider = bp

			repository, err := app.registry.Repository(context, nameRef)
			if err != nil {
				dcontext.GetLogger(context).Errorf("error resolving repository: %v", err)

				switch err := err.(type) {
				case distribution.ErrRepositoryUnknown:
					context.Errors = append(context.Errors, v2.ErrorCodeNameUnknown.WithDetail(err))
				case distribution.ErrRepositoryNameInvalid:
					context.Errors = append(context.Errors, v2.ErrorCodeNameInvalid.WithDetail(err))
				case errcode.Error:
					context.Errors = append(context.Errors, err)
				}

				if err := errcode.ServeJSON(w, context.Errors); err != nil {
					dcontext.GetLogger(context).Errorf("error serving error json: %v (from %v)", err, context.Errors)
				}
				return
			}

			migrateRepo, err := app.shouldMigrate(repository)
			if err != nil {
				panic(err)
			}

			switch {
			case migrateRepo:
				// Prepare the migration side of filesystem storage and pass it to the Context.
				bp, ok := app.migrationRegistry.Blobs().(distribution.BlobProvider)
				if !ok {
					panic(fmt.Errorf("unable to convert BlobEnumerator into BlobProvider"))
				}
				context.blobProvider = bp

				repository, err = app.migrationRegistry.Repository(context, nameRef)
				if err != nil {
					dcontext.GetLogger(context).Errorf("error resolving repository: %v", err)

					switch err := err.(type) {
					case distribution.ErrRepositoryUnknown:
						context.Errors = append(context.Errors, v2.ErrorCodeNameUnknown.WithDetail(err))
					case distribution.ErrRepositoryNameInvalid:
						context.Errors = append(context.Errors, v2.ErrorCodeNameInvalid.WithDetail(err))
					case errcode.Error:
						context.Errors = append(context.Errors, err)
					}

					if err := errcode.ServeJSON(w, context.Errors); err != nil {
						dcontext.GetLogger(context).Errorf("error serving error json: %v (from %v)", err, context.Errors)
					}
					return
				}

				// We're writing the blob to the database and optionally mirroring the
				// metadata to the filesystem as well.
				context.useDatabase = true
				context.writeFSMetadata = !app.Config.Migration.DisableMirrorFS
				// We're not migrating and the database is enabled, read/write from the
				// database except for writing blobs to common storage.
			case !app.Config.Migration.Enabled && app.Config.Database.Enabled:
				context.useDatabase = true
				context.writeFSMetadata = false
				// We're either not migrating this repository, or we're not migrating at
				// all and the database is not enabled. Either way, read/write from
				// the filesystem alone.
			case app.Config.Migration.Enabled && !migrateRepo,
				!app.Config.Database.Enabled:
				context.useDatabase = false
				context.writeFSMetadata = true
			default:
				panic("unexpected configuration")
			}

			// assign and decorate the authorized repository with an event bridge.
			context.Repository, context.RepositoryRemover = notifications.Listen(
				repository,
				context.App.repoRemover,
				app.eventBridge(context, r))

			context.Repository, err = applyRepoMiddleware(app, context.Repository, app.Config.Middleware["repository"])
			if err != nil {
				dcontext.GetLogger(context).Errorf("error initializing repository middleware: %v", err)
				context.Errors = append(context.Errors, errcode.ErrorCodeUnknown.WithDetail(err))

				if err := errcode.ServeJSON(w, context.Errors); err != nil {
					dcontext.GetLogger(context).Errorf("error serving error json: %v (from %v)", err, context.Errors)
				}
				return
			}
		}

		dispatch(context, r).ServeHTTP(w, r)
		// Automated error response handling here. Handlers may return their
		// own errors if they need different behavior (such as range errors
		// for layer upload).
		if context.Errors.Len() > 0 {
			if err := errcode.ServeJSON(w, context.Errors); err != nil {
				dcontext.GetLogger(context).Errorf("error serving error json: %v (from %v)", err, context.Errors)
			}

			app.logError(context, r, context.Errors)
		}
	})
}

func (app *App) logError(ctx context.Context, r *http.Request, errors errcode.Errors) {
	for _, e := range errors {
		var code errcode.ErrorCode
		var message, detail string

		switch ex := e.(type) {
		case errcode.Error:
			code = ex.Code
			message = ex.Message
			detail = fmt.Sprintf("%+v", ex.Detail)
		case errcode.ErrorCode:
			code = ex
			message = ex.Message()
		default:
			// just normal go 'error'
			code = errcode.ErrorCodeUnknown
			message = ex.Error()
		}

		l := dcontext.GetLogger(ctx).WithField("code", code.String())
		if detail != "" {
			l = l.WithField("detail", detail)
		}

		l.WithError(e).Error(message)

		// only report 500 errors to Sentry
		if code == errcode.ErrorCodeUnknown {
			// Encode detail in error message so that it shows up in Sentry. This is a hack until we refactor error
			// handling across the whole application to enforce consistent behaviour and formatting.
			// see https://gitlab.com/gitlab-org/container-registry/-/issues/198
			detailSuffix := ""
			if detail != "" {
				detailSuffix = fmt.Sprintf(": %s", detail)
			}
			err := errcode.ErrorCodeUnknown.WithMessage(fmt.Sprintf("%s%s", message, detailSuffix))
			errortracking.Capture(err, errortracking.WithContext(ctx), errortracking.WithRequest(r))
		}
	}
}

// context constructs the context object for the application. This only be
// called once per request.
func (app *App) context(w http.ResponseWriter, r *http.Request) *Context {
	ctx := r.Context()
	ctx = dcontext.WithVars(ctx, r)
	name := dcontext.GetStringValue(ctx, "vars.name")
	ctx = context.WithValue(ctx, "root_repo", strings.Split(name, "/")[0])
	ctx = dcontext.WithLogger(ctx, dcontext.GetLogger(ctx,
		"root_repo",
		"vars.name",
		"vars.reference",
		"vars.digest",
		"vars.uuid"))

	context := &Context{
		App:     app,
		Context: ctx,
	}

	if app.httpHost.Scheme != "" && app.httpHost.Host != "" {
		// A "host" item in the configuration takes precedence over
		// X-Forwarded-Proto and X-Forwarded-Host headers, and the
		// hostname in the request.
		context.urlBuilder = v2.NewURLBuilder(&app.httpHost, false)
	} else {
		context.urlBuilder = v2.NewURLBuilderFromRequest(r, app.Config.HTTP.RelativeURLs)
	}

	return context
}

// authorized checks if the request can proceed with access to the requested
// repository. If it succeeds, the context may access the requested
// repository. An error will be returned if access is not available.
func (app *App) authorized(w http.ResponseWriter, r *http.Request, context *Context) error {
	dcontext.GetLogger(context).Debug("authorizing request")
	repo := getName(context)

	if app.accessController == nil {
		return nil // access controller is not enabled.
	}

	var accessRecords []auth.Access

	if repo != "" {
		accessRecords = appendAccessRecords(accessRecords, r.Method, repo)
		if fromRepo := r.FormValue("from"); fromRepo != "" {
			// mounting a blob from one repository to another requires pull (GET)
			// access to the source repository.
			accessRecords = appendAccessRecords(accessRecords, "GET", fromRepo)
		}
	} else {
		// Only allow the name not to be set on the base route.
		if app.nameRequired(r) {
			// For this to be properly secured, repo must always be set for a
			// resource that may make a modification. The only condition under
			// which name is not set and we still allow access is when the
			// base route is accessed. This section prevents us from making
			// that mistake elsewhere in the code, allowing any operation to
			// proceed.
			if err := errcode.ServeJSON(w, errcode.ErrorCodeUnauthorized); err != nil {
				dcontext.GetLogger(context).Errorf("error serving error json: %v (from %v)", err, context.Errors)
			}
			return fmt.Errorf("forbidden: no repository name")
		}
		accessRecords = appendCatalogAccessRecord(accessRecords, r)
	}

	ctx, err := app.accessController.Authorized(context.Context, accessRecords...)
	if err != nil {
		switch err := err.(type) {
		case auth.Challenge:
			// Add the appropriate WWW-Auth header
			err.SetHeaders(r, w)

			if err := errcode.ServeJSON(w, errcode.ErrorCodeUnauthorized.WithDetail(accessRecords)); err != nil {
				dcontext.GetLogger(context).Errorf("error serving error json: %v (from %v)", err, context.Errors)
			}
		default:
			// This condition is a potential security problem either in
			// the configuration or whatever is backing the access
			// controller. Just return a bad request with no information
			// to avoid exposure. The request should not proceed.
			dcontext.GetLogger(context).Errorf("error checking authorization: %v", err)
			w.WriteHeader(http.StatusBadRequest)
		}

		return err
	}

	dcontext.GetLogger(ctx, auth.UserNameKey).Info("authorized request")
	// TODO(stevvooe): This pattern needs to be cleaned up a bit. One context
	// should be replaced by another, rather than replacing the context on a
	// mutable object.
	context.Context = ctx
	return nil
}

// eventBridge returns a bridge for the current request, configured with the
// correct actor and source.
func (app *App) eventBridge(ctx *Context, r *http.Request) notifications.Listener {
	actor := notifications.ActorRecord{
		Name: getUserName(ctx, r),
	}
	request := notifications.NewRequestRecord(dcontext.GetRequestID(ctx), r)

	return notifications.NewBridge(ctx.urlBuilder, app.events.source, actor, request, app.events.sink, app.Config.Notifications.EventConfig.IncludeReferences)
}

// nameRequired returns true if the route requires a name.
func (app *App) nameRequired(r *http.Request) bool {
	route := mux.CurrentRoute(r)
	if route == nil {
		return true
	}
	routeName := route.GetName()
	return routeName != v2.RouteNameBase && routeName != v2.RouteNameCatalog
}

// apiBase implements a simple yes-man for doing overall checks against the
// api. This can support auth roundtrips to support docker login.
func apiBase(w http.ResponseWriter, r *http.Request) {
	const emptyJSON = "{}"
	// Provide a simple /v2/ 200 OK response with empty json response.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprint(len(emptyJSON)))

	w.Header().Set("Gitlab-Container-Registry-Version", strings.TrimPrefix(version.Version, "v"))
	w.Header().Set("Gitlab-Container-Registry-Features", version.ExtFeatures)

	fmt.Fprint(w, emptyJSON)
}

// appendAccessRecords checks the method and adds the appropriate Access records to the records list.
func appendAccessRecords(records []auth.Access, method string, repo string) []auth.Access {
	resource := auth.Resource{
		Type: "repository",
		Name: repo,
	}

	switch method {
	case "GET", "HEAD":
		records = append(records,
			auth.Access{
				Resource: resource,
				Action:   "pull",
			})
	case "POST", "PUT", "PATCH":
		records = append(records,
			auth.Access{
				Resource: resource,
				Action:   "pull",
			},
			auth.Access{
				Resource: resource,
				Action:   "push",
			})
	case "DELETE":
		records = append(records,
			auth.Access{
				Resource: resource,
				Action:   "delete",
			})
	}
	return records
}

// Add the access record for the catalog if it's our current route
func appendCatalogAccessRecord(accessRecords []auth.Access, r *http.Request) []auth.Access {
	route := mux.CurrentRoute(r)
	routeName := route.GetName()

	if routeName == v2.RouteNameCatalog {
		resource := auth.Resource{
			Type: "registry",
			Name: "catalog",
		}

		accessRecords = append(accessRecords,
			auth.Access{
				Resource: resource,
				Action:   "*",
			})
	}
	return accessRecords
}

// applyRegistryMiddleware wraps a registry instance with the configured middlewares
func applyRegistryMiddleware(ctx context.Context, registry distribution.Namespace, middlewares []configuration.Middleware) (distribution.Namespace, error) {
	for _, mw := range middlewares {
		rmw, err := registrymiddleware.Get(ctx, mw.Name, mw.Options, registry)
		if err != nil {
			return nil, fmt.Errorf("unable to configure registry middleware (%s): %s", mw.Name, err)
		}
		registry = rmw
	}
	return registry, nil
}

// applyRepoMiddleware wraps a repository with the configured middlewares
func applyRepoMiddleware(ctx context.Context, repository distribution.Repository, middlewares []configuration.Middleware) (distribution.Repository, error) {
	for _, mw := range middlewares {
		rmw, err := repositorymiddleware.Get(ctx, mw.Name, mw.Options, repository)
		if err != nil {
			return nil, err
		}
		repository = rmw
	}
	return repository, nil
}

// applyStorageMiddleware wraps a storage driver with the configured middlewares
func applyStorageMiddleware(driver storagedriver.StorageDriver, middlewares []configuration.Middleware) (storagedriver.StorageDriver, error) {
	for _, mw := range middlewares {
		smw, err := storagemiddleware.Get(mw.Name, mw.Options, driver)
		if err != nil {
			return nil, fmt.Errorf("unable to configure storage middleware (%s): %v", mw.Name, err)
		}
		driver = smw
	}
	return driver, nil
}

// uploadPurgeDefaultConfig provides a default configuration for upload
// purging to be used in the absence of configuration in the
// configuration file
func uploadPurgeDefaultConfig() map[interface{}]interface{} {
	config := map[interface{}]interface{}{}
	config["enabled"] = true
	config["age"] = "168h"
	config["interval"] = "24h"
	config["dryrun"] = false
	return config
}

func badPurgeUploadConfig(reason string) {
	panic(fmt.Sprintf("Unable to parse upload purge configuration: %s", reason))
}

// startUploadPurger schedules a goroutine which will periodically
// check upload directories for old files and delete them
func startUploadPurger(ctx context.Context, storageDriver storagedriver.StorageDriver, log dcontext.Logger, config map[interface{}]interface{}) {
	if config["enabled"] == false {
		return
	}

	var purgeAgeDuration time.Duration
	var err error
	purgeAge, ok := config["age"]
	if ok {
		ageStr, ok := purgeAge.(string)
		if !ok {
			badPurgeUploadConfig("age is not a string")
		}
		purgeAgeDuration, err = time.ParseDuration(ageStr)
		if err != nil {
			badPurgeUploadConfig(fmt.Sprintf("Cannot parse duration: %s", err.Error()))
		}
	} else {
		badPurgeUploadConfig("age missing")
	}

	var intervalDuration time.Duration
	interval, ok := config["interval"]
	if ok {
		intervalStr, ok := interval.(string)
		if !ok {
			badPurgeUploadConfig("interval is not a string")
		}

		intervalDuration, err = time.ParseDuration(intervalStr)
		if err != nil {
			badPurgeUploadConfig(fmt.Sprintf("Cannot parse interval: %s", err.Error()))
		}
	} else {
		badPurgeUploadConfig("interval missing")
	}

	var dryRunBool bool
	dryRun, ok := config["dryrun"]
	if ok {
		dryRunBool, ok = dryRun.(bool)
		if !ok {
			badPurgeUploadConfig("cannot parse dryrun")
		}
	} else {
		badPurgeUploadConfig("dryrun missing")
	}

	go func() {
		rand.Seed(time.Now().Unix())
		jitter := time.Duration(rand.Int()%60) * time.Minute
		log.Infof("Starting upload purge in %s", jitter)
		time.Sleep(jitter)

		for {
			storage.PurgeUploads(ctx, storageDriver, time.Now().Add(-purgeAgeDuration), !dryRunBool)
			log.Infof("Starting upload purge in %s", intervalDuration)
			time.Sleep(intervalDuration)
		}
	}()
}

// GracefulShutdown allows the app to free any resources before shutdown.
func (app *App) GracefulShutdown(ctx context.Context) error {
	errors := make(chan error)

	go func() {
		errors <- app.db.Close()
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("app shutdown failed: %w", ctx.Err())
	case err := <-errors:
		if err != nil {
			return fmt.Errorf("app shutdown failed: %w", err)
		}
		return nil
	}
}

// DBStats returns the sql.DBStats for the metadata database connection handle.
func (app *App) DBStats() sql.DBStats {
	return app.db.Stats()
}
