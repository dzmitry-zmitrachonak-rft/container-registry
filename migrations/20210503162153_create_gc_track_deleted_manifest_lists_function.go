package migrations

import (
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503162153_create_gc_track_deleted_manifest_lists_function",
			Up: []string{
				`CREATE OR REPLACE FUNCTION gc_track_deleted_manifest_lists ()
					RETURNS TRIGGER
					AS $$
				BEGIN
					INSERT INTO gc_manifest_review_queue (namespace_id, repository_id, manifest_id, review_after)
						VALUES (OLD.namespace_id, OLD.repository_id, OLD.child_id, gc_review_after('manifest_list_delete'))
					ON CONFLICT (namespace_id, repository_id, manifest_id)
						DO UPDATE SET
							review_after = gc_review_after('manifest_list_delete');
					RETURN NULL;
				END;
				$$
				LANGUAGE plpgsql`,
			},
			Down: []string{
				"DROP FUNCTION IF EXISTS gc_track_deleted_manifest_lists CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
