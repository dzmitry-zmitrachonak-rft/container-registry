package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210329152236_create_gc_blobs_configurations_on_repository_id_and_manifest_id_index",
			Up: []string{
				"CREATE INDEX IF NOT EXISTS index_gc_blobs_configurations_on_repository_id_and_manifest_id ON gc_blobs_configurations USING btree (repository_id, manifest_id)",
			},
			Down: []string{
				"DROP INDEX IF EXISTS index_gc_blobs_configurations_on_repository_id_and_manifest_id CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
