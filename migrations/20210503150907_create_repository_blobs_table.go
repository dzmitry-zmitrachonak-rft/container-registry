package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503150907_create_repository_blobs_table",
			Up: []string{
				`CREATE TABLE IF NOT EXISTS repository_blobs (
					id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
					top_level_namespace_id bigint NOT NULL,
					repository_id bigint NOT NULL,
					created_at timestamp WITH time zone NOT NULL DEFAULT now(),
					blob_digest bytea NOT NULL,
					CONSTRAINT pk_repository_blobs PRIMARY KEY (top_level_namespace_id, repository_id, id),
					CONSTRAINT fk_repository_blobs_top_lvl_nmspc_id_and_rpstry_id_repositories FOREIGN KEY (top_level_namespace_id, repository_id) REFERENCES repositories (top_level_namespace_id, id) ON DELETE CASCADE,
					CONSTRAINT fk_repository_blobs_blob_digest_blobs FOREIGN KEY (blob_digest) REFERENCES blobs (digest) ON DELETE CASCADE,
					CONSTRAINT unique_repository_blobs_tp_lvl_nmspc_id_and_rpstry_id_blb_dgst UNIQUE (top_level_namespace_id, repository_id, blob_digest)
				)
				PARTITION BY HASH (top_level_namespace_id)`,
				"CREATE INDEX IF NOT EXISTS  index_repository_blobs_on_top_lvl_nmspc_id_and_repository_id ON repository_blobs USING btree (top_level_namespace_id, repository_id)",
				"CREATE INDEX IF NOT EXISTS index_repository_blobs_on_blob_digest ON repository_blobs USING btree (blob_digest)",
			},
			Down: []string{
				"DROP INDEX IF EXISTS index_repository_blobs_on_blob_digest CASCADE",
				"DROP INDEX IF EXISTS  index_repository_blobs_on_top_lvl_nmspc_id_and_repository_id CASCADE",
				"DROP TABLE IF EXISTS repository_blobs CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
