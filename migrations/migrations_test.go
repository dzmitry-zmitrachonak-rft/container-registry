package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_partitionUpStatements(t *testing.T) {
	tableName := "foo_bar"
	numPartitions := 3

	expected := []string{
		"CREATE TABLE IF NOT EXISTS partitions.foo_bar_p_0 PARTITION OF public.foo_bar " +
			"FOR VALUES WITH (MODULUS 3, REMAINDER 0)",
		"CREATE TABLE IF NOT EXISTS partitions.foo_bar_p_1 PARTITION OF public.foo_bar " +
			"FOR VALUES WITH (MODULUS 3, REMAINDER 1)",
		"CREATE TABLE IF NOT EXISTS partitions.foo_bar_p_2 PARTITION OF public.foo_bar " +
			"FOR VALUES WITH (MODULUS 3, REMAINDER 2)",
	}
	got := partitionUpStatements(tableName, numPartitions)
	require.EqualValues(t, expected, got)
}

func Test_partitionDownStatements(t *testing.T) {
	tableName := "foo_bar"
	numPartitions := 3

	expected := []string{
		"DROP TABLE IF EXISTS partitions.foo_bar_p_0 CASCADE",
		"DROP TABLE IF EXISTS partitions.foo_bar_p_1 CASCADE",
		"DROP TABLE IF EXISTS partitions.foo_bar_p_2 CASCADE",
	}
	got := partitionDownStatements(tableName, numPartitions)
	require.EqualValues(t, expected, got)
}
