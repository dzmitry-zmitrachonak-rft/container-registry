package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155117_create_layers_table_partitions",
			Up:   partitionUpStatements("layers", 128),
			Down: partitionDownStatements("layers", 128),
		},
	}

	allMigrations = append(allMigrations, m)
}
