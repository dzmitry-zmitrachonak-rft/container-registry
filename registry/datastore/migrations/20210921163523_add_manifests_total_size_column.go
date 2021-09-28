package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210921163523_add_manifests_total_size_column",
			Up: []string{
				"ALTER TABLE manifests ADD COLUMN IF NOT EXISTS total_size bigint DEFAULT 0 NOT NULL",
			},
			Down: []string{
				"ALTER TABLE manifests DROP COLUMN IF EXISTS total_size",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
