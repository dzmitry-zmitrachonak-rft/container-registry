package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155117_create_layers_table_partitions",
			Up:   partitionUpStatements("layers", 256),
			Down: partitionDownStatements("layers", 256),
		},
	}

	allMigrations = append(allMigrations, m)
}
