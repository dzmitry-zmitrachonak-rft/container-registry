package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210329152332_create_gc_blobs_configurations_on_digest_index",
			Up: []string{
				"CREATE INDEX IF NOT EXISTS index_gc_blobs_configurations_on_digest ON gc_blobs_configurations USING btree (digest)",
			},
			Down: []string{
				"DROP INDEX IF EXISTS index_gc_blobs_configurations_on_digest CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
