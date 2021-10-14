package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20211014164147_drop_manifests_total_size_default_constraint",
			Up: []string{
				"ALTER TABLE manifests ALTER COLUMN total_size DROP DEFAULT",
			},
			Down: []string{
				"ALTER TABLE manifests ALTER COLUMN total_size SET DEFAULT 0",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
