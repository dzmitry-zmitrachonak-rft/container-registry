package migrations

import (
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503162603_create_gc_track_switched_tags_trigger",
			Up: []string{
				`DO $$
				BEGIN
					IF NOT EXISTS (
						SELECT
							1
						FROM
							pg_trigger
						WHERE
							tgname = 'gc_track_switched_tags_trigger') THEN
						CREATE TRIGGER gc_track_switched_tags_trigger
							AFTER UPDATE OF manifest_id ON tags
							FOR EACH ROW
							EXECUTE PROCEDURE gc_track_switched_tags ();
					END IF;
				END
				$$`,
			},
			Down: []string{
				"DROP TRIGGER IF EXISTS gc_track_switched_tags_trigger ON tags CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
