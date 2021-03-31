package migrations

import (
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155021_create_blobs_table_partitions",
			Up:   partitionUpStatements("blobs", 128),
			Down: partitionDownStatements("blobs", 128),
		},
	}

	allMigrations = append(allMigrations, m)
}
