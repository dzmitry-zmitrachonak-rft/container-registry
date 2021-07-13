package metrics

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/docker/distribution/metrics"
	"github.com/prometheus/client_golang/prometheus"
	testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func mockTimeSince(d time.Duration) func() {
	bkp := timeSince
	timeSince = func(_ time.Time) time.Duration { return d }
	return func() { timeSince = bkp }
}

func TestMigrationRoute(t *testing.T) {
	restore := mockTimeSince(10 * time.Millisecond)
	defer restore()

	MigrationRoute(true)
	MigrationRoute(true)

	mockTimeSince(20 * time.Millisecond)
	MigrationRoute(false)

	var expected bytes.Buffer
	expected.WriteString(`
# HELP registry_http_request_migration_route_total A counter for code path routing of requests during migration.
# TYPE registry_http_request_migration_route_total counter
registry_http_request_migration_route_total{path="new"} 2
registry_http_request_migration_route_total{path="old"} 1
`)
	fullName := fmt.Sprintf("%s_%s_%s", metrics.NamespacePrefix, subsystem, migrationRouteTotalName)

	err := testutil.GatherAndCompare(prometheus.DefaultGatherer, &expected, fullName)
	require.NoError(t, err)
}
