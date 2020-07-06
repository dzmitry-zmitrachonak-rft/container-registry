package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &migrate.Migration{
		Id: "20200319131632_create_manifest_layers_table",
		Up: []string{
			`CREATE TABLE IF NOT EXISTS manifest_layers (
                id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY,
                manifest_id bigint NOT NULL,
                layer_id bigint NOT NULL,
                created_at timestamp WITH time zone NOT NULL DEFAULT now(),
                CONSTRAINT pk_manifest_layers PRIMARY KEY (id),
                CONSTRAINT fk_manifest_layers_manifest_id_manifests FOREIGN KEY (manifest_id) REFERENCES manifests (id) ON DELETE CASCADE,
                CONSTRAINT fk_manifest_layers_layer_id_layers FOREIGN KEY (layer_id) REFERENCES layers (id) ON DELETE CASCADE,
                CONSTRAINT uq_manifest_layers_manifest_id_layer_id UNIQUE (manifest_id, layer_id)
            )`,
			"CREATE INDEX IF NOT EXISTS ix_manifest_layers_layer_id ON manifest_layers (layer_id)",
		},
		Down: []string{
			"DROP INDEX IF EXISTS ix_manifest_layers_layer_id CASCADE",
			"DROP TABLE IF EXISTS manifest_layers CASCADE",
		},
	}

	allMigrations = append(allMigrations, m)
}
