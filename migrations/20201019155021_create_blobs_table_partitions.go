package migrations

import (
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155021_create_blobs_table_partitions",
			Up:   partitionUpStatements("blobs", 256),
			Down: partitionDownStatements("blobs", 256),
		},
	}

	allMigrations = append(allMigrations, m)
}
