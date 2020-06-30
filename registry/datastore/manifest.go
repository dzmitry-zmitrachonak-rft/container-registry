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
	Layers(ctx context.Context, m *models.Manifest) (models.Layers, error)
	AssociatedManifests(ctx context.Context, m *models.Manifest) (models.Manifests, error)
	Repositories(ctx context.Context, m *models.Manifest) (models.Repositories, error)
}

// ManifestWriter is the interface that defines write operations for a Manifest store.
type ManifestWriter interface {
	Create(ctx context.Context, m *models.Manifest) error
	Update(ctx context.Context, m *models.Manifest) error
	Mark(ctx context.Context, m *models.Manifest) error
	AssociateLayer(ctx context.Context, m *models.Manifest, l *models.Layer) error
	DissociateLayer(ctx context.Context, m *models.Manifest, l *models.Layer) error
	AssociateManifest(ctx context.Context, parent *models.Manifest, child *models.Manifest) error
	DissociateManifest(ctx context.Context, parent *models.Manifest, child *models.Manifest) error
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

// Layers finds layers associated with a manifest, through the ManifestLayer relationship entity.
func (s *manifestStore) Layers(ctx context.Context, m *models.Manifest) (models.Layers, error) {
	q := `SELECT l.id, l.media_type, l.digest_hex, l.size, l.created_at, l.marked_at FROM layers as l
		JOIN manifest_layers as ml ON ml.layer_id = l.id
		JOIN manifests as m ON m.id = ml.manifest_id
		WHERE m.id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("error finding layers: %w", err)
	}

	return scanFullLayers(rows)
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

// AssociatedManifests finds all manifests associated with a manifest list (if any).
func (s *manifestStore) AssociatedManifests(ctx context.Context, m *models.Manifest) (models.Manifests, error) {
	q := `SELECT m.id, m.schema_version, m.media_type, m.digest_hex, m.payload, m.created_at, m.marked_at
		FROM manifests AS m
		JOIN manifest_list_manifests AS mlm ON mlm.parent_id = $1
		WHERE m.id = mlm.child_id`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("error finding associated manifests: %w", err)
	}

	return scanFullManifests(rows)
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

// AssociateManifest associates a parent manifest (manifest list) and a child manifest (manifest or manifest list). It
// does nothing if already associated.
func (s *manifestStore) AssociateManifest(ctx context.Context, parent *models.Manifest, child *models.Manifest) error {
	q := `INSERT INTO manifest_list_manifests (parent_id, child_id) VALUES ($1, $2)
		ON CONFLICT (parent_id, child_id) DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, parent.ID, child.ID); err != nil {
		return fmt.Errorf("error associating manifest: %w", err)
	}

	return nil
}

// DissociateManifest dissociates a parent manifest (manifest list) and a child manifest (manifest or manifest list). It
// does nothing if not associated.
func (s *manifestStore) DissociateManifest(ctx context.Context, parent *models.Manifest, child *models.Manifest) error {
	q := "DELETE FROM manifest_list_manifests WHERE parent_id = $1 AND child_id = $2"

	res, err := s.db.ExecContext(ctx, q, parent.ID, child.ID)
	if err != nil {
		return fmt.Errorf("error dissociating manifest: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("error dissociating manifest: %w", err)
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

// AssociateLayer associates a layer and a manifest. It does nothing if already associated.
func (s *manifestStore) AssociateLayer(ctx context.Context, m *models.Manifest, l *models.Layer) error {
	q := `INSERT INTO manifest_layers (manifest_id, layer_id) VALUES ($1, $2)
		ON CONFLICT (manifest_id, layer_id) DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, m.ID, l.ID); err != nil {
		return fmt.Errorf("error associating layer: %w", err)
	}

	return nil
}

// DissociateLayer dissociates a layer and a manifest. It does nothing if not associated.
func (s *manifestStore) DissociateLayer(ctx context.Context, m *models.Manifest, l *models.Layer) error {
	q := "DELETE FROM manifest_layers WHERE manifest_id = $1 AND layer_id = $2"

	res, err := s.db.ExecContext(ctx, q, m.ID, l.ID)
	if err != nil {
		return fmt.Errorf("error dissociating layer: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("error dissociating layer: %w", err)
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
