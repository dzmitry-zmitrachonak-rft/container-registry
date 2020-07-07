package migrations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"text/template"
	"time"
)

const (
	migrationSeqFormat  = "20060102150405"
	migrationNameFormat = `\A[a-z0-9_]+\z`
)

const migrationTemplate = `package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &migrate.Migration{
		Id:   "{{ .Sequence }}_{{ .Name }}",
		Up:   []string{},
		Down: []string{},
	}

	allMigrations = append(allMigrations, m)
}`

// NewFromTemplate creates a new migration file based on migrationTemplate under dir and returns its full path.
func NewFromTemplate(dir, name string) (string, error) {
	matched, err := regexp.MatchString(migrationNameFormat, name)
	if err != nil {
		return "", fmt.Errorf("unable to validate name: %w", err)
	}
	if !matched {
		return "", errors.New("name can only contain alphanumeric and underscore characters")
	}

	if stat, err := os.Stat(dir); err != nil || !stat.IsDir() {
		return "", fmt.Errorf("%q directory not found in path", dir)
	}

	tmpl, err := template.New("").Parse(migrationTemplate)
	if err != nil {
		return "", fmt.Errorf("failure loading template: %w", err)
	}

	t := time.Now().UTC().Format(migrationSeqFormat)
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.go", t, name))
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("unable to create file: %w", err)
	}
	defer f.Close()

	if err = tmpl.Execute(f, struct {
		Sequence string
		Name     string
	}{
		Sequence: t,
		Name:     name,
	}); err != nil {
		return "", fmt.Errorf("failure processing template: %w", err)
	}

	return path, nil
}
