package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id:   "20201019155144_create_tags_table_partitions",
			Up:   partitionUpStatements("tags", 256),
			Down: partitionDownStatements("tags", 256),
		},
	}

	allMigrations = append(allMigrations, m)
}
