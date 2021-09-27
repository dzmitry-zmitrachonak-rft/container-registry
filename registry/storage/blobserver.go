package storage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/docker/distribution"
	"github.com/docker/distribution/log"
	"github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/internal/metrics"
	"github.com/opencontainers/go-digest"
)

// TODO(stevvooe): This should configurable in the future.
const blobCacheControlMaxAge = 365 * 24 * time.Hour

type redirect struct {
	enabled     bool          // allows toggling redirects to storage backends for blob downloads
	expiryDelay time.Duration // allows setting a custom delay for the presigned URLs expiration (defaults to 20m)
}

// blobServer simply serves blobs from a driver instance using a path function
// to identify paths and a descriptor service to fill in metadata.
type blobServer struct {
	driver   driver.StorageDriver
	statter  distribution.BlobStatter
	pathFn   func(dgst digest.Digest) (string, error)
	redirect redirect
}

func (bs *blobServer) ServeBlob(ctx context.Context, w http.ResponseWriter, r *http.Request, dgst digest.Digest) error {
	desc, err := bs.statter.Stat(ctx, dgst)
	if err != nil {
		return err
	}

	path, err := bs.pathFn(desc.Digest)
	if err != nil {
		return err
	}

	l := log.GetLogger(log.WithContext(ctx)).WithFields(log.Fields{
		"size_bytes": desc.Size,
		"digest":     desc.Digest,
	})

	if bs.redirect.enabled {
		opts := map[string]interface{}{"method": r.Method}
		if bs.redirect.expiryDelay > 0 {
			opts["expiry"] = time.Now().Add(bs.redirect.expiryDelay)
		}
		redirectURL, err := bs.driver.URLFor(ctx, path, opts)
		switch err.(type) {
		case nil:
			// Redirect to storage URL.
			http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
			metrics.BlobDownload(true, desc.Size)
			if r.Method == http.MethodGet {
				l.WithFields(log.Fields{"redirect": true}).Info("blob downloaded")
			}
			return nil

		case driver.ErrUnsupportedMethod:
			// Fallback to serving the content directly.
		default:
			// Some unexpected error.
			return err
		}
	}

	br, err := newFileReader(ctx, bs.driver, path, desc.Size)
	if err != nil {
		return err
	}
	defer br.Close()

	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, desc.Digest)) // If-None-Match handled by ServeContent
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%.f", blobCacheControlMaxAge.Seconds()))

	if w.Header().Get("Docker-Content-Digest") == "" {
		w.Header().Set("Docker-Content-Digest", desc.Digest.String())
	}

	if w.Header().Get("Content-Type") == "" {
		// Set the content type if not already set.
		w.Header().Set("Content-Type", desc.MediaType)
	}

	if w.Header().Get("Content-Length") == "" {
		// Set the content length if not already set.
		w.Header().Set("Content-Length", fmt.Sprint(desc.Size))
	}

	http.ServeContent(w, r, desc.Digest.String(), time.Time{}, br)
	metrics.BlobDownload(false, desc.Size)
	if r.Method == http.MethodGet {
		l.WithFields(log.Fields{"redirect": false}).Info("blob downloaded")
	}

	return nil
}
