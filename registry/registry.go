package registry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/docker/distribution/configuration"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/health"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/handlers"
	"github.com/docker/distribution/registry/listener"
	"github.com/docker/distribution/uuid"
	"github.com/docker/distribution/version"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gitlab.com/gitlab-org/labkit/correlation"
	"gitlab.com/gitlab-org/labkit/errortracking"
	logkit "gitlab.com/gitlab-org/labkit/log"
	"gitlab.com/gitlab-org/labkit/monitoring"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

var tlsLookup = map[string]uint16{
	"":       tls.VersionTLS12,
	"tls1.2": tls.VersionTLS12,
	"tls1.3": tls.VersionTLS13,
}

// ServeCmd is a cobra command for running the registry.
var ServeCmd = &cobra.Command{
	Use:   "serve <config>",
	Short: "`serve` stores and distributes Docker images",
	Long:  "`serve` stores and distributes Docker images.",
	Run: func(cmd *cobra.Command, args []string) {

		// setup context
		ctx := dcontext.WithVersion(dcontext.Background(), version.Version)

		config, err := resolveConfiguration(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			cmd.Usage()
			os.Exit(1)
		}

		registry, err := NewRegistry(ctx, config)
		if err != nil {
			log.Fatalln(err)
		}

		go func() {
			opts := configureMonitoring(config)
			if err := monitoring.Start(opts...); err != nil {
				log.WithError(err).Error("unable to start monitoring service")
			}
		}()

		if err = registry.ListenAndServe(); err != nil {
			log.Fatalln(err)
		}
	},
}

// A Registry represents a complete instance of the registry.
// TODO(aaronl): It might make sense for Registry to become an interface.
type Registry struct {
	config *configuration.Configuration
	app    *handlers.App
	server *http.Server
}

// NewRegistry creates a new registry from a context and configuration struct.
func NewRegistry(ctx context.Context, config *configuration.Configuration) (*Registry, error) {
	var err error
	ctx, err = configureLogging(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("configuring logger: %w", err)
	}

	// inject a logger into the uuid library. warns us if there is a problem
	// with uuid generation under low entropy.
	uuid.Loggerf = dcontext.GetLogger(ctx).Warnf

	app := handlers.NewApp(ctx, config)
	// TODO(aaronl): The global scope of the health checks means NewRegistry
	// can only be called once per process.
	app.RegisterHealthChecks()
	handler := panicHandler(app)
	if handler, err = configureReporting(config, handler); err != nil {
		return nil, fmt.Errorf("configuring reporting services: %w", err)
	}
	handler = alive("/", handler)
	handler = health.Handler(handler)
	if handler, err = configureAccessLogging(config, handler); err != nil {
		return nil, fmt.Errorf("configuring access logger: %w", err)
	}
	handler = correlation.InjectCorrelationID(handler, correlation.WithPropagation())

	server := &http.Server{
		Handler: handler,
	}

	return &Registry{
		app:    app,
		config: config,
		server: server,
	}, nil
}

// Channel to capture singals used to gracefully shutdown the registry.
// It is global to ease unit testing
var quit = make(chan os.Signal, 1)

// ListenAndServe runs the registry's HTTP server.
func (registry *Registry) ListenAndServe() error {
	config := registry.config

	ln, err := listener.NewListener(config.HTTP.Net, config.HTTP.Addr)
	if err != nil {
		return err
	}

	if config.HTTP.TLS.Certificate != "" || config.HTTP.TLS.LetsEncrypt.CacheFile != "" {
		tlsMinVersion, ok := tlsLookup[config.HTTP.TLS.MinimumTLS]
		if !ok {
			return fmt.Errorf("unknown minimum TLS level %q specified for http.tls.minimumtls", config.HTTP.TLS.MinimumTLS)
		}

		if config.HTTP.TLS.MinimumTLS != "" {
			dcontext.GetLogger(registry.app).Infof("restricting TLS to %s or higher", config.HTTP.TLS.MinimumTLS)
		}

		tlsConf := &tls.Config{
			ClientAuth:               tls.NoClientCert,
			NextProtos:               nextProtos(config),
			MinVersion:               tlsMinVersion,
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			},
		}

		if config.HTTP.TLS.LetsEncrypt.CacheFile != "" {
			if config.HTTP.TLS.Certificate != "" {
				return fmt.Errorf("cannot specify both certificate and Let's Encrypt")
			}
			m := &autocert.Manager{
				HostPolicy: autocert.HostWhitelist(config.HTTP.TLS.LetsEncrypt.Hosts...),
				Cache:      autocert.DirCache(config.HTTP.TLS.LetsEncrypt.CacheFile),
				Email:      config.HTTP.TLS.LetsEncrypt.Email,
				Prompt:     autocert.AcceptTOS,
			}
			tlsConf.GetCertificate = m.GetCertificate
			tlsConf.NextProtos = append(tlsConf.NextProtos, acme.ALPNProto)
		} else {
			tlsConf.Certificates = make([]tls.Certificate, 1)
			tlsConf.Certificates[0], err = tls.LoadX509KeyPair(config.HTTP.TLS.Certificate, config.HTTP.TLS.Key)
			if err != nil {
				return err
			}
		}

		if len(config.HTTP.TLS.ClientCAs) != 0 {
			pool := x509.NewCertPool()

			for _, ca := range config.HTTP.TLS.ClientCAs {
				caPem, err := ioutil.ReadFile(ca)
				if err != nil {
					return err
				}

				if ok := pool.AppendCertsFromPEM(caPem); !ok {
					return fmt.Errorf("could not add CA to pool")
				}
			}

			for _, subj := range pool.Subjects() {
				dcontext.GetLogger(registry.app).Debugf("CA Subject: %s", string(subj))
			}

			tlsConf.ClientAuth = tls.RequireAndVerifyClientCert
			tlsConf.ClientCAs = pool
		}

		ln = tls.NewListener(ln, tlsConf)
		dcontext.GetLogger(registry.app).Infof("listening on %v, tls", ln.Addr())
	} else {
		dcontext.GetLogger(registry.app).Infof("listening on %v", ln.Addr())
	}

	// Setup channel to get notified on SIGTERM and interrupt signals.
	signal.Notify(quit, syscall.SIGTERM, os.Interrupt)
	serveErr := make(chan error)

	// Start serving in goroutine and listen for stop signal in main thread
	go func() {
		serveErr <- registry.server.Serve(ln)
	}()

	select {
	case err := <-serveErr:
		return err
	case s := <-quit:
		log := log.WithFields(log.Fields{
			"quit_signal":            s.String(),
			"http_drain_timeout":     registry.config.HTTP.DrainTimeout,
			"database_drain_timeout": registry.config.Database.DrainTimeout,
		})
		log.Info("attempting to stop server gracefully...")

		// shutdown the server with a grace period of configured timeout
		if registry.config.HTTP.DrainTimeout != 0 {
			log.Info("draining http connections")
			ctx, cancel := context.WithTimeout(context.Background(), registry.config.HTTP.DrainTimeout)
			defer cancel()
			if err := registry.server.Shutdown(ctx); err != nil {
				return err
			}
		}

		if registry.config.Database.Enabled {
			log.Info("closing database connections")

			ctx := context.Background()
			var cancel context.CancelFunc

			// Drain database with grace period, rather than waiting indefinitely.
			if registry.config.Database.DrainTimeout != 0 {
				ctx, cancel = context.WithTimeout(ctx, registry.config.Database.DrainTimeout)
				defer cancel()
			}

			if err := registry.app.GracefulShutdown(ctx); err != nil {
				return err
			}
		}

		log.Info("graceful shutdown successful")
		return nil
	}
}

func configureReporting(config *configuration.Configuration, h http.Handler) (http.Handler, error) {
	handler := h

	if config.Reporting.Sentry.Enabled {
		if err := errortracking.Initialize(
			errortracking.WithSentryDSN(config.Reporting.Sentry.DSN),
			errortracking.WithSentryEnvironment(config.Reporting.Sentry.Environment),
			errortracking.WithVersion(version.Version),
		); err != nil {
			return nil, fmt.Errorf("failed to configure Sentry: %w", err)
		}

		handler = errortracking.NewHandler(handler)
	}

	return handler, nil
}

// configureLogging prepares the context with a logger using the configuration.
func configureLogging(ctx context.Context, config *configuration.Configuration) (context.Context, error) {
	// the registry doesn't log to a file, so we can ignore the io.Closer (noop) returned by LabKit (we could also
	// ignore the error, but keeping it for future proofing)
	if _, err := logkit.Initialize(
		logkit.WithFormatter(config.Log.Formatter.String()),
		logkit.WithLogLevel(config.Log.Level.String()),
		logkit.WithOutputName(config.Log.Output.String()),
	); err != nil {
		return nil, err
	}

	if len(config.Log.Fields) > 0 {
		// build up the static fields, if present.
		var fields []interface{}
		for k := range config.Log.Fields {
			fields = append(fields, k)
		}

		ctx = dcontext.WithValues(ctx, config.Log.Fields)
		ctx = dcontext.WithLogger(ctx, dcontext.GetLogger(ctx, fields...))
	}

	return ctx, nil
}

func configureAccessLogging(config *configuration.Configuration, h http.Handler) (http.Handler, error) {
	if config.Log.AccessLog.Disabled {
		return h, nil
	}

	logger := log.New()
	// the registry doesn't log to a file, so we can ignore the io.Closer (noop) returned by LabKit (we could also
	// ignore the error, but keeping it for future proofing)
	if _, err := logkit.Initialize(
		logkit.WithLogger(logger),
		logkit.WithFormatter(config.Log.AccessLog.Formatter.String()),
		logkit.WithOutputName(config.Log.Output.String()),
	); err != nil {
		return nil, err
	}

	return logkit.AccessLogger(h, logkit.WithAccessLogger(logger)), nil
}

func configureMonitoring(config *configuration.Configuration) []monitoring.Option {
	var opts []monitoring.Option
	addr := config.HTTP.Debug.Addr

	if addr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/health", health.StatusHandler)
		log.WithFields(log.Fields{"address": addr, "path": "/debug/health"}).Info("starting health checker")

		opts = []monitoring.Option{
			monitoring.WithServeMux(mux),
			monitoring.WithListenerAddress(addr),
		}

		if config.HTTP.Debug.Prometheus.Enabled {
			opts = append(opts, monitoring.WithMetricsHandlerPattern(config.HTTP.Debug.Prometheus.Path))
			opts = append(opts, monitoring.WithBuildInformation(version.Version, version.BuildTime))
			opts = append(opts, monitoring.WithBuildExtraLabels(map[string]string{
				"package":  version.Package,
				"revision": version.Revision,
			}))
			log.WithFields(log.Fields{"address": addr, "path": config.HTTP.Debug.Prometheus.Path}).Info("starting Prometheus listener")
		} else {
			opts = append(opts, monitoring.WithoutMetrics())
		}

		if config.HTTP.Debug.Pprof.Enabled {
			log.WithFields(log.Fields{"address": addr, "path": "/debug/pprof/"}).Info("starting pprof listener")
		} else {
			opts = append(opts, monitoring.WithoutPprof())
		}
	} else {
		opts = []monitoring.Option{
			monitoring.WithoutMetrics(),
			monitoring.WithoutPprof(),
		}
	}

	if config.Profiling.Stackdriver.Enabled {
		opts = append(opts, monitoring.WithProfilerCredentialsFile(config.Profiling.Stackdriver.KeyFile))
		if err := configureStackdriver(config); err != nil {
			log.WithError(err).Error("failed to configure Stackdriver profiler")
			return opts
		}
		log.Info("starting Stackdriver profiler")
	} else {
		opts = append(opts, monitoring.WithoutContinuousProfiling())
	}

	return opts
}

func configureStackdriver(config *configuration.Configuration) error {
	if !config.Profiling.Stackdriver.Enabled {
		return nil
	}

	// the GITLAB_CONTINUOUS_PROFILING env var (as per the LabKit spec) takes precedence over any application
	// configuration settings and is required to configure the Stackdriver service.
	envVar := "GITLAB_CONTINUOUS_PROFILING"
	var service, serviceVersion, projectID string

	// if it's not set then we must set it based on the registry settings, with URL encoded settings for Stackdriver,
	// see https://pkg.go.dev/gitlab.com/gitlab-org/labkit/monitoring?tab=doc for details.
	if _, ok := os.LookupEnv(envVar); !ok {
		service = config.Profiling.Stackdriver.Service
		serviceVersion = config.Profiling.Stackdriver.ServiceVersion
		projectID = config.Profiling.Stackdriver.ProjectID

		u, err := url.Parse("stackdriver")
		if err != nil {
			// this should never happen
			return fmt.Errorf("failed to parse base URL: %w", err)
		}

		q := u.Query()
		if service != "" {
			q.Add("service", service)
		}
		if serviceVersion != "" {
			q.Add("service_version", serviceVersion)
		}
		if projectID != "" {
			q.Add("project_id", projectID)
		}
		u.RawQuery = q.Encode()

		log.WithFields(log.Fields{"name": envVar, "value": u.String()}).Debug("setting environment variable")
		if err := os.Setenv(envVar, u.String()); err != nil {
			return fmt.Errorf("unable to set environment variable %q: %w", envVar, err)
		}
	}

	return nil
}

// panicHandler add an HTTP handler to web app. The handler recover the happening
// panic. logrus.Panic transmits panic message to pre-config log hooks, which is
// defined in config.yml.
func panicHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Panic(fmt.Sprintf("%v", err))
			}
		}()
		handler.ServeHTTP(w, r)
	})
}

// alive simply wraps the handler with a route that always returns an http 200
// response when the path is matched. If the path is not matched, the request
// is passed to the provided handler. There is no guarantee of anything but
// that the server is up. Wrap with other handlers (such as health.Handler)
// for greater affect.
func alive(path string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

func resolveConfiguration(args []string, opts ...configuration.ParseOption) (*configuration.Configuration, error) {
	var configurationPath string

	if len(args) > 0 {
		configurationPath = args[0]
	} else if os.Getenv("REGISTRY_CONFIGURATION_PATH") != "" {
		configurationPath = os.Getenv("REGISTRY_CONFIGURATION_PATH")
	}

	if configurationPath == "" {
		return nil, fmt.Errorf("configuration path unspecified")
	}

	fp, err := os.Open(configurationPath)
	if err != nil {
		return nil, err
	}

	defer fp.Close()

	config, err := configuration.Parse(fp, opts...)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", configurationPath, err)
	}

	if err := validate(config); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	return config, nil
}

func validate(config *configuration.Configuration) error {
	var errs error
	// Validate redirect section.
	if redirectConfig, ok := config.Storage["redirect"]; ok {
		v, ok := redirectConfig["disable"]
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("'storage.redirect' section must include 'disable' parameter (boolean)"))
		}
		switch v := v.(type) {
		case bool:
		default:
			errs = multierror.Append(errs, fmt.Errorf("invalid type %[1]T for 'storage.redirect.disable' (boolean)", v))
		}
	}

	// Validate migration section.
	if config.Migration.Enabled {
		if !config.Database.Enabled {
			errs = multierror.Append(errs, fmt.Errorf("database must be enabled to migrate"))
		}

		storageParams := config.Storage.Parameters()
		if storageParams == nil {
			storageParams = make(configuration.Parameters)
		}

		// We can only run garbage collection during a migration if we are managing
		// migrated fs data under a separate root directory. Otherwise migrated and
		// unmigrated data will be mixed together and we will not be able to perform
		// garbage collection safely.
		if !config.GC.Disabled {
			if config.Migration.AlternativeRootDirectory == "" ||
				(config.Migration.AlternativeRootDirectory == fmt.Sprintf("%s", storageParams["rootdirectory"])) {
				errs = multierror.Append(errs,
					fmt.Errorf("cannot run online garbage collection during a migration without moving migrated data to new root directory, "+
						"see AlternativeRootDirectory in the Migration section of the configuration documentation"))
			}
		}
	}

	return nil
}

func nextProtos(config *configuration.Configuration) []string {
	switch config.HTTP.HTTP2.Disabled {
	case true:
		return []string{"http/1.1"}
	default:
		return []string{"h2", "http/1.1"}
	}
}

func dbFromConfig(config *configuration.Configuration) (*datastore.DB, error) {
	return datastore.Open(&datastore.DSN{
		Host:        config.Database.Host,
		Port:        config.Database.Port,
		User:        config.Database.User,
		Password:    config.Database.Password,
		DBName:      config.Database.DBName,
		SSLMode:     config.Database.SSLMode,
		SSLCert:     config.Database.SSLCert,
		SSLKey:      config.Database.SSLKey,
		SSLRootCert: config.Database.SSLRootCert,
	},
		datastore.WithLogger(log.WithFields(log.Fields{"database": config.Database.DBName})),
		datastore.WithLogLevel(config.Log.Level),
		datastore.WithPoolConfig(&datastore.PoolConfig{
			MaxIdle:     config.Database.Pool.MaxIdle,
			MaxOpen:     config.Database.Pool.MaxOpen,
			MaxLifetime: config.Database.Pool.MaxLifetime,
		}),
	)
}
