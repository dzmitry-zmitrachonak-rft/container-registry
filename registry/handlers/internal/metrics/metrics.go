package metrics

import (
	"time"

	"github.com/docker/distribution/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	migrationRoutingCounter *prometheus.CounterVec

	timeSince = time.Since // for test purposes only
)

const (
	subsystem = "http"

	codePathLabel    = "path"
	oldCodePathValue = "old"
	newCodePathValue = "new"

	migrationRouteTotalName = "request_migration_route_total"
	migrationRouteTotalDesc = "A counter for code path routing of requests during migration."
)

func init() {
	migrationRoutingCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metrics.NamespacePrefix,
			Subsystem: subsystem,
			Name:      migrationRouteTotalName,
			Help:      migrationRouteTotalDesc,
		},
		[]string{codePathLabel},
	)

	prometheus.MustRegister(migrationRoutingCounter)
}

func MigrationRoute(newCodePath bool) {
	var codePath string
	if newCodePath {
		codePath = newCodePathValue
	} else {
		codePath = oldCodePathValue
	}
	migrationRoutingCounter.WithLabelValues(codePath).Inc()
}
