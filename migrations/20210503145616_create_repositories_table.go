package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503145616_create_repositories_table",
			Up: []string{
				`CREATE TABLE IF NOT EXISTS repositories (
					id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
					top_level_namespace_id bigint NOT NULL,
					parent_id bigint,
					created_at timestamp WITH time zone NOT NULL DEFAULT now(),
					updated_at timestamp WITH time zone,
					name text NOT NULL,
					path text NOT NULL,
					CONSTRAINT pk_repositories PRIMARY KEY (top_level_namespace_id, id),
					CONSTRAINT fk_repositories_top_level_namespace_id_top_level_namespaces FOREIGN KEY (top_level_namespace_id) REFERENCES top_level_namespaces (id) ON DELETE CASCADE,
					CONSTRAINT fk_repositories_top_lvl_namespace_id_and_parent_id_repositories FOREIGN KEY (top_level_namespace_id, parent_id) REFERENCES repositories (top_level_namespace_id, id) ON DELETE CASCADE,
					CONSTRAINT unique_repositories_top_level_namespace_id_and_path UNIQUE (top_level_namespace_id, path),
					CONSTRAINT check_repositories_name_length CHECK ((char_length(name) <= 255)),
					CONSTRAINT check_repositories_path_length CHECK ((char_length(path) <= 255))
				)
				PARTITION BY HASH (top_level_namespace_id)`,
				"CREATE INDEX IF NOT EXISTS index_repositories_on_top_level_namespace_id ON repositories USING btree (top_level_namespace_id)",
				"CREATE INDEX IF NOT EXISTS index_repositories_on_top_level_namespace_id_and_parent_id ON repositories USING btree (top_level_namespace_id, parent_id)",
			},
			Down: []string{
				"DROP INDEX IF EXISTS index_repositories_on_top_level_namespace_id_and_parent_id CASCADE",
				"DROP INDEX IF EXISTS index_repositories_on_top_level_namespace_id CASCADE",
				"DROP TABLE IF EXISTS repositories CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
