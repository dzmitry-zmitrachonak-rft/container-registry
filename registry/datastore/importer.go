package datastore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/datastore/models"
	"github.com/docker/distribution/registry/storage/driver"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
)

// Importer populates the registry database with filesystem metadata. This is only meant to be used for an initial
// one-off migration, starting with an empty database.
type Importer struct {
	storageDriver   driver.StorageDriver
	registry        distribution.Namespace
	db              *DB
	repositoryStore *repositoryStore
	manifestStore   *manifestStore
	tagStore        *tagStore
	blobStore       *blobStore

	importDanglingManifests bool
	importDanglingBlobs     bool
	requireEmptyDatabase    bool
	dryRun                  bool
}

// ImporterOption provides functional options for the Importer.
type ImporterOption func(*Importer)

// WithImportDanglingManifests configures the Importer to import all manifests
// rather than only tagged manifests.
func WithImportDanglingManifests(imp *Importer) {
	imp.importDanglingManifests = true
}

// WithImportDanglingBlobs configures the Importer to import all blobs
// rather than only blobs referenced by manifests.
func WithImportDanglingBlobs(imp *Importer) {
	imp.importDanglingBlobs = true
}

// WithRequireEmptyDatabase configures the Importer to stop import unless the
// database being imported to is empty.
func WithRequireEmptyDatabase(imp *Importer) {
	imp.requireEmptyDatabase = true
}

// WithDryRun configures the Importer to use a single transacton which is rolled
// back and the end of an import cycle.
func WithDryRun(imp *Importer) {
	imp.dryRun = true
}

// NewImporter creates a new Importer.
func NewImporter(db *DB, storageDriver driver.StorageDriver, registry distribution.Namespace, opts ...ImporterOption) *Importer {
	imp := &Importer{
		storageDriver: storageDriver,
		registry:      registry,
		db:            db,
	}

	for _, o := range opts {
		o(imp)
	}

	//imp.loadStores(imp.db)

	return imp
}

func (imp *Importer) beginTx(ctx context.Context) (*Tx, error) {
	tx, err := imp.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	imp.loadStores(tx)

	return tx, nil
}

func (imp *Importer) loadStores(db Queryer) {
	imp.manifestStore = NewManifestStore(db)
	imp.blobStore = NewBlobStore(db)
	imp.repositoryStore = NewRepositoryStore(db)
	imp.tagStore = NewTagStore(db)
}

func (imp *Importer) findOrCreateDBManifest(ctx context.Context, dbRepo *models.Repository, m *models.Manifest) (*models.Manifest, error) {
	dbManifest, err := imp.repositoryStore.FindManifestByDigest(ctx, dbRepo, m.Digest)
	if err != nil {
		return nil, fmt.Errorf("searching for manifest: %w", err)
	}

	if dbManifest == nil {
		if err := imp.manifestStore.Create(ctx, m); err != nil {
			return nil, fmt.Errorf("creating manifest: %w", err)
		}
		dbManifest = m
	}

	return dbManifest, nil
}

func (imp *Importer) findOrCreateDBLayer(ctx context.Context, fsRepo distribution.Repository, l *models.Blob) (*models.Blob, error) {
	dbLayer, err := imp.blobStore.FindByDigest(ctx, l.Digest)
	if err != nil {
		return nil, fmt.Errorf("searching for layer blob: %w", err)
	}

	if dbLayer == nil {
		// v1 manifests don't include the layers blob size and media type, so we must Stat the blob to know
		if l.Size == 0 {
			blobStore := fsRepo.Blobs(ctx)
			desc, err := blobStore.Stat(ctx, digest.Digest(l.Digest))
			if err != nil {
				return nil, fmt.Errorf("obtaining blob layer size: %w", err)
			}
			l.Size = desc.Size
			l.MediaType = desc.MediaType
		}

		if err := imp.blobStore.Create(ctx, l); err != nil {
			return nil, fmt.Errorf("creating layer blob: %w", err)
		}
		dbLayer = l
	}

	return dbLayer, nil
}

func (imp *Importer) findOrCreateDBManifestConfigBlob(ctx context.Context, d distribution.Descriptor, payload []byte) (*models.Blob, error) {
	dbBlob, err := imp.blobStore.FindByDigest(ctx, d.Digest)
	if err != nil {
		return nil, fmt.Errorf("searching for configuration blob: %w", err)
	}
	if dbBlob == nil {
		dbBlob = &models.Blob{
			MediaType: d.MediaType,
			Digest:    d.Digest,
			Size:      d.Size,
		}
		if err := imp.blobStore.Create(ctx, dbBlob); err != nil {
			return nil, err
		}
	}

	return dbBlob, nil
}

func (imp *Importer) findOrCreateDBTag(ctx context.Context, dbRepo *models.Repository, dbTag *models.Tag) {
	log := logrus.WithField("tagName", dbTag.Name)

	foundTag, err := imp.repositoryStore.FindTagByName(ctx, dbRepo, dbTag.Name)
	if err != nil {
		log.WithError(err).Error("finding repository tag")
		return
	}

	if foundTag != nil {
		log.Info("tag already present in database")
		return
	}

	if err := imp.tagStore.CreateOrUpdate(ctx, dbTag); err != nil {
		log.WithError(err).Error("creating tag")
		return
	}
}

func (imp *Importer) importLayer(ctx context.Context, fsRepo distribution.Repository, dbRepo *models.Repository, dbManifest *models.Manifest, l *models.Blob) error {
	dbLayer, err := imp.findOrCreateDBLayer(ctx, fsRepo, l)
	if err != nil {
		return err
	}
	if err := imp.manifestStore.AssociateLayerBlob(ctx, dbManifest, dbLayer); err != nil {
		return fmt.Errorf("associating layer blob with manifest: %w", err)
	}

	if err := imp.repositoryStore.LinkBlob(ctx, dbRepo, dbLayer.Digest); err != nil {
		return fmt.Errorf("linking layer blob to repository: %w", err)
	}

	return nil
}

func (imp *Importer) migrateObject(ctx context.Context, src, dst string) error {
	return imp.storageDriver.Move(ctx, src, dst)
}

func (imp *Importer) migrateBlob(ctx context.Context, repo distribution.Repository, layer distribution.Descriptor) error {
	log := logrus.WithField("digest", layer.Digest)

	start := time.Now()

	// copy blob data
	src := fmt.Sprintf(
		"docker/registry/v2/blobs/%s/%s/%s/data",
		layer.Digest.Algorithm().String(),
		layer.Digest.Hex()[0:2],
		layer.Digest.Hex(),
	)
	dst := "gitlab/" + src
	if err := imp.migrateObject(ctx, src, dst); err != nil {
		return err
	}

	log.WithField("duration_s", time.Since(start).Seconds()).Info("blob migrated")
	return nil
}

func (imp *Importer) importLayers(ctx context.Context, fsRepo distribution.Repository, dbRepo *models.Repository, dbManifest *models.Manifest, fsLayers []distribution.Descriptor) error {
	total := len(fsLayers)
	for i, fsLayer := range fsLayers {
		log := logrus.WithFields(logrus.Fields{
			"digest":     fsLayer.Digest,
			"media_type": fsLayer.MediaType,
			"size":       fsLayer.Size,
			"count":      i + 1,
			"total":      total,
		})
		log.Info("importing layer")

		if err := imp.migrateBlob(ctx, fsRepo, fsLayer); err != nil {
			log.WithError(err).Error("migrating layer")
			os.Exit(1)
		}

		err := imp.importLayer(ctx, fsRepo, dbRepo, dbManifest, &models.Blob{
			MediaType: fsLayer.MediaType,
			Digest:    fsLayer.Digest,
			Size:      fsLayer.Size,
		})
		if err != nil {
			log.WithError(err).Error("importing layer")
			continue
		}
	}

	return nil
}

type v2Manifest interface {
	distribution.Manifest
	version() manifest.Versioned
	config() distribution.Descriptor
	layers() []distribution.Descriptor
}

type schema2Extended struct {
	*schema2.DeserializedManifest
}

func (m *schema2Extended) version() manifest.Versioned       { return m.Versioned }
func (m *schema2Extended) config() distribution.Descriptor   { return m.Config }
func (m *schema2Extended) layers() []distribution.Descriptor { return m.Layers }

type ociExtended struct {
	*ocischema.DeserializedManifest
}

func (m *ociExtended) version() manifest.Versioned {
	// Helm chart manifests do not include a mediatype, set them to oci.
	if m.Versioned.MediaType == "" {
		m.Versioned.MediaType = v1.MediaTypeImageManifest
	}

	return m.Versioned
}

func (m *ociExtended) config() distribution.Descriptor   { return m.Config }
func (m *ociExtended) layers() []distribution.Descriptor { return m.Layers }

func (imp *Importer) importV2Manifest(ctx context.Context, fsRepo distribution.Repository, dbRepo *models.Repository, m v2Manifest, dgst digest.Digest) (*models.Manifest, error) {
	_, payload, err := m.Payload()
	if err != nil {
		return nil, fmt.Errorf("error parsing manifest payload: %w", err)
	}

	// get configuration blob payload
	blobStore := fsRepo.Blobs(ctx)
	configPayload, err := blobStore.Get(ctx, m.config().Digest)
	if err != nil {
		return nil, fmt.Errorf("error obtaining configuration payload: %w", err)
	}

	dbConfigBlob, err := imp.findOrCreateDBManifestConfigBlob(ctx, m.config(), configPayload)
	if err != nil {
		return nil, err
	}

	// link configuration to repository
	if err := imp.repositoryStore.LinkBlob(ctx, dbRepo, dbConfigBlob.Digest); err != nil {
		return nil, fmt.Errorf("error associating configuration blob with repository: %w", err)
	}

	// find or create DB manifest
	dbManifest, err := imp.findOrCreateDBManifest(ctx, dbRepo, &models.Manifest{
		RepositoryID:  dbRepo.ID,
		SchemaVersion: m.version().SchemaVersion,
		MediaType:     m.version().MediaType,
		Digest:        dgst,
		Payload:       payload,
		Configuration: &models.Configuration{
			MediaType: dbConfigBlob.MediaType,
			Digest:    dbConfigBlob.Digest,
			Payload:   configPayload,
		},
	})
	if err != nil {
		return nil, err
	}

	// import manifest layers
	if err := imp.importLayers(ctx, fsRepo, dbRepo, dbManifest, m.layers()); err != nil {
		return nil, fmt.Errorf("error importing layers: %w", err)
	}

	return dbManifest, nil
}

func (imp *Importer) importManifestList(ctx context.Context, fsRepo distribution.Repository, dbRepo *models.Repository, ml *manifestlist.DeserializedManifestList, dgst digest.Digest) (*models.Manifest, error) {
	_, payload, err := ml.Payload()
	if err != nil {
		return nil, fmt.Errorf("error parsing manifest list payload: %w", err)
	}

	// Media type can be either Docker (`application/vnd.docker.distribution.manifest.list.v2+json`) or OCI (empty).
	// We need to make it explicit if empty, otherwise we're not able to distinguish between media types.
	mediaType := ml.MediaType
	if mediaType == "" {
		mediaType = v1.MediaTypeImageIndex
	}

	// create manifest list on DB
	dbManifestList, err := imp.findOrCreateDBManifest(ctx, dbRepo, &models.Manifest{
		RepositoryID:  dbRepo.ID,
		SchemaVersion: ml.SchemaVersion,
		MediaType:     mediaType,
		Digest:        dgst,
		Payload:       payload,
	})
	if err != nil {
		return nil, fmt.Errorf("creating manifest list: %w", err)
	}

	manifestService, err := fsRepo.Manifests(ctx)
	if err != nil {
		return nil, fmt.Errorf("constructing manifest service: %w", err)
	}

	// import manifests in list
	total := len(ml.Manifests)
	for i, m := range ml.Manifests {
		fsManifest, err := manifestService.Get(ctx, m.Digest)
		if err != nil {
			logrus.WithError(err).Error("retrieving manifest")
			continue
		}

		logrus.WithFields(logrus.Fields{
			"digest": m.Digest.String(),
			"count":  i + 1,
			"total":  total,
			"type":   fmt.Sprintf("%T", fsManifest),
		}).Info("importing manifest from list")

		dbManifest, err := imp.importManifest(ctx, fsRepo, dbRepo, fsManifest, m.Digest)
		if err != nil {
			logrus.WithError(err).Error("importing manifest")
			continue
		}

		if err := imp.manifestStore.AssociateManifest(ctx, dbManifestList, dbManifest); err != nil {
			logrus.WithError(err).Error("associating manifest and manifest list")
			continue
		}
	}

	return dbManifestList, nil
}

func (imp *Importer) importManifest(ctx context.Context, fsRepo distribution.Repository, dbRepo *models.Repository, m distribution.Manifest, dgst digest.Digest) (*models.Manifest, error) {
	switch fsManifest := m.(type) {
	case *schema1.SignedManifest:
		return nil, distribution.ErrSchemaV1Unsupported
	case *schema2.DeserializedManifest:
		return imp.importV2Manifest(ctx, fsRepo, dbRepo, &schema2Extended{fsManifest}, dgst)
	case *ocischema.DeserializedManifest:
		return imp.importV2Manifest(ctx, fsRepo, dbRepo, &ociExtended{fsManifest}, dgst)
	default:
		return nil, fmt.Errorf("unknown manifest class")
	}
}

func (imp *Importer) importManifests(ctx context.Context, fsRepo distribution.Repository, dbRepo *models.Repository) error {
	manifestService, err := fsRepo.Manifests(ctx)
	if err != nil {
		return fmt.Errorf("constructing manifest service: %w", err)
	}
	manifestEnumerator, ok := manifestService.(distribution.ManifestEnumerator)
	if !ok {
		return fmt.Errorf("converting ManifestService into ManifestEnumerator")
	}

	index := 0
	err = manifestEnumerator.Enumerate(ctx, func(dgst digest.Digest) error {
		index++

		m, err := manifestService.Get(ctx, dgst)
		if err != nil {
			return fmt.Errorf("retrieving manifest %q: %w", dgst, err)
		}

		log := logrus.WithFields(logrus.Fields{"digest": dgst, "count": index, "type": fmt.Sprintf("%T", m)})

		switch fsManifest := m.(type) {
		case *manifestlist.DeserializedManifestList:
			log.Info("importing manifest list")
			_, err = imp.importManifestList(ctx, fsRepo, dbRepo, fsManifest, dgst)
		default:
			log.Info("importing manifest")
			_, err = imp.importManifest(ctx, fsRepo, dbRepo, fsManifest, dgst)
			if errors.Is(err, distribution.ErrSchemaV1Unsupported) {
				logrus.WithError(err).Error("importing manifest")
				return nil
			}
		}

		return err
	})

	return err
}

func (imp *Importer) importTags(ctx context.Context, fsRepo distribution.Repository, dbRepo *models.Repository) error {
	manifestService, err := fsRepo.Manifests(ctx)
	if err != nil {
		return fmt.Errorf("constructing manifest service: %w", err)
	}

	tagService := fsRepo.Tags(ctx)
	fsTags, err := tagService.All(ctx)
	if err != nil {
		return fmt.Errorf("reading tags: %w", err)
	}

	total := len(fsTags)

	for i, fsTag := range fsTags {
		log := logrus.WithFields(logrus.Fields{"name": fsTag, "count": i + 0, "total": total})

		// read tag details from the filesystem
		desc, err := tagService.Get(ctx, fsTag)
		if err != nil {
			log.WithError(err).Error("reading tag details")
			continue
		}

		log = log.WithField("target", desc.Digest)
		log.Info("importing tag")

		dbTag := &models.Tag{Name: fsTag, RepositoryID: dbRepo.ID}

		// Find corresponding manifest in DB or filesystem.
		var dbManifest *models.Manifest
		dbManifest, err = imp.repositoryStore.FindManifestByDigest(ctx, dbRepo, desc.Digest)
		if err != nil {
			log.WithError(err).Error("finding tag manifest")
			continue
		}
		if dbManifest == nil {
			m, err := manifestService.Get(ctx, desc.Digest)
			if err != nil {
				log.WithError(err).Errorf("retrieving manifest %q", desc.Digest)
				continue
			}

			switch fsManifest := m.(type) {
			case *manifestlist.DeserializedManifestList:
				log.Info("importing manifest list")
				dbManifest, err = imp.importManifestList(ctx, fsRepo, dbRepo, fsManifest, desc.Digest)
			default:
				log.Info("importing manifest")
				dbManifest, err = imp.importManifest(ctx, fsRepo, dbRepo, fsManifest, desc.Digest)
			}
			if err != nil {
				log.WithError(err).Error("importing manifest")
				continue
			}
		}

		dbTag.ManifestID = dbManifest.ID

		imp.findOrCreateDBTag(ctx, dbRepo, dbTag)
	}

	return nil
}

func (imp *Importer) importRepository(ctx context.Context, path string) error {
	named, err := reference.WithName(path)
	if err != nil {
		return fmt.Errorf("parsing repository name: %w", err)
	}
	fsRepo, err := imp.registry.Repository(ctx, named)
	if err != nil {
		return fmt.Errorf("constructing repository: %w", err)
	}

	// Find or create repository.
	var dbRepo *models.Repository

	dbRepo, err = imp.repositoryStore.FindByPath(ctx, path)
	if err != nil {
		return fmt.Errorf("checking for existence of repository: %w", err)
	}

	if dbRepo == nil {
		if dbRepo, err = imp.repositoryStore.CreateByPath(ctx, path); err != nil {
			return fmt.Errorf("importing repository: %w", err)
		}
	}

	if imp.importDanglingManifests {
		// import all repository manifests
		if err := imp.importManifests(ctx, fsRepo, dbRepo); err != nil {
			return fmt.Errorf("importing manifests: %w", err)
		}
	}

	// import repository tags and associated manifests
	if err := imp.importTags(ctx, fsRepo, dbRepo); err != nil {
		return fmt.Errorf("importing tags: %w", err)
	}

	return nil
}

func (imp *Importer) countRows(ctx context.Context) (map[string]int, error) {
	numRepositories, err := imp.repositoryStore.Count(ctx)
	if err != nil {
		return nil, err
	}
	numManifests, err := imp.manifestStore.Count(ctx)
	if err != nil {
		return nil, err
	}
	numBlobs, err := imp.blobStore.Count(ctx)
	if err != nil {
		return nil, err
	}
	numTags, err := imp.tagStore.Count(ctx)
	if err != nil {
		return nil, err
	}

	count := map[string]int{
		"repositories": numRepositories,
		"manifests":    numManifests,
		"blobs":        numBlobs,
		"tags":         numTags,
	}

	return count, nil
}

func (imp *Importer) isDatabaseEmpty(ctx context.Context) (bool, error) {
	counters, err := imp.countRows(ctx)
	if err != nil {
		return false, err
	}

	for _, c := range counters {
		if c > 0 {
			return false, nil
		}
	}

	return true, nil
}

// ImportAll populates the registry database with metadata from all repositories in the storage backend.
func (imp *Importer) ImportAll(ctx context.Context) error {
	var err error
	var tx *Tx

	// Create a single transaction and roll it back at the end for dry runs.
	if imp.dryRun {
		tx, err = imp.beginTx(ctx)
		if err != nil {
			return fmt.Errorf("begin dry run transaction: %w", err)
		}
		defer tx.Rollback()
	}

	start := time.Now()
	log := logrus.New()
	log.Info("starting metadata import")

	if imp.requireEmptyDatabase {
		empty, err := imp.isDatabaseEmpty(ctx)
		if err != nil {
			return fmt.Errorf("checking if database is empty: %w", err)
		}
		if !empty {
			return errors.New("non-empty database")
		}
	}

	if imp.importDanglingBlobs {
		var index int
		blobStart := time.Now()
		log.Info("importing all blobs")
		err := imp.registry.Blobs().Enumerate(ctx, func(desc distribution.Descriptor) error {
			index++
			log := log.WithFields(logrus.Fields{"digest": desc.Digest, "count": index, "size": desc.Size})
			log.Info("importing blob")

			dbBlob, err := imp.blobStore.FindByDigest(ctx, desc.Digest)
			if err != nil {
				return fmt.Errorf("checking for existence of blob: %w", err)
			}

			if dbBlob == nil {
				if err := imp.blobStore.Create(ctx, &models.Blob{MediaType: "application/octet-stream", Digest: desc.Digest, Size: desc.Size}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("importing blobs: %w", err)
		}

		blobEnd := time.Since(blobStart).Seconds()
		log.WithField("duration_s", blobEnd).Info("blob import complete")
	}

	repositoryEnumerator, ok := imp.registry.(distribution.RepositoryEnumerator)
	if !ok {
		return errors.New("error building repository enumerator")
	}

	index := 0
	err = repositoryEnumerator.Enumerate(ctx, func(path string) error {
		if !imp.dryRun {
			tx, err = imp.beginTx(ctx)
			if err != nil {
				return fmt.Errorf("begin repository transaction: %w", err)
			}
			defer tx.Rollback()
		}

		index++
		repoStart := time.Now()
		log := logrus.WithFields(logrus.Fields{"path": path, "count": index})
		log.Info("importing repository")

		if err := imp.importRepository(ctx, path); err != nil {
			log.WithError(err).Error("error importing repository")
			// if the storage driver failed to find a repository path (usually due to missing `_manifests/revisions`
			// or `_manifests/tags` folders) continue to the next one, otherwise stop as the error is unknown.
			if !(errors.As(err, &driver.PathNotFoundError{}) || errors.As(err, &distribution.ErrRepositoryUnknown{})) {
				return err
			}
			return nil
		}

		repoEnd := time.Since(repoStart).Seconds()
		log.WithField("duration_s", repoEnd).Info("repository import complete")

		if !imp.dryRun {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit repository transaction: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	if !imp.dryRun {
		// reset stores to use the main connection handler instead of the last (committed/rolled back) transaction
		imp.loadStores(imp.db)
	}

	counters, err := imp.countRows(ctx)
	if err != nil {
		logrus.WithError(err).Error("counting table rows")
	}

	logCounters := make(map[string]interface{}, len(counters))
	for t, n := range counters {
		logCounters[t] = n
	}

	t := time.Since(start).Seconds()
	logrus.WithField("duration_s", t).WithFields(logCounters).Info("metadata import complete")

	return err
}

// Import populates the registry database with metadata from a specific repository in the storage backend.
func (imp *Importer) Import(ctx context.Context, path string) error {
	//tx, err := imp.beginTx(ctx)
	//if err != nil {
	//	return fmt.Errorf("begin repository transaction: %w", err)
	//}
	//defer tx.Rollback()

	//start := time.Now()
	logrus.Info("starting metadata import")

	layers := []distribution.Descriptor{
		{Digest: "sha256:7a1d2072582baedec1ea52969daabfe9b33342085dd155220c32d89c1f736110"},
		{Digest: "sha256:49c046dddc448c5ec1737ff00a0bdd79deeec8e80995e02afab6623c703e929c"},
		{Digest: "sha256:03adee120fba60dba35efb6a95a4d450b71f2ba1c0f86e359b86630a7fdeaf44"},
		{Digest: "sha256:4f4fb700ef54461cfa02571ae0db9a0dc1e0cdb5577484a6d75e68dc38e8acc1"},
	}

	for _, l := range layers {
		if err := imp.migrateBlob(ctx, nil, l); err != nil {
			return err
		}
	}

	//if imp.requireEmptyDatabase {
	//	empty, err := imp.isDatabaseEmpty(ctx)
	//	if err != nil {
	//		return fmt.Errorf("checking if database is empty: %w", err)
	//	}
	//	if !empty {
	//		return errors.New("non-empty database")
	//	}
	//}
	//
	//log := logrus.WithField("path", path)
	//log.Info("importing repository")
	//
	//if err := imp.importRepository(ctx, path); err != nil {
	//	log.WithError(err).Error("error importing repository")
	//	return err
	//}
	//
	//counters, err := imp.countRows(ctx)
	//if err != nil {
	//	logrus.WithError(err).Error("error counting table rows")
	//}
	//
	//logCounters := make(map[string]interface{}, len(counters))
	//for t, n := range counters {
	//	logCounters[t] = n
	//}
	//
	//t := time.Since(start).Seconds()
	//logrus.WithField("duration_s", t).WithFields(logCounters).Info("metadata import complete")
	//
	//if !imp.dryRun {
	//	if err := tx.Commit(); err != nil {
	//		return fmt.Errorf("commit repository transaction: %w", err)
	//	}
	//}

	return nil
}
