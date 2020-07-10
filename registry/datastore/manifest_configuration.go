package datastore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker/distribution/registry/datastore/models"
	"github.com/opencontainers/go-digest"
)

// ManifestConfigurationReader is the interface that defines read operations for a manifest configuration store.
type ManifestConfigurationReader interface {
	FindAll(ctx context.Context) (models.ManifestConfigurations, error)
	FindByID(ctx context.Context, id int64) (*models.ManifestConfiguration, error)
	FindByDigest(ctx context.Context, d digest.Digest) (*models.ManifestConfiguration, error)
	Count(ctx context.Context) (int, error)
	Manifest(ctx context.Context, c *models.ManifestConfiguration) (*models.Manifest, error)
}

// ManifestConfigurationWriter is the interface that defines write operations for a manifest configuration store.
type ManifestConfigurationWriter interface {
	Create(ctx context.Context, c *models.ManifestConfiguration) error
	Update(ctx context.Context, c *models.ManifestConfiguration) error
	Delete(ctx context.Context, id int64) error
}

// ManifestConfigurationStore is the interface that a manifest configuration store should conform to.
type ManifestConfigurationStore interface {
	ManifestConfigurationReader
	ManifestConfigurationWriter
}

// manifestConfigurationStore is the concrete implementation of a ManifestConfigurationStore.
type manifestConfigurationStore struct {
	db Queryer
}

// NewManifestConfigurationStore builds a new repository store.
func NewManifestConfigurationStore(db Queryer) *manifestConfigurationStore {
	return &manifestConfigurationStore{db: db}
}

func scanFullManifestConfiguration(row *sql.Row) (*models.ManifestConfiguration, error) {
	var digestAlgorithm DigestAlgorithm
	var digestHex []byte

	c := new(models.ManifestConfiguration)
	err := row.Scan(&c.ID, &c.ManifestID, &c.BlobID, &c.MediaType, &digestAlgorithm, &digestHex, &c.Size, &c.Payload, &c.CreatedAt)
	if err != nil {
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("error scaning manifest configuration: %w", err)
		}
		return nil, nil
	}

	alg, err := digestAlgorithm.Parse()
	if err != nil {
		return nil, err
	}
	c.Digest = digest.NewDigestFromBytes(alg, digestHex)

	return c, nil
}

func scanFullManifestConfigurations(rows *sql.Rows) (models.ManifestConfigurations, error) {
	cc := make(models.ManifestConfigurations, 0)
	defer rows.Close()

	for rows.Next() {
		var digestAlgorithm DigestAlgorithm
		var digestHex []byte

		c := new(models.ManifestConfiguration)
		err := rows.Scan(&c.ID, &c.ManifestID, &c.BlobID, &c.MediaType, &digestAlgorithm, &digestHex, &c.Size, &c.Payload, &c.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("error scanning manifest configuration: %w", err)
		}

		alg, err := digestAlgorithm.Parse()
		if err != nil {
			return nil, err
		}
		c.Digest = digest.NewDigestFromBytes(alg, digestHex)

		cc = append(cc, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning manifest configurations: %w", err)
	}

	return cc, nil
}

// FindByID finds a manifest configuration by ID.
func (s *manifestConfigurationStore) FindByID(ctx context.Context, id int64) (*models.ManifestConfiguration, error) {
	q := `SELECT mc.id, mc.manifest_id, mc.blob_id, b.media_type, b.digest_algorithm, b.digest_hex, b.size, mc.payload, mc.created_at
		FROM manifest_configurations AS mc
		JOIN blobs AS b ON mc.blob_id = b.id
		WHERE mc.id = $1`
	row := s.db.QueryRowContext(ctx, q, id)

	return scanFullManifestConfiguration(row)
}

// FindByDigest finds a manifest configuration by the digest.
func (s *manifestConfigurationStore) FindByDigest(ctx context.Context, d digest.Digest) (*models.ManifestConfiguration, error) {
	q := `SELECT mc.id, mc.manifest_id, mc.blob_id, b.media_type, b.digest_algorithm, b.digest_hex, b.size, mc.payload, mc.created_at
		FROM manifest_configurations AS mc
		JOIN blobs AS b ON mc.blob_id = b.id
		WHERE b.digest_algorithm = $1 AND b.digest_hex = decode($2, 'hex')`

	alg, err := NewDigestAlgorithm(d.Algorithm())
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, q, alg, d.Hex())

	return scanFullManifestConfiguration(row)
}

// FindAll finds all manifest configurations.
func (s *manifestConfigurationStore) FindAll(ctx context.Context) (models.ManifestConfigurations, error) {
	q := `SELECT mc.id, mc.manifest_id, mc.blob_id, b.media_type, b.digest_algorithm, b.digest_hex, b.size, mc.payload, mc.created_at
		FROM manifest_configurations AS mc
		JOIN blobs AS b ON mc.blob_id = b.id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("error finding manifest configurations: %w", err)
	}

	return scanFullManifestConfigurations(rows)
}

// Count counts all manifest configurations.
func (s *manifestConfigurationStore) Count(ctx context.Context) (int, error) {
	q := "SELECT COUNT(*) FROM manifest_configurations"
	var count int

	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return count, fmt.Errorf("error counting manifest configurations: %w", err)
	}

	return count, nil
}

// Manifest finds the manifest that the configuration belongs to.
func (s *manifestConfigurationStore) Manifest(ctx context.Context, c *models.ManifestConfiguration) (*models.Manifest, error) {
	q := `SELECT id, schema_version, media_type, digest_algorithm, digest_hex, payload, created_at, marked_at
		FROM manifests WHERE id = $1`

	row := s.db.QueryRowContext(ctx, q, c.ManifestID)

	return scanFullManifest(row)
}

// Create saves a new manifest configuration.
func (s *manifestConfigurationStore) Create(ctx context.Context, c *models.ManifestConfiguration) error {
	q := `INSERT INTO manifest_configurations (manifest_id, blob_id, payload)
		VALUES ($1, $2, $3) RETURNING id, created_at`

	row := s.db.QueryRowContext(ctx, q, c.ManifestID, c.BlobID, c.Payload)
	if err := row.Scan(&c.ID, &c.CreatedAt); err != nil {
		return fmt.Errorf("error creating manifest configuration: %w", err)
	}

	return nil
}

// Update updates an existing manifest configuration.
func (s *manifestConfigurationStore) Update(ctx context.Context, c *models.ManifestConfiguration) error {
	q := `UPDATE manifest_configurations
		SET (manifest_id, blob_id, payload) = ($1, $2, $3) WHERE id = $4`

	res, err := s.db.ExecContext(ctx, q, c.ManifestID, c.BlobID, c.Payload, c.ID)
	if err != nil {
		return fmt.Errorf("error updating manifest configuration: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error updating manifest configuration: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("manifest configuration not found")
	}

	return nil
}

// Delete deletes a manifest configuration.
func (s *manifestConfigurationStore) Delete(ctx context.Context, id int64) error {
	q := "DELETE FROM manifest_configurations WHERE id = $1"

	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("error deleting manifest configuration: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error deleting manifest configuration: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("manifest configuration not found")
	}

	return nil
}
