package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/docker/distribution/registry/datastore/models"
	"github.com/opencontainers/go-digest"
)

// ManifestReader is the interface that defines read operations for a Manifest store.
type ManifestReader interface {
	FindAll(ctx context.Context) (models.Manifests, error)
	FindByID(ctx context.Context, id int64) (*models.Manifest, error)
	FindByDigest(ctx context.Context, d digest.Digest) (*models.Manifest, error)
	Count(ctx context.Context) (int, error)
	Config(ctx context.Context, m *models.Manifest) (*models.ManifestConfiguration, error)
	LayerBlobs(ctx context.Context, m *models.Manifest) (models.Blobs, error)
	Lists(ctx context.Context, m *models.Manifest) (models.ManifestLists, error)
	Repositories(ctx context.Context, m *models.Manifest) (models.Repositories, error)
}

// ManifestWriter is the interface that defines write operations for a Manifest store.
type ManifestWriter interface {
	Create(ctx context.Context, m *models.Manifest) error
	Update(ctx context.Context, m *models.Manifest) error
	Mark(ctx context.Context, m *models.Manifest) error
	AssociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error
	DissociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error
	Delete(ctx context.Context, id int64) error
}

// ManifestStore is the interface that a Manifest store should conform to.
type ManifestStore interface {
	ManifestReader
	ManifestWriter
}

// manifestStore is the concrete implementation of a ManifestStore.
type manifestStore struct {
	db Queryer
}

// NewManifestStore builds a new manifest store.
func NewManifestStore(db Queryer) *manifestStore {
	return &manifestStore{db: db}
}

func scanFullManifest(row *sql.Row) (*models.Manifest, error) {
	var digestHex []byte
	m := new(models.Manifest)

	err := row.Scan(&m.ID, &m.SchemaVersion, &m.MediaType, &digestHex, &m.Payload, &m.CreatedAt, &m.MarkedAt)
	if err != nil {
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("error scaning manifest: %w", err)
		}
		return nil, nil
	}
	m.Digest = digest.NewDigestFromBytes(digest.SHA256, digestHex)

	return m, nil
}

func scanFullManifests(rows *sql.Rows) (models.Manifests, error) {
	mm := make(models.Manifests, 0)
	defer rows.Close()

	for rows.Next() {
		var digestHex []byte
		m := new(models.Manifest)

		err := rows.Scan(&m.ID, &m.SchemaVersion, &m.MediaType, &digestHex, &m.Payload, &m.CreatedAt, &m.MarkedAt)
		if err != nil {
			return nil, fmt.Errorf("error scanning manifest: %w", err)
		}
		m.Digest = digest.NewDigestFromBytes(digest.SHA256, digestHex)
		mm = append(mm, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning manifests: %w", err)
	}

	return mm, nil
}

// FindByID finds a Manifest by ID.
func (s *manifestStore) FindByID(ctx context.Context, id int64) (*models.Manifest, error) {
	q := `SELECT id, schema_version, media_type, digest_hex, payload, created_at, marked_at
		FROM manifests WHERE id = $1`

	row := s.db.QueryRowContext(ctx, q, id)

	return scanFullManifest(row)
}

// FindByDigest finds a Manifest by the digest.
func (s *manifestStore) FindByDigest(ctx context.Context, d digest.Digest) (*models.Manifest, error) {
	q := `SELECT id, schema_version, media_type, digest_hex, payload, created_at, marked_at
		FROM manifests WHERE digest_hex = decode($1, 'hex')`

	row := s.db.QueryRowContext(ctx, q, d.Hex())

	return scanFullManifest(row)
}

// FindAll finds all manifests.
func (s *manifestStore) FindAll(ctx context.Context) (models.Manifests, error) {
	q := `SELECT id, schema_version, media_type, digest_hex, payload, created_at, marked_at
		FROM manifests`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("error finding manifests: %w", err)
	}

	return scanFullManifests(rows)
}

// Count counts all manifests.
func (s *manifestStore) Count(ctx context.Context) (int, error) {
	q := "SELECT COUNT(*) FROM manifests"
	var count int

	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return count, fmt.Errorf("error counting manifests: %w", err)
	}

	return count, nil
}

// Config finds the manifest configuration.
func (s *manifestStore) Config(ctx context.Context, m *models.Manifest) (*models.ManifestConfiguration, error) {
	q := `SELECT id, manifest_id, media_type, digest_hex, size, payload, created_at
		FROM manifest_configurations WHERE manifest_id = $1`
	row := s.db.QueryRowContext(ctx, q, m.ID)

	return scanFullManifestConfiguration(row)
}

// LayerBlobs finds layer blobs associated with a manifest, through the `manifest_layers` relationship entity.
func (s *manifestStore) LayerBlobs(ctx context.Context, m *models.Manifest) (models.Blobs, error) {
	q := `SELECT b.id, b.media_type, b.digest_hex, b.size, b.created_at, b.marked_at FROM blobs as b
		JOIN manifest_layers as ml ON ml.blob_id = b.id
		JOIN manifests as m ON m.id = ml.manifest_id
		WHERE m.id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("error finding blobs: %w", err)
	}

	return scanFullBlobs(rows)
}

// Lists finds all manifest lists which reference a manifest, through the ManifestListManifest relationship entity.
func (s *manifestStore) Lists(ctx context.Context, m *models.Manifest) (models.ManifestLists, error) {
	q := `SELECT ml.id, ml.schema_version, ml.media_type, ml.digest_hex, ml.payload, ml.created_at, ml.marked_at
		FROM manifest_lists as ml
		JOIN manifest_list_manifests as mlm ON mlm.manifest_list_id = ml.id
		JOIN manifests as m ON m.id = mlm.manifest_id
		WHERE m.id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("error finding manifest lists: %w", err)
	}

	return scanFullManifestLists(rows)
}

// Repositories finds all repositories which reference a manifest.
func (s *manifestStore) Repositories(ctx context.Context, m *models.Manifest) (models.Repositories, error) {
	q := `SELECT r.id, r.name, r.path, r.parent_id, r.created_at, updated_at FROM repositories as r
		JOIN repository_manifests as rm ON rm.repository_id = r.id
		JOIN manifests as m ON m.id = rm.manifest_id
		WHERE m.id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("error finding repositories: %w", err)
	}

	return scanFullRepositories(rows)
}

// Create saves a new Manifest.
func (s *manifestStore) Create(ctx context.Context, m *models.Manifest) error {
	q := `INSERT INTO manifests (schema_version, media_type, digest_hex, payload)
		VALUES ($1, $2, decode($3, 'hex'), $4) RETURNING id, created_at`

	row := s.db.QueryRowContext(ctx, q, m.SchemaVersion, m.MediaType, m.Digest.Hex(), m.Payload)
	if err := row.Scan(&m.ID, &m.CreatedAt); err != nil {
		return fmt.Errorf("error creating manifest: %w", err)
	}

	return nil
}

// Update updates an existing Manifest.
func (s *manifestStore) Update(ctx context.Context, m *models.Manifest) error {
	q := `UPDATE manifests
		SET (schema_version, media_type, digest_hex, payload) = ($1, $2, decode($3, 'hex'), $4)
		WHERE id = $5`

	res, err := s.db.ExecContext(ctx, q, m.SchemaVersion, m.MediaType, m.Digest.Hex(), m.Payload, m.ID)
	if err != nil {
		return fmt.Errorf("error updating manifest: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error updating manifest: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("manifest not found")
	}

	return nil
}

// Mark marks a Manifest during garbage collection.
func (s *manifestStore) Mark(ctx context.Context, m *models.Manifest) error {
	q := "UPDATE manifests SET marked_at = NOW() WHERE id = $1 RETURNING marked_at"

	if err := s.db.QueryRowContext(ctx, q, m.ID).Scan(&m.MarkedAt); err != nil {
		if err == sql.ErrNoRows {
			return errors.New("manifest not found")
		}
		return fmt.Errorf("error soft deleting manifest: %w", err)
	}

	return nil
}

// AssociateLayerBlob associates a layer blob and a manifest. It does nothing if already associated.
func (s *manifestStore) AssociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error {
	q := `INSERT INTO manifest_layers (manifest_id, blob_id) VALUES ($1, $2)
		ON CONFLICT (manifest_id, blob_id) DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, m.ID, b.ID); err != nil {
		return fmt.Errorf("error associating layer blob: %w", err)
	}

	return nil
}

// DissociateLayerBlob dissociates a layer blob and a manifest. It does nothing if not associated.
func (s *manifestStore) DissociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error {
	q := "DELETE FROM manifest_layers WHERE manifest_id = $1 AND blob_id = $2"

	res, err := s.db.ExecContext(ctx, q, m.ID, b.ID)
	if err != nil {
		return fmt.Errorf("error dissociating layer blob: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("error dissociating layer blob: %w", err)
	}

	return nil
}

// Delete deletes a Manifest.
func (s *manifestStore) Delete(ctx context.Context, id int64) error {
	q := "DELETE FROM manifests WHERE id = $1"

	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("error deleting manifest: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error deleting manifest: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("manifest not found")
	}

	return nil
}
