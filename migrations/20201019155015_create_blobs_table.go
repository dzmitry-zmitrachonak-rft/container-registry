package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &migrate.Migration{
		Id: "20201019155015_create_blobs_table",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS blobs (
				size bigint NOT NULL,
				created_at timestamp WITH time zone NOT NULL DEFAULT now(),
				media_type_id smallint NOT NULL,
				digest bytea NOT NULL,
				CONSTRAINT pk_blobs PRIMARY KEY (digest),
				CONSTRAINT fk_blobs_media_type_id_media_types FOREIGN KEY (media_type_id) REFERENCES media_types (id)
			)
			PARTITION BY HASH (digest)`,
			"CREATE INDEX IF NOT EXISTS ix_blobs_media_type_id ON blobs USING btree (media_type_id)",
		},
		Down: []string{
			"DROP INDEX IF EXISTS ix_blobs_media_type_id CASCADE",
			"DROP TABLE IF EXISTS blobs CASCADE",
		},
	}

	allMigrations = append(allMigrations, m)
}
