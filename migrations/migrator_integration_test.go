// +build integration

package migrations_test

import (
	"sort"
	"testing"
	"time"

	"github.com/docker/distribution/migrations"
	testmigrations "github.com/docker/distribution/migrations/testdata/fixtures"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/datastore/testutil"
	migrate "github.com/rubenv/sql-migrate"

	"github.com/stretchr/testify/require"
)

const migrationTableName = "test_migrations"

func init() {
	migrate.SetTable(migrationTableName)
}

func TestMigrator_Version(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	latest, err := m.LatestVersion()
	require.NoError(t, err)

	current, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, latest, current)
}

func TestMigrator_Version_NoMigrations(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	// Create migrator with an empty migration source.
	m := migrations.NewMigrator(db.DB, migrations.Source([]*migrations.Migration{}))

	v, err := m.Version()
	require.NoError(t, err)
	require.Empty(t, v)
}

func TestMigrator_LatestVersion(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))

	v, err := m.LatestVersion()
	require.NoError(t, err)
	require.Equal(t, v, testmigrations.All()[len(testmigrations.All())-1].Id)
}

func TestMigrator_LatestVersion_NoMigrations(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	// Create migrator with an empty migration source.
	m := migrations.NewMigrator(db.DB, migrations.Source([]*migrations.Migration{}))
	v, err := m.LatestVersion()
	require.NoError(t, err)
	require.Empty(t, v)
}

func TestMigrator_Up(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))

	all := testmigrations.All()

	count, err := m.Up()
	require.NoError(t, err)
	require.Equal(t, len(all), count)

	currentVersion, err := m.Version()
	require.NoError(t, err)

	v, err := m.LatestVersion()
	require.NoError(t, err)
	require.Equal(t, v, currentVersion)
}

func TestMigrator_Up_ApplyPostDeploymentMigrations(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)

	migs := testmigrations.NonPostDeployment()

	count, err := m.Up()
	require.NoError(t, err)
	require.Equal(t, len(migs), count)

	initialCurrentVersion, err := m.Version()
	require.NoError(t, err)

	initialLatestVersion, err := m.LatestVersion()
	require.NoError(t, err)
	require.Equal(t, initialLatestVersion, initialCurrentVersion)

	// Run post deployment migrations after fully applying all others.
	m = migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
	)

	all := testmigrations.All()

	count, err = m.Up()
	require.NoError(t, err)
	require.Equal(t, len(all)-len(migs), count)

	currentVersion, err := m.Version()
	require.NoError(t, err)

	latestVersion, err := m.LatestVersion()
	require.NoError(t, err)
	require.Equal(t, latestVersion, currentVersion)

	require.NotEqual(t, initialLatestVersion, latestVersion)
	require.NotEqual(t, initialCurrentVersion, currentVersion)
}

func TestMigrator_Up_SkipPostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)

	migs := testmigrations.NonPostDeployment()
	n := len(migs)

	count, err := m.Up()
	require.NoError(t, err)
	require.Equal(t, n, count)

	currentVersion, err := m.Version()
	require.NoError(t, err)

	v, err := m.LatestVersion()
	require.NoError(t, err)
	require.Equal(t, v, currentVersion)
}

func TestMigrator_UpN(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))

	// apply all except the last two
	all := testmigrations.All()
	n := len(all) - 1 - 2
	nth := all[n-1]

	count, err := m.UpN(n)
	require.NoError(t, err)
	require.Equal(t, n, count)

	v, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, nth.Id, v)

	// resume and apply the remaining
	count, err = m.UpN(0)
	require.NoError(t, err)
	require.Equal(t, len(all)-n, count)

	v, err = m.Version()
	require.NoError(t, err)
	require.Equal(t, all[len(all)-1].Id, v)

	// make sure it's idempotent
	count, err = m.UpN(100)
	require.NoError(t, err)
	require.Zero(t, count)

	v2, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, v, v2)
}

func TestMigrator_UpN_SkipPostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)

	// apply all non postdeployment migrations except the last two
	migs := testmigrations.NonPostDeployment()
	n := len(migs) - 1 - 2
	nth := migs[n-1]

	count, err := m.UpN(n)
	require.NoError(t, err)
	require.Equal(t, n, count)

	v, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, nth.Id, v)

	// resume and apply the remaining
	count, err = m.UpN(0)
	require.NoError(t, err)
	require.Equal(t, len(migs)-n, count)

	v, err = m.Version()
	require.NoError(t, err)
	require.Equal(t, migs[len(migs)-1].Id, v)

	// make sure it's idempotent
	count, err = m.UpN(100)
	require.NoError(t, err)
	require.Zero(t, count)

	v2, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, v, v2)
}

func TestMigrator_UpNPlan(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))

	all := testmigrations.All()

	var allPlan []string
	for _, migration := range all {
		allPlan = append(allPlan, migration.Id)
	}

	// plan all except the last two
	n := len(allPlan) - 1 - 2
	allExceptLastTwoPlan := allPlan[:n]

	plan, err := m.UpNPlan(n)
	require.NoError(t, err)
	require.Equal(t, allExceptLastTwoPlan, plan)

	// apply two migrations and re-plan all (the first two shouldn't be part of the plan anymore)
	_, err = m.UpN(2)
	require.NoError(t, err)

	plan, err = m.UpNPlan(0)
	require.NoError(t, err)

	allExceptFirstTwoPlan := allPlan[2:]
	require.Equal(t, allExceptFirstTwoPlan, plan)

	// make sure it's idempotent
	plan, err = m.UpNPlan(100)
	require.NoError(t, err)
	require.Equal(t, allExceptFirstTwoPlan, plan)
}

func TestMigrator_UpNPlan_SkipPostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)

	migs := testmigrations.NonPostDeployment()

	var allPlan []string
	for _, migration := range migs {
		allPlan = append(allPlan, migration.Id)
	}

	// plan all except the last two
	n := len(allPlan) - 1 - 2
	allExceptLastTwoPlan := allPlan[:n]

	plan, err := m.UpNPlan(n)
	require.NoError(t, err)
	require.Equal(t, allExceptLastTwoPlan, plan)

	// apply two migrations and re-plan all (the first two shouldn't be part of the plan anymore)
	_, err = m.UpN(2)
	require.NoError(t, err)

	plan, err = m.UpNPlan(0)
	require.NoError(t, err)

	allExceptFirstTwoPlan := allPlan[2:]
	require.Equal(t, allExceptFirstTwoPlan, plan)

	// make sure it's idempotent
	plan, err = m.UpNPlan(100)
	require.NoError(t, err)
	require.Equal(t, allExceptFirstTwoPlan, plan)
}

func TestMigrator_Down(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	all := testmigrations.All()

	count, err := m.Down()
	require.NoError(t, err)
	require.Equal(t, len(all), count)

	currentVersion, err := m.Version()
	require.NoError(t, err)
	require.Empty(t, currentVersion)
}

func TestMigrator_Down_SkipPostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)
	_, err = m.Up()
	require.NoError(t, err)

	migs := testmigrations.NonPostDeployment()

	count, err := m.Down()
	require.NoError(t, err)
	require.Equal(t, len(migs), count)

	currentVersion, err := m.Version()
	require.NoError(t, err)
	require.Empty(t, currentVersion)
}

func TestMigrator_Down_SkipPostDeployment_ExistingPostDeployments(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	all := testmigrations.All()

	// Configure migrator to skip postdeployment migrations, down migrations
	// should ignore this and operate on all applied migrations.
	migrations.SkipPostDeployment(m)

	count, err := m.Down()
	require.NoError(t, err)
	require.Equal(t, len(all), count)

	currentVersion, err := m.Version()
	require.NoError(t, err)
	require.Empty(t, currentVersion)
}

func TestMigrator_DownN(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	// rollback all except the first two
	all := testmigrations.All()
	n := len(all) - 2
	second := all[1]

	count, err := m.DownN(n)
	require.NoError(t, err)
	require.Equal(t, n, count)

	v, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, second.Id, v)

	// resume and rollback the remaining two
	count, err = m.DownN(0)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	v, err = m.Version()
	require.NoError(t, err)
	require.Empty(t, v)

	// make sure it's idempotent
	count, err = m.DownN(100)
	require.NoError(t, err)
	require.Zero(t, count)

	v, err = m.Version()
	require.NoError(t, err)
	require.Empty(t, v)
}

func TestMigrator_DownN_SkipPostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)
	_, err = m.Up()
	require.NoError(t, err)

	// rollback all except the first two
	migs := testmigrations.NonPostDeployment()
	n := len(migs) - 2
	second := migs[1]

	count, err := m.DownN(n)
	require.NoError(t, err)
	require.Equal(t, n, count)

	v, err := m.Version()
	require.NoError(t, err)
	require.Equal(t, second.Id, v)

	// resume and rollback the remaining two
	count, err = m.DownN(0)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	v, err = m.Version()
	require.NoError(t, err)
	require.Empty(t, v)

	// make sure it's idempotent
	count, err = m.DownN(100)
	require.NoError(t, err)
	require.Zero(t, count)

	v, err = m.Version()
	require.NoError(t, err)
	require.Empty(t, v)
}

func TestMigrator_DownNPlan(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	all := testmigrations.All()

	var allPlan []string

	for _, migration := range all {
		allPlan = append(allPlan, migration.Id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(allPlan))) // down migrations are applied in reverse order

	// plan all except the last two
	n := len(allPlan) - 1 - 2
	allExceptLastTwoPlan := allPlan[:n]

	plan, err := m.DownNPlan(n)
	require.NoError(t, err)
	require.Equal(t, allExceptLastTwoPlan, plan)

	// apply two migrations and re-plan all (the first two shouldn't be part of the plan anymore)
	_, err = m.DownN(2)
	require.NoError(t, err)

	plan, err = m.DownNPlan(0)
	require.NoError(t, err)

	allExceptFirstTwoPlan := allPlan[2:]
	require.Equal(t, allExceptFirstTwoPlan, plan)

	// make sure it's idempotent
	plan, err = m.DownNPlan(100)
	require.NoError(t, err)
	require.Equal(t, allExceptFirstTwoPlan, plan)
}

func TestMigrator_DownNPlan_SkipPostDeploymnet(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)
	_, err = m.Up()
	require.NoError(t, err)

	migs := testmigrations.NonPostDeployment()

	var migsPlan []string

	for _, migration := range migs {
		migsPlan = append(migsPlan, migration.Id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(migsPlan))) // down migrations are applied in reverse order

	// plan all except the last two
	n := len(migsPlan) - 1 - 2
	allExceptLastTwoPlan := migsPlan[:n]

	plan, err := m.DownNPlan(n)
	require.NoError(t, err)
	require.Equal(t, allExceptLastTwoPlan, plan)

	// apply two migrations and re-plan all (the first two shouldn't be part of the plan anymore)
	_, err = m.DownN(2)
	require.NoError(t, err)

	plan, err = m.DownNPlan(0)
	require.NoError(t, err)

	allExceptFirstTwoPlan := migsPlan[2:]
	require.Equal(t, allExceptFirstTwoPlan, plan)

	// make sure it's idempotent
	plan, err = m.DownNPlan(100)
	require.NoError(t, err)
	require.Equal(t, allExceptFirstTwoPlan, plan)
}

func TestMigrator_Status_Empty(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))

	all := testmigrations.All()

	statuses, err := m.Status()
	require.NoError(t, err)
	require.Len(t, statuses, len(all))

	var expectedIDs, actualIDs []string
	for _, m := range all {
		expectedIDs = append(expectedIDs, m.Id)
	}
	for id := range statuses {
		actualIDs = append(actualIDs, id)
	}
	require.ElementsMatch(t, expectedIDs, actualIDs)

	for _, s := range statuses {
		require.False(t, s.Unknown)
		require.Nil(t, s.AppliedAt)
	}
}

func TestMigrator_Status_Full(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	all := testmigrations.All()

	statuses, err := m.Status()
	require.NoError(t, err)
	require.Len(t, statuses, len(all))

	var expectedIDs, actualIDs []string
	for _, m := range all {
		expectedIDs = append(expectedIDs, m.Id)
	}
	for id := range statuses {
		actualIDs = append(actualIDs, id)
	}
	require.ElementsMatch(t, expectedIDs, actualIDs)

	for _, s := range statuses {
		require.False(t, s.Unknown)
		require.NotNil(t, s.AppliedAt)
	}
}

func TestMigrator_Status_PostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	all := testmigrations.All()

	statuses, err := m.Status()
	require.NoError(t, err)
	require.Len(t, statuses, len(all))

	// See migrations/testdata/fixtures/
	postDeploymentID := "20201027124302_create_post_migration_test_two_table"
	standardID := "20200319131542_create_manifests_test_table"

	postDeploymentStatus := statuses[postDeploymentID]
	require.NotNil(t, postDeploymentStatus)
	require.True(t, postDeploymentStatus.PostDeployment)

	standardStatus := statuses[standardID]
	require.NotNil(t, standardStatus)
	require.False(t, standardStatus.PostDeployment)
}

func TestMigrator_Status_Unknown(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	all := testmigrations.All()

	// temporarily insert fake migration record
	fakeID := "20060102150405_foo"
	fakeAppliedAt := time.Now()
	_, err = db.DB.Exec("INSERT INTO "+migrationTableName+" (id, applied_at) VALUES ($1, $2)", fakeID, fakeAppliedAt)
	require.NoError(t, err)
	defer db.DB.Exec("DELETE FROM "+migrationTableName+" WHERE id = $1", fakeID)

	statuses, err := m.Status()
	require.NoError(t, err)
	require.Len(t, statuses, len(all)+1)

	fakeStatus := statuses[fakeID]
	require.NotNil(t, fakeStatus)
	require.True(t, fakeStatus.Unknown)
	require.Equal(t, fakeAppliedAt.Round(time.Millisecond).UTC(), fakeStatus.AppliedAt.Round(time.Millisecond).UTC())
}

func TestMigrator_HasPending_No(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	pending, err := m.HasPending()
	require.NoError(t, err)
	require.False(t, pending)
}

func TestMigrator_HasPending_No_SkipPostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)
	_, err = m.Up()
	require.NoError(t, err)

	pending, err := m.HasPending()
	require.NoError(t, err)
	require.False(t, pending)
}

func TestMigrator_HasPending_Yes(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))
	_, err = m.Up()
	require.NoError(t, err)

	_, err = m.DownN(1)
	require.NoError(t, err)

	pending, err := m.HasPending()
	require.NoError(t, err)
	require.True(t, pending)
}

func TestMigrator_HasPending_Yes_PendingPostDeployment(t *testing.T) {
	db, err := testutil.NewDB()
	require.NoError(t, err)
	defer cleanupDB(t, db)

	m := migrations.NewMigrator(
		db.DB,
		migrations.Source(testmigrations.All()),
		migrations.SkipPostDeployment,
	)
	_, err = m.Up()
	require.NoError(t, err)

	m = migrations.NewMigrator(db.DB, migrations.Source(testmigrations.All()))

	pending, err := m.HasPending()
	require.NoError(t, err)
	require.True(t, pending)
}

func cleanupDB(t *testing.T, db *datastore.DB) {
	_, err := db.DB.Exec("DELETE FROM " + migrationTableName)
	require.NoError(t, err)

	require.NoError(t, db.Close())
}
