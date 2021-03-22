package migrations

import (
	"fmt"

	migrate "github.com/rubenv/sql-migrate"
)

var allMigrations []*Migration

type Migration struct {
	*migrate.Migration

	PostDeployment bool
}

// partitionUpStatements generates the DDL statements to create n partitions for a table with name table.
func partitionUpStatements(table string, n int) []string {
	s := "CREATE TABLE IF NOT EXISTS partitions.%s_p_%d PARTITION OF public.%s " +
		"FOR VALUES WITH (MODULUS %d, REMAINDER %d)"

	stmts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		stmts = append(stmts, fmt.Sprintf(s, table, i, table, n, i))
	}

	return stmts
}

// partitionDownStatements generates the DDL statements to drop n partitions for a table with name table.
func partitionDownStatements(table string, n int) []string {
	s := "DROP TABLE IF EXISTS partitions.%s_p_%d CASCADE"

	stmts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		stmts = append(stmts, fmt.Sprintf(s, table, i))
	}

	return stmts
}
