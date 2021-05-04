// +build integration

package migrations

import (
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503163943_create_gc_blobs_layers_table_partitions_testing",
			Up: []string{
				"CREATE TABLE IF NOT EXISTS partitions.gc_blobs_layers_p_0 PARTITION OF public.gc_blobs_layers FOR VALUES WITH (MODULUS 4, REMAINDER 0)",
				"CREATE TABLE IF NOT EXISTS partitions.gc_blobs_layers_p_1 PARTITION OF public.gc_blobs_layers FOR VALUES WITH (MODULUS 4, REMAINDER 1)",
				"CREATE TABLE IF NOT EXISTS partitions.gc_blobs_layers_p_2 PARTITION OF public.gc_blobs_layers FOR VALUES WITH (MODULUS 4, REMAINDER 2)",
				"CREATE TABLE IF NOT EXISTS partitions.gc_blobs_layers_p_3 PARTITION OF public.gc_blobs_layers FOR VALUES WITH (MODULUS 4, REMAINDER 3)",
			},
			Down: []string{
				"DROP TABLE IF EXISTS partitions.gc_blobs_layers_p_0 CASCADE",
				"DROP TABLE IF EXISTS partitions.gc_blobs_layers_p_1 CASCADE",
				"DROP TABLE IF EXISTS partitions.gc_blobs_layers_p_2 CASCADE",
				"DROP TABLE IF EXISTS partitions.gc_blobs_layers_p_3 CASCADE",
			},
		},
	}

	allMigrations = append(allMigrations, m)
}
