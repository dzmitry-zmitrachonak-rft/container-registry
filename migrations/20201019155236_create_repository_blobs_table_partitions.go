package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155236_create_repository_blobs_table_partitions",
			Up:   partitionUpStatements("repository_blobs", 128),
			Down: partitionDownStatements("repository_blobs", 128),
		},
	}

	allMigrations = append(allMigrations, m)
}
