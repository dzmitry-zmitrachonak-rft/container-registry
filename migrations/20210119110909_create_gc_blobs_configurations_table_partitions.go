package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20210119110909_create_gc_blobs_configurations_table_partitions",
			Up:   partitionUpStatements("gc_blobs_configurations", 128),
			Down: partitionDownStatements("gc_blobs_configurations", 128),
		},
	}

	allMigrations = append(allMigrations, m)
}
