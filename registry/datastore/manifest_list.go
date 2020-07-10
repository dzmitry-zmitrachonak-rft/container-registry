package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/docker/distribution/registry/datastore/models"
	"github.com/opencontainers/go-digest"
)

// ManifestListReader is the interface that defines read operations for a manifest list store.
type ManifestListReader interface {
	FindAll(ctx context.Context) (models.ManifestLists, error)
	FindByID(ctx context.Context, id int64) (*models.ManifestList, error)
	FindByDigest(ctx context.Context, d digest.Digest) (*models.ManifestList, error)
	Count(ctx context.Context) (int, error)
	Manifests(ctx context.Context, ml *models.ManifestList) (models.Manifests, error)
	Repositories(ctx context.Context, m *models.ManifestList) (models.Repositories, error)
}

// ManifestListWriter is the interface that defines write operations for a manifest list store.
type ManifestListWriter interface {
	Create(ctx context.Context, ml *models.ManifestList) error
	Update(ctx context.Context, ml *models.ManifestList) error
	Mark(ctx context.Context, ml *models.ManifestList) error
	AssociateManifest(ctx context.Context, ml *models.ManifestList, m *models.Manifest) error
	DissociateManifest(ctx context.Context, ml *models.ManifestList, m *models.Manifest) error
	Delete(ctx context.Context, id int64) error
}

// ManifestListStore is the interface that a manifest list store should conform to.
type ManifestListStore interface {
	ManifestListReader
	ManifestListWriter
}

// manifestListStore is the concrete implementation of a ManifestListStore.
type manifestListStore struct {
	db Queryer
}

// NewManifestListStore builds a new manifest list store.
func NewManifestListStore(db Queryer) *manifestListStore {
	return &manifestListStore{db: db}
}

func scanFullManifestList(row *sql.Row) (*models.ManifestList, error) {
	var digestAlgorithm DigestAlgorithm
	var digestHex []byte
	ml := new(models.ManifestList)

	err := row.Scan(&ml.ID, &ml.SchemaVersion, &ml.MediaType, &digestAlgorithm, &digestHex, &ml.Payload, &ml.CreatedAt, &ml.MarkedAt)
	if err != nil {
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("error scaning manifest list: %w", err)
		}
		return nil, nil
	}

	alg, err := digestAlgorithm.Parse()
	if err != nil {
		return nil, err
	}
	ml.Digest = digest.NewDigestFromBytes(alg, digestHex)

	return ml, nil
}

func scanFullManifestLists(rows *sql.Rows) (models.ManifestLists, error) {
	mls := make(models.ManifestLists, 0)
	defer rows.Close()

	for rows.Next() {
		var digestAlgorithm DigestAlgorithm
		var digestHex []byte
		ml := new(models.ManifestList)

		err := rows.Scan(&ml.ID, &ml.SchemaVersion, &ml.MediaType, &digestAlgorithm, &digestHex, &ml.Payload, &ml.CreatedAt, &ml.MarkedAt)
		if err != nil {
			return nil, fmt.Errorf("error scanning manifest list: %w", err)
		}

		alg, err := digestAlgorithm.Parse()
		if err != nil {
			return nil, err
		}
		ml.Digest = digest.NewDigestFromBytes(alg, digestHex)

		mls = append(mls, ml)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning manifest lists: %w", err)
	}

	return mls, nil
}

// FindByID finds a manifest list by ID.
func (s *manifestListStore) FindByID(ctx context.Context, id int64) (*models.ManifestList, error) {
	q := `SELECT id, schema_version, media_type, digest_algorithm, digest_hex, payload, created_at, marked_at
		FROM manifest_lists WHERE id = $1`

	row := s.db.QueryRowContext(ctx, q, id)

	return scanFullManifestList(row)
}

// FindByDigest finds a manifest list by the digest.
func (s *manifestListStore) FindByDigest(ctx context.Context, d digest.Digest) (*models.ManifestList, error) {
	q := `SELECT id, schema_version, media_type, digest_algorithm, digest_hex, payload, created_at, marked_at
		FROM manifest_lists WHERE digest_algorithm = $1 AND digest_hex = decode($2, 'hex')`

	alg, err := NewDigestAlgorithm(d.Algorithm())
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, q, alg, d.Hex())

	return scanFullManifestList(row)
}

// FindAll finds all manifest lists.
func (s *manifestListStore) FindAll(ctx context.Context) (models.ManifestLists, error) {
	q := `SELECT id, schema_version, media_type, digest_algorithm, digest_hex, payload, created_at, marked_at
		FROM manifest_lists`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("error finding manifest lists: %w", err)
	}

	return scanFullManifestLists(rows)
}

// Count counts all manifest lists.
func (s *manifestListStore) Count(ctx context.Context) (int, error) {
	q := "SELECT COUNT(*) FROM manifest_lists"
	var count int

	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return count, fmt.Errorf("error counting manifest lists: %w", err)
	}

	return count, nil
}

// Manifests finds all manifests associated with a manifest list, through the ManifestListManifest relationship entity.
func (s *manifestListStore) Manifests(ctx context.Context, ml *models.ManifestList) (models.Manifests, error) {
	q := `SELECT m.id, m.schema_version, m.media_type, m.digest_algorithm, m.digest_hex, m.payload, m.created_at, m.marked_at
		FROM manifests as m
		JOIN manifest_list_manifests as mlm ON mlm.manifest_id = m.id
		JOIN manifest_lists as ml ON ml.id = mlm.manifest_list_id
		WHERE ml.id = $1`

	rows, err := s.db.QueryContext(ctx, q, ml.ID)
	if err != nil {
		return nil, fmt.Errorf("error finding manifests: %w", err)
	}

	return scanFullManifests(rows)
}

// Repositories finds all repositories which reference a manifest list.
func (s *manifestListStore) Repositories(ctx context.Context, ml *models.ManifestList) (models.Repositories, error) {
	q := `SELECT r.id, r.name, r.path, r.parent_id, r.created_at, updated_at FROM repositories as r
		JOIN repository_manifest_lists as rml ON rml.repository_id = r.id
		JOIN manifest_lists as ml ON ml.id = rml.manifest_list_id
		WHERE ml.id = $1`

	rows, err := s.db.QueryContext(ctx, q, ml.ID)
	if err != nil {
		return nil, fmt.Errorf("error finding repositories: %w", err)
	}

	return scanFullRepositories(rows)
}

// Create saves a new ManifestList.
func (s *manifestListStore) Create(ctx context.Context, ml *models.ManifestList) error {
	q := `INSERT INTO manifest_lists (schema_version, media_type, digest_algorithm, digest_hex, payload)
		VALUES ($1, $2, $3, decode($4, 'hex'), $5) RETURNING id, created_at`

	digestAlgorithm, err := NewDigestAlgorithm(ml.Digest.Algorithm())
	if err != nil {
		return err
	}
	row := s.db.QueryRowContext(ctx, q, ml.SchemaVersion, ml.MediaType, digestAlgorithm, ml.Digest.Hex(), ml.Payload)
	if err := row.Scan(&ml.ID, &ml.CreatedAt); err != nil {
		return fmt.Errorf("error creating manifest list: %w", err)
	}

	return nil
}

// Update updates an existing manifest list.
func (s *manifestListStore) Update(ctx context.Context, ml *models.ManifestList) error {
	q := `UPDATE manifest_lists
		SET (schema_version, media_type, digest_algorithm, digest_hex, payload) = ($1, $2, $3, decode($4, 'hex'), $5)
		WHERE id = $6`

	digestAlgorithm, err := NewDigestAlgorithm(ml.Digest.Algorithm())
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, q, ml.SchemaVersion, ml.MediaType, digestAlgorithm, ml.Digest.Hex(), ml.Payload, ml.ID)
	if err != nil {
		return fmt.Errorf("error updating manifest list: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error updating manifest list: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("manifest list not found")
	}

	return nil
}

// Mark marks a manifest list during garbage collection.
func (s *manifestListStore) Mark(ctx context.Context, ml *models.ManifestList) error {
	q := "UPDATE manifest_lists SET marked_at = NOW() WHERE id = $1 RETURNING marked_at"

	if err := s.db.QueryRowContext(ctx, q, ml.ID).Scan(&ml.MarkedAt); err != nil {
		if err == sql.ErrNoRows {
			return errors.New("manifest list not found")
		}
		return fmt.Errorf("error soft deleting manifest list: %w", err)
	}

	return nil
}

// AssociateManifest associates a manifest and a manifest list. It does nothing if already associated.
func (s *manifestListStore) AssociateManifest(ctx context.Context, ml *models.ManifestList, m *models.Manifest) error {
	q := `INSERT INTO manifest_list_manifests (manifest_list_id, manifest_id) VALUES ($1, $2)
		ON CONFLICT (manifest_list_id, manifest_id) DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, ml.ID, m.ID); err != nil {
		return fmt.Errorf("error associating manifest: %w", err)
	}

	return nil
}

// DissociateManifest dissociates a manifest and a manifest list. It does nothing if not associated.
func (s *manifestListStore) DissociateManifest(ctx context.Context, ml *models.ManifestList, m *models.Manifest) error {
	q := "DELETE FROM manifest_list_manifests WHERE manifest_list_id = $1 AND manifest_id = $2"

	res, err := s.db.ExecContext(ctx, q, ml.ID, m.ID)
	if err != nil {
		return fmt.Errorf("error dissociating manifest: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("error dissociating manifest: %w", err)
	}

	return nil
}

// Delete deletes a manifest list.
func (s *manifestListStore) Delete(ctx context.Context, id int64) error {
	q := "DELETE FROM manifest_lists WHERE id = $1"

	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("error deleting manifest list: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error deleting manifest list: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("manifest list not found")
	}

	return nil
}
