// +build integration

package datastore_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/datastore/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dsnFactory func() (*datastore.DSN, error)
		opts       []datastore.OpenOption
		wantErr    bool
	}{
		{
			name:       "success",
			dsnFactory: testutil.NewDSNFromEnv,
			opts: []datastore.OpenOption{
				datastore.WithLogger(logrus.NewEntry(logrus.New())),
				datastore.WithPoolConfig(&datastore.PoolConfig{
					MaxIdle:     1,
					MaxOpen:     1,
					MaxLifetime: 1 * time.Minute,
				}),
			},
			wantErr: false,
		},
		{
			name: "error",
			dsnFactory: func() (*datastore.DSN, error) {
				dsn, err := testutil.NewDSNFromEnv()
				if err != nil {
					return nil, err
				}
				dsn.DBName = "nonexistent"
				return dsn, nil
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := tt.dsnFactory()
			require.NoError(t, err)

			db, err := datastore.Open(dsn)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				defer db.Close()
				require.NoError(t, err)
				require.IsType(t, new(datastore.DB), db)
			}
		})
	}
}

func TestTx_Savepoint(t *testing.T) {
	dsn, err := testutil.NewDSNFromEnv()
	require.NoError(t, err)
	db, err := datastore.Open(dsn)
	require.NoError(t, err)

	tx, err := db.BeginTx(suite.ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	svp := "mySavepoint"
	err = tx.Savepoint(suite.ctx, svp)
	require.NoError(t, err)

	// this would fail if the savepoint wasn't created
	_, err = tx.ExecContext(suite.ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", svp))
	require.NoError(t, err)
}

func TestTx_RollbackTo(t *testing.T) {
	dsn, err := testutil.NewDSNFromEnv()
	require.NoError(t, err)
	db, err := datastore.Open(dsn)
	require.NoError(t, err)

	tx, err := db.BeginTx(suite.ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()

	// create temp table
	tmpTable := "foo"
	_, err = tx.ExecContext(suite.ctx, fmt.Sprintf("CREATE TABLE %s (id integer)", tmpTable))
	require.NoError(t, err)

	// create savepoint
	svp := "mySavepoint"
	err = tx.Savepoint(suite.ctx, svp)
	require.NoError(t, err)

	// insert row in temp table
	_, err = tx.ExecContext(suite.ctx, fmt.Sprintf("INSERT INTO %s (id) VALUES (1)", tmpTable))
	require.NoError(t, err)

	// rollback to savepoint
	err = tx.RollbackTo(suite.ctx, svp)
	require.NoError(t, err)

	// make sure the table is empty but does exist
	var count int
	err = tx.QueryRowContext(suite.ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tmpTable)).Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
}
