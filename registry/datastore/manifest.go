package datastore

import (
	"context"
	"database/sql"
	"encoding/json"
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
	LayerBlobs(ctx context.Context, m *models.Manifest) (models.Blobs, error)
	References(ctx context.Context, m *models.Manifest) (models.Manifests, error)
	Repositories(ctx context.Context, m *models.Manifest) (models.Repositories, error)
}

// ManifestWriter is the interface that defines write operations for a Manifest store.
type ManifestWriter interface {
	Create(ctx context.Context, m *models.Manifest) error
	AssociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error
	DissociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error
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
	var dgst Digest
	var cfgDigest, cfgMediaType sql.NullString
	var cfgPayload *json.RawMessage
	m := new(models.Manifest)

	err := row.Scan(&m.ID, &m.SchemaVersion, &m.MediaType, &dgst, &m.Payload, &cfgMediaType, &cfgDigest, &cfgPayload, &m.CreatedAt)
	if err != nil {
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("scaning manifest: %w", err)
		}
		return nil, nil
	}

	d, err := dgst.Parse()
	if err != nil {
		return nil, err
	}
	m.Digest = d

	if cfgDigest.Valid {
		d, err := Digest(cfgDigest.String).Parse()
		if err != nil {
			return nil, err
		}

		m.Configuration = &models.Configuration{
			MediaType: cfgMediaType.String,
			Digest:    d,
			Payload:   *cfgPayload,
		}
	}

	return m, nil
}

func scanFullManifests(rows *sql.Rows) (models.Manifests, error) {
	mm := make(models.Manifests, 0)
	defer rows.Close()

	for rows.Next() {
		var dgst Digest
		var cfgDigest, cfgMediaType sql.NullString
		var cfgPayload *json.RawMessage
		m := new(models.Manifest)

		err := rows.Scan(&m.ID, &m.SchemaVersion, &m.MediaType, &dgst, &m.Payload, &cfgMediaType, &cfgDigest, &cfgPayload, &m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning manifest: %w", err)
		}

		d, err := dgst.Parse()
		if err != nil {
			return nil, err
		}
		m.Digest = d

		if cfgDigest.Valid {
			d, err := Digest(cfgDigest.String).Parse()
			if err != nil {
				return nil, err
			}

			m.Configuration = &models.Configuration{
				MediaType: cfgMediaType.String,
				Digest:    d,
				Payload:   *cfgPayload,
			}
		}

		mm = append(mm, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning manifests: %w", err)
	}

	return mm, nil
}

// FindByID finds a Manifest by ID.
func (s *manifestStore) FindByID(ctx context.Context, id int64) (*models.Manifest, error) {
	q := `SELECT
			m.id,
			m.schema_version,
			mt.media_type,
			encode(m.digest, 'hex') as digest,
			m.payload,
			mtc.media_type as configuration_media_type,
			encode(m.configuration_blob_digest, 'hex') as configuration_blob_digest,
			m.configuration_payload,
			m.created_at
		FROM
			manifests AS m
			JOIN media_types AS mt ON mt.id = m.media_type_id
			LEFT JOIN media_types AS mtc ON mtc.id = m.configuration_media_type_id
		WHERE
			m.id = $1`

	row := s.db.QueryRowContext(ctx, q, id)

	return scanFullManifest(row)
}

// FindByDigest finds a Manifest by the digest.
func (s *manifestStore) FindByDigest(ctx context.Context, d digest.Digest) (*models.Manifest, error) {
	q := `SELECT
			m.id,
			m.schema_version,
			mt.media_type,
			encode(m.digest, 'hex') as digest,
			m.payload,
			mtc.media_type as configuration_media_type,
			encode(m.configuration_blob_digest, 'hex') as configuration_blob_digest,
			m.configuration_payload,
			m.created_at
		FROM
			manifests AS m
			JOIN media_types AS mt ON mt.id = m.media_type_id
			LEFT JOIN media_types AS mtc ON mtc.id = m.configuration_media_type_id
		WHERE
			m.digest = decode($1, 'hex')`

	dgst, err := NewDigest(d)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, q, dgst)

	return scanFullManifest(row)
}

// FindAll finds all manifests.
func (s *manifestStore) FindAll(ctx context.Context) (models.Manifests, error) {
	q := `SELECT
			m.id,
			m.schema_version,
			mt.media_type,
			encode(m.digest, 'hex') as digest,
			m.payload,
			mtc.media_type as configuration_media_type,
			encode(m.configuration_blob_digest, 'hex') as configuration_blob_digest,
			m.configuration_payload,
			m.created_at
		FROM
			manifests AS m
			JOIN media_types AS mt ON mt.id = m.media_type_id
			LEFT JOIN media_types AS mtc ON mtc.id = m.configuration_media_type_id`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("finding manifests: %w", err)
	}

	return scanFullManifests(rows)
}

// Count counts all manifests.
func (s *manifestStore) Count(ctx context.Context) (int, error) {
	q := "SELECT COUNT(*) FROM manifests"
	var count int

	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return count, fmt.Errorf("counting manifests: %w", err)
	}

	return count, nil
}

// LayerBlobs finds layer blobs associated with a manifest, through the `manifest_layers` relationship entity.
func (s *manifestStore) LayerBlobs(ctx context.Context, m *models.Manifest) (models.Blobs, error) {
	q := `SELECT
			mt.media_type,
			encode(b.digest, 'hex') as digest,
			b.size,
			b.created_at
		FROM
			blobs AS b
			JOIN manifest_layers AS ml ON ml.blob_digest = b.digest
			JOIN manifests AS m ON m.id = ml.manifest_id
			JOIN media_types AS mt ON mt.id = b.media_type_id
		WHERE
			m.id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("finding blobs: %w", err)
	}

	return scanFullBlobs(rows)
}

// Repositories finds all repositories which reference a manifest.
func (s *manifestStore) Repositories(ctx context.Context, m *models.Manifest) (models.Repositories, error) {
	q := `SELECT
			r.id,
			r.name,
			r.path,
			r.parent_id,
			r.created_at,
			updated_at
		FROM
			repositories AS r
			JOIN repository_manifests AS rm ON rm.repository_id = r.id
			JOIN manifests AS m ON m.id = rm.manifest_id
		WHERE
			m.id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("finding repositories: %w", err)
	}

	return scanFullRepositories(rows)
}

// References finds all manifests directly referenced by a manifest (if any).
func (s *manifestStore) References(ctx context.Context, m *models.Manifest) (models.Manifests, error) {
	q := `SELECT DISTINCT
			m.id,
			m.schema_version,
			mt.media_type,
			encode(m.digest, 'hex') as digest,
			m.payload,
			mtc.media_type as configuration_media_type,
			encode(m.configuration_blob_digest, 'hex') as configuration_blob_digest,
			m.configuration_payload,
			m.created_at
		FROM
			manifests AS m
			JOIN manifest_references AS mr ON mr.child_id = m.id
			JOIN media_types AS mt ON mt.id = m.media_type_id
			LEFT JOIN media_types AS mtc ON mtc.id = m.configuration_media_type_id
		WHERE
			mr.parent_id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("finding referenced manifests: %w", err)
	}

	return scanFullManifests(rows)
}

func mapMediaType(ctx context.Context, db Queryer, mediaType string) (int, error) {
	q := `SELECT
			id
		FROM
			media_types
		WHERE
			media_type = $1`

	var id int
	row := db.QueryRowContext(ctx, q, mediaType)
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("unknown media type %q", mediaType)
		}
		return 0, fmt.Errorf("unable to map media type: %w", err)
	}

	return id, nil
}

// Create saves a new Manifest.
func (s *manifestStore) Create(ctx context.Context, m *models.Manifest) error {
	q := `INSERT INTO manifests (schema_version, media_type_id, digest, payload, configuration_media_type_id, 
				configuration_blob_digest, configuration_payload)
			VALUES ($1, $2, decode($3, 'hex'), $4, $5, decode($6, 'hex'), $7)
		RETURNING
			id, created_at`

	dgst, err := NewDigest(m.Digest)
	if err != nil {
		return err
	}
	mediaTypeID, err := mapMediaType(ctx, s.db, m.MediaType)
	if err != nil {
		return err
	}

	var configDgst sql.NullString
	var configMediaTypeID sql.NullInt32
	var configPayload *json.RawMessage
	if m.Configuration != nil {
		dgst, err := NewDigest(m.Configuration.Digest)
		if err != nil {
			return err
		}
		configDgst.Valid = true
		configDgst.String = dgst.String()
		id, err := mapMediaType(ctx, s.db, m.Configuration.MediaType)
		if err != nil {
			return err
		}
		configMediaTypeID.Valid = true
		configMediaTypeID.Int32 = int32(id)
		configPayload = &m.Configuration.Payload
	}

	row := s.db.QueryRowContext(ctx, q, m.SchemaVersion, mediaTypeID, dgst, m.Payload, configMediaTypeID, configDgst, configPayload)
	if err := row.Scan(&m.ID, &m.CreatedAt); err != nil {
		return fmt.Errorf("creating manifest: %w", err)
	}

	return nil
}

// AssociateManifest associates a manifest with a manifest list. It does nothing if already associated.
func (s *manifestStore) AssociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error {
	if ml.ID == m.ID {
		return fmt.Errorf("cannot associate a manifest with itself")
	}

	q := `INSERT INTO manifest_references (parent_id, child_id)
			VALUES ($1, $2)
		ON CONFLICT (parent_id, child_id)
			DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, ml.ID, m.ID); err != nil {
		return fmt.Errorf("associating manifest: %w", err)
	}

	return nil
}

// DissociateManifest dissociates a manifest and a manifest list. It does nothing if not associated.
func (s *manifestStore) DissociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error {
	q := "DELETE FROM manifest_references WHERE parent_id = $1 AND child_id = $2"

	res, err := s.db.ExecContext(ctx, q, ml.ID, m.ID)
	if err != nil {
		return fmt.Errorf("dissociating manifest: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("dissociating manifest: %w", err)
	}

	return nil
}

// AssociateLayerBlob associates a layer blob and a manifest. It does nothing if already associated.
func (s *manifestStore) AssociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error {
	q := `INSERT INTO manifest_layers (manifest_id, blob_digest)
			VALUES ($1, decode($2, 'hex'))
		ON CONFLICT (manifest_id, blob_digest)
			DO NOTHING`

	dgst, err := NewDigest(b.Digest)
	if err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, q, m.ID, dgst); err != nil {
		return fmt.Errorf("associating layer blob: %w", err)
	}

	return nil
}

// DissociateLayerBlob dissociates a layer blob and a manifest. It does nothing if not associated.
func (s *manifestStore) DissociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error {
	q := "DELETE FROM manifest_layers WHERE manifest_id = $1 AND blob_digest = decode($2, 'hex')"

	dgst, err := NewDigest(b.Digest)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx, q, m.ID, dgst)
	if err != nil {
		return fmt.Errorf("dissociating layer blob: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("dissociating layer blob: %w", err)
	}

	return nil
}

// Delete deletes a Manifest.
func (s *manifestStore) Delete(ctx context.Context, id int64) error {
	q := "DELETE FROM manifests WHERE id = $1"

	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("deleting manifest: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleting manifest: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("manifest not found")
	}

	return nil
}
