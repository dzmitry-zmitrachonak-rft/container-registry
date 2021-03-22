package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155128_create_manifest_references_table_partitions",
			Up:   partitionUpStatements("manifest_references", 256),
			Down: partitionDownStatements("manifest_references", 256),
		},
	}

	allMigrations = append(allMigrations, m)
}
