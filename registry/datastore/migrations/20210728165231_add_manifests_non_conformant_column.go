package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210728165231_add_manifests_non_conformant_column",
			Up: []string{
				"ALTER TABLE manifests ADD COLUMN IF NOT EXISTS non_conformant BOOLEAN DEFAULT FALSE",
			},
			Down: []string{
				"ALTER TABLE manifests DROP COLUMN IF EXISTS non_conformant",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
