package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &migrate.Migration{
		Id: "20201019155015_create_blobs_table",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS public.blobs (
				size bigint NOT NULL,
				created_at timestamp WITH time zone NOT NULL DEFAULT now(),
				media_type_id smallint NOT NULL,
				digest bytea NOT NULL,
				CONSTRAINT pk_blobs PRIMARY KEY (digest),
				CONSTRAINT fk_blobs_media_type_id_media_types FOREIGN KEY (media_type_id) REFERENCES media_types (id)
			)
			PARTITION BY HASH (digest)`,
		},
		Down: []string{
			"DROP TABLE IF EXISTS public.blobs CASCADE",
		},
	}

	allMigrations = append(allMigrations, m)
}
