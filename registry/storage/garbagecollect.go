package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"
	log "github.com/sirupsen/logrus"

	"github.com/docker/distribution"
	"github.com/docker/distribution/registry/storage/driver"
)

// GCOpts contains options for garbage collector
type GCOpts struct {
	DryRun         bool
	RemoveUntagged bool
}

// ManifestDel contains manifest structure which will be deleted
type ManifestDel struct {
	Name   string
	Digest digest.Digest
	Tags   []string
}

// MarkAndSweep performs a mark and sweep of registry data
func MarkAndSweep(ctx context.Context, storageDriver driver.StorageDriver, registry distribution.Namespace, opts GCOpts) error {
	blobService := registry.Blobs()

	countChan := make(chan struct{})
	countDone := make(chan struct{})
	var count int
	go func() {
		for range countChan {
			count++
		}
		countDone <- struct{}{}
	}()

	sizeChan := make(chan int64)
	sizeDone := make(chan struct{})
	var totalSizeBytes int64
	go func() {
		for size := range sizeChan {
			totalSizeBytes += size
		}
		sizeDone <- struct{}{}
	}()

	start := time.Now()
	log.Info("starting")

	err := blobService.Enumerate(ctx, func(desc distribution.Descriptor) error {
		countChan <- struct{}{}
		sizeChan <- desc.Size
		return nil
	})
	if err != nil {
		return fmt.Errorf("error enumerating blobs: %v", err)
	}

	close(countChan)
	<-countDone
	close(sizeChan)
	<-sizeDone

	log.WithFields(log.Fields{
		"duration_s":            time.Since(start),
		"blob_count":            count,
		"blob_total_size_bytes": totalSizeBytes,
	}).Info("complete")

	return nil
}
