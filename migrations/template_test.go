package migrations_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/docker/distribution/migrations"

	"github.com/stretchr/testify/require"
)

func TestNewFromTemplate(t *testing.T) {
	name := "foo_bar"
	dir, err := ioutil.TempDir("", "migrations")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	path, err := migrations.NewFromTemplate(dir, name)
	require.NoError(t, err)

	// validate file path
	require.Equal(t, dir, filepath.Dir(path))

	// validate file name
	dt := time.Now().UTC().Format("20060102150405")
	require.Equal(t, fmt.Sprintf("%s_%s.go", dt, name), filepath.Base(path))

	// validate file content
	expected := `package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &migrate.Migration{
		Id:   "` + dt + `_` + name + `",
		Up:   []string{},
		Down: []string{},
	}

	allMigrations = append(allMigrations, m)
}`
	actual, err := ioutil.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, expected, string(actual))
}

func TestNewFromTemplate_InvalidName(t *testing.T) {
	dir, err := ioutil.TempDir("", "migrations")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	names := []string{
		"foo bar",
		"foo%bar",
		"foo.bar",
		"",
	}
	for i, name := range names {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			path, err := migrations.NewFromTemplate(dir, name)
			require.Empty(t, path)
			require.Error(t, err)
			require.EqualError(t, err, "name can only contain alphanumeric and underscore characters")
			require.NoFileExists(t, path)
		})
	}
}

func TestNewFromTemplate_InvalidBaseDir(t *testing.T) {
	dir, err := ioutil.TempDir("", "migrations")
	require.NoError(t, err)
	os.RemoveAll(dir)

	path, err := migrations.NewFromTemplate(dir, "name")
	require.Empty(t, path)
	require.Error(t, err)
	require.EqualError(t, err, fmt.Sprintf("%q directory not found in path", dir))
}
