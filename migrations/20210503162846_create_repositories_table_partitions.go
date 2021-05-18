// +build !integration

package migrations

import migrate "github.com/rubenv/sql-migrate"

func init() {
	m := &Migration{
		Migration: &migrate.Migration{
			Id: "20210503162846_create_repositories_table_partitions",
			Up: []string{
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_0 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 0)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_1 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 1)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_2 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 2)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_3 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 3)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_4 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 4)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_5 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 5)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_6 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 6)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_7 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 7)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_8 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 8)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_9 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 9)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_10 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 10)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_11 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 11)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_12 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 12)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_13 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 13)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_14 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 14)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_15 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 15)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_16 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 16)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_17 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 17)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_18 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 18)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_19 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 19)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_20 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 20)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_21 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 21)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_22 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 22)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_23 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 23)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_24 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 24)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_25 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 25)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_26 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 26)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_27 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 27)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_28 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 28)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_29 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 29)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_30 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 30)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_31 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 31)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_32 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 32)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_33 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 33)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_34 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 34)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_35 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 35)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_36 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 36)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_37 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 37)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_38 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 38)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_39 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 39)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_40 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 40)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_41 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 41)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_42 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 42)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_43 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 43)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_44 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 44)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_45 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 45)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_46 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 46)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_47 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 47)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_48 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 48)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_49 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 49)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_50 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 50)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_51 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 51)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_52 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 52)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_53 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 53)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_54 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 54)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_55 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 55)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_56 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 56)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_57 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 57)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_58 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 58)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_59 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 59)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_60 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 60)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_61 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 61)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_62 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 62)",
				"CREATE TABLE IF NOT EXISTS partitions.repositories_p_63 PARTITION OF public.repositories FOR VALUES WITH (MODULUS 64, REMAINDER 63)",
			},
			Down: []string{
				"DROP TABLE IF EXISTS partitions.repositories_p_0 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_1 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_2 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_3 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_4 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_5 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_6 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_7 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_8 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_9 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_10 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_11 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_12 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_13 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_14 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_15 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_16 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_17 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_18 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_19 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_20 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_21 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_22 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_23 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_24 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_25 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_26 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_27 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_28 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_29 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_30 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_31 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_32 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_33 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_34 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_35 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_36 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_37 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_38 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_39 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_40 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_41 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_42 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_43 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_44 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_45 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_46 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_47 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_48 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_49 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_50 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_51 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_52 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_53 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_54 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_55 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_56 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_57 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_58 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_59 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_60 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_61 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_62 CASCADE",
				"DROP TABLE IF EXISTS partitions.repositories_p_63 CASCADE",
			},
		},
		PostDeployment: false,
	}

	allMigrations = append(allMigrations, m)
}
