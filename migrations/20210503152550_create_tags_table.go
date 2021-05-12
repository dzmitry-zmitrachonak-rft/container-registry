package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503152550_create_tags_table",
			Up: []string{
				`CREATE TABLE IF NOT EXISTS tags (
					id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
					top_level_namespace_id bigint NOT NULL,
					repository_id bigint NOT NULL,
					manifest_id bigint NOT NULL,
					created_at timestamp WITH time zone NOT NULL DEFAULT now(),
					updated_at timestamp WITH time zone,
					name text NOT NULL,
					CONSTRAINT pk_tags PRIMARY KEY (top_level_namespace_id, repository_id, id),
					CONSTRAINT fk_tags_repository_id_and_manifest_id_manifests FOREIGN KEY (top_level_namespace_id, repository_id, manifest_id) REFERENCES manifests (top_level_namespace_id, repository_id, id) ON DELETE CASCADE,
					CONSTRAINT unique_tags_top_level_namespace_id_and_repository_id_and_name UNIQUE (top_level_namespace_id, repository_id, name),
					CONSTRAINT check_tags_name_length CHECK ((char_length(name) <= 255))
				)
				PARTITION BY HASH (top_level_namespace_id)`,
				"CREATE INDEX IF NOT EXISTS index_tags_on_top_lvl_nmspc_id_and_rpository_id_and_manifest_id ON tags USING btree (top_level_namespace_id, repository_id, manifest_id)",
			},
			Down: []string{
				"DROP INDEX IF EXISTS index_tags_on_top_lvl_nmspc_id_and_rpository_id_and_manifest_id CASCADE",
				"DROP TABLE IF EXISTS tags CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
