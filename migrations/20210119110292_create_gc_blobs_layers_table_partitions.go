package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20210119110292_create_gc_blobs_layers_table_partitions",
			Up:   partitionUpStatements("gc_blobs_layers", 128),
			Down: partitionDownStatements("gc_blobs_layers", 128),
		},
	}

	allMigrations = append(allMigrations, m)
}
