package metrics

import (
	"strconv"
	"time"

	"github.com/docker/distribution/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	blobDownloadBytesHist *prometheus.HistogramVec

	timeSince = time.Since // for test purposes only
)

const (
	subsystem             = "storage"
	redirectLabel         = "redirect"
	blobDownloadBytesName = "blob_download_bytes"
	blobDownloadBytesDesc = "A histogram of blob download sizes for the storage backend."
)

func init() {
	blobDownloadBytesHist = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metrics.NamespacePrefix,
			Subsystem: subsystem,
			Name:      blobDownloadBytesName,
			Help:      blobDownloadBytesDesc,
			Buckets: []float64{
				512 * 1024,              // 512KiB
				1024 * 1024,             // 1MiB
				1024 * 1024 * 64,        // 64MiB
				1024 * 1024 * 128,       // 128MiB
				1024 * 1024 * 256,       // 256MiB
				1024 * 1024 * 512,       // 512MiB
				1024 * 1024 * 1024,      // 1GiB
				1024 * 1024 * 1024 * 2,  // 2GiB
				1024 * 1024 * 1024 * 3,  // 3GiB
				1024 * 1024 * 1024 * 4,  // 4GiB
				1024 * 1024 * 1024 * 5,  // 5GiB
				1024 * 1024 * 1024 * 6,  // 6GiB
				1024 * 1024 * 1024 * 7,  // 7GiB
				1024 * 1024 * 1024 * 8,  // 8GiB
				1024 * 1024 * 1024 * 9,  // 9GiB
				1024 * 1024 * 1024 * 10, // 10GiB
				1024 * 1024 * 1024 * 20, // 20GiB
				1024 * 1024 * 1024 * 30, // 30GiB
				1024 * 1024 * 1024 * 40, // 40GiB
				1024 * 1024 * 1024 * 50, // 50GiB
			},
		},
		[]string{redirectLabel},
	)

	prometheus.MustRegister(blobDownloadBytesHist)
}

func BlobDownload(redirect bool, size int64) {
	blobDownloadBytesHist.WithLabelValues(strconv.FormatBool(redirect)).Observe(float64(size))
}
