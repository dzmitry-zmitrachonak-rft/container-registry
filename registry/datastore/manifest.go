//go:generate mockgen -package mocks -destination mocks/manifest.go . ManifestStore

package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/docker/distribution/registry/datastore/metrics"
	"github.com/docker/distribution/registry/datastore/models"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgerrcode"
)

// ManifestReader is the interface that defines read operations for a Manifest store.
type ManifestReader interface {
	FindAll(ctx context.Context) (models.Manifests, error)
	Count(ctx context.Context) (int, error)
	LayerBlobs(ctx context.Context, m *models.Manifest) (models.Blobs, error)
	References(ctx context.Context, m *models.Manifest) (models.Manifests, error)
}

// ManifestWriter is the interface that defines write operations for a Manifest store.
type ManifestWriter interface {
	Create(ctx context.Context, m *models.Manifest) error
	AssociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error
	DissociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error
	AssociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error
	DissociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error
	Delete(ctx context.Context, m *models.Manifest) (bool, error)
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
func NewManifestStore(db Queryer) ManifestStore {
	return &manifestStore{db: db}
}

func scanFullManifest(row *sql.Row) (*models.Manifest, error) {
	var dgst Digest
	var cfgDigest, cfgMediaType sql.NullString
	var cfgPayload *models.Payload
	m := new(models.Manifest)

	err := row.Scan(&m.ID, &m.NamespaceID, &m.RepositoryID, &m.SchemaVersion, &m.MediaType, &dgst, &m.Payload, &cfgMediaType, &cfgDigest, &cfgPayload, &m.CreatedAt)
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
		var cfgPayload *models.Payload
		m := new(models.Manifest)

		err := rows.Scan(&m.ID, &m.NamespaceID, &m.RepositoryID, &m.SchemaVersion, &m.MediaType, &dgst, &m.Payload,
			&cfgMediaType, &cfgDigest, &cfgPayload, &m.CreatedAt)
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

// FindAll finds all manifests.
func (s *manifestStore) FindAll(ctx context.Context) (models.Manifests, error) {
	defer metrics.InstrumentQuery("manifest_find_all")()
	q := `SELECT
			m.id,
			m.namespace_id,
			m.repository_id,
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
		ORDER BY
			id`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("finding manifests: %w", err)
	}

	return scanFullManifests(rows)
}

// Count counts all manifests.
func (s *manifestStore) Count(ctx context.Context) (int, error) {
	defer metrics.InstrumentQuery("manifest_count")()
	q := "SELECT COUNT(*) FROM manifests"
	var count int

	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return count, fmt.Errorf("counting manifests: %w", err)
	}

	return count, nil
}

// LayerBlobs finds layer blobs associated with a manifest, through the `layers` relationship entity.
func (s *manifestStore) LayerBlobs(ctx context.Context, m *models.Manifest) (models.Blobs, error) {
	defer metrics.InstrumentQuery("manifest_layer_blobs")()
	q := `SELECT
			mt.media_type,
			encode(b.digest, 'hex') as digest,
			b.size,
			b.created_at
		FROM
			blobs AS b
			JOIN layers AS l ON l.digest = b.digest
			JOIN manifests AS m ON m.id = l.manifest_id
			JOIN media_types AS mt ON mt.id = b.media_type_id
		WHERE
			m.id = $1`

	rows, err := s.db.QueryContext(ctx, q, m.ID)
	if err != nil {
		return nil, fmt.Errorf("finding blobs: %w", err)
	}

	return scanFullBlobs(rows)
}

// References finds all manifests directly referenced by a manifest (if any).
func (s *manifestStore) References(ctx context.Context, m *models.Manifest) (models.Manifests, error) {
	defer metrics.InstrumentQuery("manifest_references")()
	q := `SELECT DISTINCT
			m.id,
			m.namespace_id,
			m.repository_id,
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
			JOIN manifest_references AS mr ON mr.namespace_id = m.namespace_id
				AND mr.child_id = m.id
			JOIN media_types AS mt ON mt.id = m.media_type_id
			LEFT JOIN media_types AS mtc ON mtc.id = m.configuration_media_type_id
		WHERE
			m.namespace_id = $1
			AND mr.repository_id = $2
			AND mr.parent_id = $3`

	rows, err := s.db.QueryContext(ctx, q, m.NamespaceID, m.RepositoryID, m.ID)
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
	defer metrics.InstrumentQuery("manifest_create")()
	q := `INSERT INTO manifests (namespace_id, repository_id, schema_version, media_type_id, digest, payload,
				configuration_media_type_id, configuration_blob_digest, configuration_payload)
			VALUES ($1, $2, $3, $4, decode($5, 'hex'), $6, $7, decode($8, 'hex'), $9)
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
	var configPayload *models.Payload
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

	row := s.db.QueryRowContext(ctx, q, m.NamespaceID, m.RepositoryID, m.SchemaVersion, mediaTypeID, dgst, m.Payload,
		configMediaTypeID, configDgst, configPayload)
	if err := row.Scan(&m.ID, &m.CreatedAt); err != nil {
		return fmt.Errorf("creating manifest: %w", err)
	}

	return nil
}

// AssociateManifest associates a manifest with a manifest list. It does nothing if already associated.
func (s *manifestStore) AssociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error {
	defer metrics.InstrumentQuery("manifest_associate_manifest")()
	if ml.ID == m.ID {
		return fmt.Errorf("cannot associate a manifest with itself")
	}

	q := `INSERT INTO manifest_references (namespace_id, repository_id, parent_id, child_id)
			VALUES ($1, $2, $3, $4)
		ON CONFLICT (namespace_id, repository_id, parent_id, child_id)
			DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, ml.NamespaceID, ml.RepositoryID, ml.ID, m.ID); err != nil {
		var pgErr *pgconn.PgError
		// this can happen if the child manifest is deleted by the online GC while attempting to create the list
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return ErrRefManifestNotFound
		}
		return fmt.Errorf("associating manifest: %w", err)
	}

	return nil
}

// DissociateManifest dissociates a manifest and a manifest list. It does nothing if not associated.
func (s *manifestStore) DissociateManifest(ctx context.Context, ml *models.Manifest, m *models.Manifest) error {
	defer metrics.InstrumentQuery("manifest_dissociate_manifest")()
	q := `DELETE FROM manifest_references
		WHERE namespace_id = $1
			AND repository_id = $2
			AND parent_id = $3
			AND child_id = $4`

	res, err := s.db.ExecContext(ctx, q, ml.NamespaceID, ml.RepositoryID, ml.ID, m.ID)
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
	defer metrics.InstrumentQuery("manifest_associate_layer_blob")()
	q := `INSERT INTO layers (namespace_id, repository_id, manifest_id, digest, media_type_id, size)
			VALUES ($1, $2, $3, decode($4, 'hex'), $5, $6)
		ON CONFLICT (namespace_id, repository_id, manifest_id, digest)
			DO NOTHING`

	dgst, err := NewDigest(b.Digest)
	if err != nil {
		return err
	}
	mediaTypeID, err := mapMediaType(ctx, s.db, b.MediaType)
	if err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, q, m.NamespaceID, m.RepositoryID, m.ID, dgst, mediaTypeID, b.Size); err != nil {
		return fmt.Errorf("associating layer blob: %w", err)
	}

	return nil
}

// DissociateLayerBlob dissociates a layer blob and a manifest. It does nothing if not associated.
func (s *manifestStore) DissociateLayerBlob(ctx context.Context, m *models.Manifest, b *models.Blob) error {
	defer metrics.InstrumentQuery("manifest_dissociate_layer_blob")()
	q := `DELETE FROM layers
		WHERE namespace_id = $1
			AND repository_id = $2
			AND manifest_id = $3
			AND digest = decode($4, 'hex')`

	dgst, err := NewDigest(b.Digest)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx, q, m.NamespaceID, m.RepositoryID, m.ID, dgst)
	if err != nil {
		return fmt.Errorf("dissociating layer blob: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("dissociating layer blob: %w", err)
	}

	return nil
}

// Delete deletes a manifest. A boolean is returned to denote whether the manifest was deleted or not. This avoids the
// need for a separate preceding `SELECT` to find if it exists. A manifest cannot be deleted if it is referenced by a
// manifest list.
func (s *manifestStore) Delete(ctx context.Context, m *models.Manifest) (bool, error) {
	defer metrics.InstrumentQuery("manifest_delete")()
	q := "DELETE FROM manifests WHERE namespace_id = $1 AND repository_id = $2 AND id = $3"

	res, err := s.db.ExecContext(ctx, q, m.NamespaceID, m.RepositoryID, m.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation && pgErr.TableName == "manifest_references" {
			return false, fmt.Errorf("deleting manifest: %w", ErrManifestReferencedInList)
		}
		return false, fmt.Errorf("deleting manifest: %w", err)
	}

	count, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("deleting manifest: %w", err)
	}

	return count == 1, nil
}
