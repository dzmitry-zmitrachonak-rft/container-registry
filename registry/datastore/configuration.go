package datastore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker/distribution/registry/datastore/models"
	"github.com/opencontainers/go-digest"
)

// ConfigurationReader is the interface that defines read operations for a configuration store.
type ConfigurationReader interface {
	FindAll(ctx context.Context) (models.Configurations, error)
	FindByID(ctx context.Context, id int64) (*models.Configuration, error)
	FindByDigest(ctx context.Context, d digest.Digest) (*models.Configuration, error)
	Count(ctx context.Context) (int, error)
	Manifests(ctx context.Context, c *models.Configuration) (models.Manifests, error)
}

// ConfigurationWriter is the interface that defines write operations for a configuration store.
type ConfigurationWriter interface {
	Create(ctx context.Context, c *models.Configuration) error
	Delete(ctx context.Context, id int64) error
}

// ConfigurationStore is the interface that a configuration store should conform to.
type ConfigurationStore interface {
	ConfigurationReader
	ConfigurationWriter
}

// configurationStore is the concrete implementation of a ConfigurationStore.
type configurationStore struct {
	db Queryer
}

// NewConfigurationStore builds a new repository store.
func NewConfigurationStore(db Queryer) *configurationStore {
	return &configurationStore{db: db}
}

func scanFullConfiguration(row *sql.Row) (*models.Configuration, error) {
	var dgst Digest
	c := new(models.Configuration)
	err := row.Scan(&c.ID, &c.MediaType, &dgst, &c.Size, &c.Payload, &c.CreatedAt)
	if err != nil {
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("scaning configuration: %w", err)
		}
		return nil, nil
	}

	d, err := dgst.Parse()
	if err != nil {
		return nil, err
	}
	c.Digest = d

	return c, nil
}

func scanFullConfigurations(rows *sql.Rows) (models.Configurations, error) {
	cc := make(models.Configurations, 0)
	defer rows.Close()

	for rows.Next() {
		var dgst Digest
		c := new(models.Configuration)
		err := rows.Scan(&c.ID, &c.MediaType, &dgst, &c.Size, &c.Payload, &c.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning configuration: %w", err)
		}

		d, err := dgst.Parse()
		if err != nil {
			return nil, err
		}
		c.Digest = d

		cc = append(cc, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning configurations: %w", err)
	}

	return cc, nil
}

// FindByID finds a configuration by ID.
func (s *configurationStore) FindByID(ctx context.Context, id int64) (*models.Configuration, error) {
	q := `SELECT
			c.id,
			mt.media_type,
			encode(b.digest, 'hex') as digest,
			b.size,
			c.payload,
			c.created_at
		FROM
			configurations AS c
			JOIN blobs AS b ON c.blob_digest = b.digest
			JOIN media_types AS mt ON mt.id = b.media_type_id
		WHERE
			c.id = $1`
	row := s.db.QueryRowContext(ctx, q, id)

	return scanFullConfiguration(row)
}

// FindByDigest finds a configuration by the digest.
func (s *configurationStore) FindByDigest(ctx context.Context, d digest.Digest) (*models.Configuration, error) {
	q := `SELECT
			c.id,
			mt.media_type,
			encode(b.digest, 'hex') as digest,
			b.size,
			c.payload,
			c.created_at
		FROM
			configurations AS c
			JOIN blobs AS b ON c.blob_digest = b.digest
			JOIN media_types AS mt ON mt.id = b.media_type_id
		WHERE
			digest = decode($1, 'hex')`

	dgst, err := NewDigest(d)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, q, dgst)

	return scanFullConfiguration(row)
}

// FindAll finds all configurations.
func (s *configurationStore) FindAll(ctx context.Context) (models.Configurations, error) {
	q := `SELECT
			c.id,
			mt.media_type,
			encode(b.digest, 'hex') as digest,
			b.size,
			c.payload,
			c.created_at
		FROM
			configurations AS c
			JOIN blobs AS b ON c.blob_digest = b.digest
			JOIN media_types AS mt ON mt.id = b.media_type_id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("finding configurations: %w", err)
	}

	return scanFullConfigurations(rows)
}

// Count counts all configurations.
func (s *configurationStore) Count(ctx context.Context) (int, error) {
	q := "SELECT COUNT(*) FROM configurations"
	var count int

	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return count, fmt.Errorf("counting configurations: %w", err)
	}

	return count, nil
}

// Manifests finds the manifests that reference a configuration.
func (s *configurationStore) Manifests(ctx context.Context, c *models.Configuration) (models.Manifests, error) {
	q := `SELECT
			m.id,
			m.configuration_id,
			m.schema_version,
			mt.media_type,
			encode(m.digest, 'hex') as digest,
			m.payload,
			m.created_at,
			m.marked_at
		FROM
			manifests AS m
			JOIN media_types AS mt ON mt.id = m.media_type_id
		WHERE
			m.configuration_id = $1`

	rows, err := s.db.QueryContext(ctx, q, c.ID)
	if err != nil {
		return nil, fmt.Errorf("finding manifests: %w", err)
	}

	return scanFullManifests(rows)
}

// Create saves a new configuration.
func (s *configurationStore) Create(ctx context.Context, c *models.Configuration) error {
	q := `INSERT INTO configurations (blob_digest, payload)
			VALUES (decode($1, 'hex'), $2)
		RETURNING
			id, created_at`

	dgst, err := NewDigest(c.Digest)
	if err != nil {
		return err
	}
	row := s.db.QueryRowContext(ctx, q, dgst, c.Payload)
	if err := row.Scan(&c.ID, &c.CreatedAt); err != nil {
		return fmt.Errorf("creating configuration: %w", err)
	}

	return nil
}

// Delete deletes a configuration.
func (s *configurationStore) Delete(ctx context.Context, id int64) error {
	q := "DELETE FROM configurations WHERE id = $1"

	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("deleting configuration: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleting configuration: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("configuration not found")
	}

	return nil
}
