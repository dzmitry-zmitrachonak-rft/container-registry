package migrations

import (
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503162024_create_gc_track_deleted_manifests_trigger",
			Up: []string{
				`DO $$
				BEGIN
					IF NOT EXISTS (
						SELECT
							1
						FROM
							pg_trigger
						WHERE
							tgname = 'gc_track_deleted_manifests_trigger') THEN
						CREATE TRIGGER gc_track_deleted_manifests_trigger
							AFTER DELETE ON manifests
							FOR EACH ROW
							EXECUTE PROCEDURE gc_track_deleted_manifests ();
					END IF;
				END
				$$`,
			},
			Down: []string{
				"DROP TRIGGER IF EXISTS gc_track_deleted_manifests_trigger ON layers CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
