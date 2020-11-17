// +build integration

package migrationfixtures

import (
	"github.com/docker/distribution/migrations"
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &migrations.Migration{Migration: &migrate.Migration{
		Id: "20200527132906_create_repository_blobs_test_table",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS repository_blobs_test (
                id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
                repository_id bigint NOT NULL,
                blob_id bigint NOT NULL,
                created_at timestamp WITH time zone NOT NULL DEFAULT now(),
                CONSTRAINT pk_repository_blobs_test PRIMARY KEY (id),
                CONSTRAINT fk_repository_blobs_test_repository_id_repositories FOREIGN KEY (repository_id) REFERENCES repositories_test (id) ON DELETE CASCADE,
                CONSTRAINT fk_repository_blobs_test_blob_id_blobs FOREIGN KEY (blob_id) REFERENCES blobs_test (id) ON DELETE CASCADE,
                CONSTRAINT unique_repository_blobs_test_repository_id_blob_id UNIQUE (repository_id, blob_id)
            )`,
			"CREATE INDEX IF NOT EXISTS index_repository_blobs_test_repository_id ON repository_blobs_test (repository_id)",
			"CREATE INDEX IF NOT EXISTS index_repository_blobs_test_blob_id ON repository_blobs_test (blob_id)",
		},
		Down: []string{
			"DROP INDEX IF EXISTS index_repository_blobs_test_blob_id CASCADE",
			"DROP INDEX IF EXISTS index_repository_blobs_test_repository_id CASCADE",
			"DROP TABLE IF EXISTS repository_blobs_test CASCADE",
		},
	}}

	allMigrations = append(allMigrations, m)
}
