package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503151531_create_manifests_table",
			Up: []string{
				`CREATE TABLE IF NOT EXISTS manifests (
					id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
					top_level_namespace_id bigint NOT NULL,
					repository_id bigint NOT NULL,
					created_at timestamp WITH time zone NOT NULL DEFAULT now(),
					schema_version smallint NOT NULL,
					media_type_id smallint NOT NULL,
					configuration_media_type_id smallint,
					configuration_payload bytea,
					configuration_blob_digest bytea,
					digest bytea NOT NULL,
					payload bytea NOT NULL,
					CONSTRAINT pk_manifests PRIMARY KEY (top_level_namespace_id, repository_id, id),
					CONSTRAINT fk_manifests_top_lvl_nmespace_id_and_repository_id_repositories FOREIGN KEY (top_level_namespace_id, repository_id) REFERENCES repositories (top_level_namespace_id, id) ON DELETE CASCADE,
					CONSTRAINT fk_manifests_media_type_id_media_types FOREIGN KEY (media_type_id) REFERENCES media_types (id),
					CONSTRAINT fk_manifests_configuration_media_type_id_media_types FOREIGN KEY (configuration_media_type_id) REFERENCES media_types (id),
					CONSTRAINT fk_manifests_configuration_blob_digest_blobs FOREIGN KEY (configuration_blob_digest) REFERENCES blobs (digest),
					CONSTRAINT unique_manifests_top_lvl_nmspc_id_and_repository_id_and_digest UNIQUE (top_level_namespace_id, repository_id, digest),
					CONSTRAINT unique_manifests_tp_lvl_nmspc_id_and_cfg_blob_dgst_repo_id_id UNIQUE (top_level_namespace_id, configuration_blob_digest, repository_id, id)
				)
				PARTITION BY HASH (top_level_namespace_id)`,
				"CREATE INDEX IF NOT EXISTS index_manifests_on_media_type_id ON manifests USING btree (media_type_id)",
				"CREATE INDEX IF NOT EXISTS index_manifests_on_configuration_media_type_id ON manifests USING btree (configuration_media_type_id)",
				"CREATE INDEX IF NOT EXISTS index_manifests_on_configuration_blob_digest ON manifests USING btree (configuration_blob_digest)",
			},
			Down: []string{
				"DROP INDEX IF EXISTS index_manifests_on_configuration_blob_digest CASCADE",
				"DROP INDEX IF EXISTS index_manifests_on_configuration_media_type_id CASCADE",
				"DROP INDEX IF EXISTS index_manifests_on_media_type_id CASCADE",
				"DROP TABLE IF EXISTS manifests CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
