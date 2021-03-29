package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210329150113_create_gc_blobs_layers_on_repository_id_and_layer_id_index",
			Up: []string{
				"CREATE INDEX IF NOT EXISTS index_gc_blobs_layers_on_repository_id_and_layer_id ON gc_blobs_layers USING btree (repository_id, layer_id)",
			},
			Down: []string{
				"DROP INDEX IF EXISTS index_gc_blobs_layers_on_repository_id_and_layer_id CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
