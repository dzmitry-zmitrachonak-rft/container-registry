package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155109_create_manifests_table_partitions",
			Up:   partitionUpStatements("manifests", 256),
			Down: partitionDownStatements("manifests", 256),
		},
	}

	allMigrations = append(allMigrations, m)
}
