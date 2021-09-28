package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210927141959_add_gc_blob_review_queue_created_at_column",
			Up: []string{
				"ALTER TABLE gc_blob_review_queue ADD COLUMN IF NOT EXISTS created_at timestamp WITH time zone NOT NULL DEFAULT now()",
			},
			Down: []string{
				"ALTER TABLE gc_blob_review_queue DROP COLUMN IF EXISTS created_at",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
