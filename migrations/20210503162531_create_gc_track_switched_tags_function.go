package migrations

import (
	migrate "github.com/rubenv/sql-migrate"
)

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503162531_create_gc_track_switched_tags_function",
			Up: []string{
				`CREATE OR REPLACE FUNCTION gc_track_switched_tags ()
					RETURNS TRIGGER
					AS $$
				BEGIN
					INSERT INTO gc_manifest_review_queue (namespace_id, repository_id, manifest_id, review_after)
						VALUES (OLD.namespace_id, OLD.repository_id, OLD.manifest_id, gc_review_after ('tag_switch'))
					ON CONFLICT (namespace_id, repository_id, manifest_id)
						DO UPDATE SET
							review_after = gc_review_after ('tag_switch');
					RETURN NULL;
				END;
				$$
				LANGUAGE plpgsql`,
			},
			Down: []string{
				"DROP FUNCTION IF EXISTS gc_track_switched_tags CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
