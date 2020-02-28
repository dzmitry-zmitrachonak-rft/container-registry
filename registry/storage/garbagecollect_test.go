package storage

import (
	"io"
	"path"
	"testing"

	"github.com/docker/distribution"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/driver/inmemory"
	"github.com/docker/distribution/testutil"
	"github.com/docker/libtrust"
	"github.com/opencontainers/go-digest"
)

func createRegistry(t *testing.T, driver driver.StorageDriver, options ...RegistryOption) distribution.Namespace {
	ctx := context.Background()
	k, err := libtrust.GenerateECP256PrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	options = append([]RegistryOption{EnableDelete, Schema1SigningKey(k), EnableSchema1}, options...)
	registry, err := NewRegistry(ctx, driver, options...)
	if err != nil {
		t.Fatalf("Failed to construct namespace")
	}
	return registry
}

func makeRepository(t *testing.T, registry distribution.Namespace, name string) distribution.Repository {
	ctx := context.Background()

	// Initialize a dummy repository
	named, err := reference.WithName(name)
	if err != nil {
		t.Fatalf("Failed to parse name %s:  %v", name, err)
	}

	repo, err := registry.Repository(ctx, named)
	if err != nil {
		t.Fatalf("Failed to construct repository: %v", err)
	}
	return repo
}

func allManifests(t *testing.T, manifestService distribution.ManifestService) map[digest.Digest]struct{} {
	ctx := context.Background()
	allManSet := newSyncDigestSet()
	manifestEnumerator, ok := manifestService.(distribution.ManifestEnumerator)
	if !ok {
		t.Fatalf("unable to convert ManifestService into ManifestEnumerator")
	}
	err := manifestEnumerator.Enumerate(ctx, func(dgst digest.Digest) error {
		allManSet.add(dgst)
		return nil
	})
	if err != nil {
		t.Fatalf("Error getting all manifests: %v", err)
	}
	return allManSet.members
}

func allBlobs(t *testing.T, registry distribution.Namespace) map[digest.Digest]struct{} {
	ctx := context.Background()
	blobService := registry.Blobs()
	allBlobsSet := newSyncDigestSet()
	err := blobService.Enumerate(ctx, func(desc distribution.Descriptor) error {
		allBlobsSet.add(desc.Digest)
		return nil
	})
	if err != nil {
		t.Fatalf("Error getting all blobs: %v", err)
	}
	return allBlobsSet.members
}

func TestNoDeletionNoEffect(t *testing.T) {
	ctx := context.Background()
	inmemoryDriver := inmemory.New()

	registry := createRegistry(t, inmemoryDriver)
	repo := makeRepository(t, registry, "palailogos")
	manifestService, _ := repo.Manifests(ctx)

	image1, err := testutil.UploadRandomSchema1Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema1 image: %v", err)
	}
	image2, err := testutil.UploadRandomSchema1Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema1 image: %v", err)
	}
	_, err = testutil.UploadRandomSchema2Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema2 image: %v", err)
	}

	// construct manifestlist for fun.
	blobstatter := registry.BlobStatter()
	manifestList, err := testutil.MakeManifestList(blobstatter, []digest.Digest{
		image1.ManifestDigest, image2.ManifestDigest})
	if err != nil {
		t.Fatalf("Failed to make manifest list: %v", err)
	}

	_, err = manifestService.Put(ctx, manifestList)
	if err != nil {
		t.Fatalf("Failed to add manifest list: %v", err)
	}

	before := allBlobs(t, registry)

	// Run GC
	err = MarkAndSweep(context.Background(), inmemoryDriver, registry, GCOpts{
		DryRun:         false,
		RemoveUntagged: false,
	})
	if err != nil {
		t.Fatalf("Failed mark and sweep: %v", err)
	}

	after := allBlobs(t, registry)
	if len(before) != len(after) {
		t.Fatalf("Garbage collection affected storage: %d != %d", len(before), len(after))
	}
}

func TestDeleteManifestIfTagNotFound(t *testing.T) {
	ctx := context.Background()
	inmemoryDriver := inmemory.New()

	registry := createRegistry(t, inmemoryDriver)
	repo := makeRepository(t, registry, "deletemanifests")
	manifestService, _ := repo.Manifests(ctx)

	// Create random layers
	randomLayers1, err := testutil.CreateRandomLayers(3)
	if err != nil {
		t.Fatalf("failed to make layers: %v", err)
	}

	randomLayers2, err := testutil.CreateRandomLayers(3)
	if err != nil {
		t.Fatalf("failed to make layers: %v", err)
	}

	// Upload all layers
	err = testutil.UploadBlobs(repo, randomLayers1)
	if err != nil {
		t.Fatalf("failed to upload layers: %v", err)
	}

	err = testutil.UploadBlobs(repo, randomLayers2)
	if err != nil {
		t.Fatalf("failed to upload layers: %v", err)
	}

	// Construct manifests
	manifest1, err := testutil.MakeSchema1Manifest(getKeys(randomLayers1))
	if err != nil {
		t.Fatalf("failed to make manifest: %v", err)
	}

	manifest2, err := testutil.MakeSchema1Manifest(getKeys(randomLayers2))
	if err != nil {
		t.Fatalf("failed to make manifest: %v", err)
	}

	_, err = manifestService.Put(ctx, manifest1)
	if err != nil {
		t.Fatalf("manifest upload failed: %v", err)
	}

	_, err = manifestService.Put(ctx, manifest2)
	if err != nil {
		t.Fatalf("manifest upload failed: %v", err)
	}

	manifestEnumerator, _ := manifestService.(distribution.ManifestEnumerator)
	manifestEnumerator.Enumerate(ctx, func(dgst digest.Digest) error {
		repo.Tags(ctx).Tag(ctx, "test", distribution.Descriptor{Digest: dgst})
		return nil
	})

	before1 := allBlobs(t, registry)
	before2 := allManifests(t, manifestService)

	// run GC with dry-run (should not remove anything)
	err = MarkAndSweep(context.Background(), inmemoryDriver, registry, GCOpts{
		DryRun:         true,
		RemoveUntagged: true,
	})
	if err != nil {
		t.Fatalf("Failed mark and sweep: %v", err)
	}
	afterDry1 := allBlobs(t, registry)
	afterDry2 := allManifests(t, manifestService)
	if len(before1) != len(afterDry1) {
		t.Fatalf("Garbage collection affected blobs storage: %d != %d", len(before1), len(afterDry1))
	}
	if len(before2) != len(afterDry2) {
		t.Fatalf("Garbage collection affected manifest storage: %d != %d", len(before2), len(afterDry2))
	}

	// Run GC (removes everything because no manifests with tags exist)
	err = MarkAndSweep(context.Background(), inmemoryDriver, registry, GCOpts{
		DryRun:         false,
		RemoveUntagged: true,
	})
	if err != nil {
		t.Fatalf("Failed mark and sweep: %v", err)
	}

	after1 := allBlobs(t, registry)
	after2 := allManifests(t, manifestService)
	if len(before1) == len(after1) {
		t.Fatalf("Garbage collection affected blobs storage: %d == %d", len(before1), len(after1))
	}
	if len(before2) == len(after2) {
		t.Fatalf("Garbage collection affected manifest storage: %d == %d", len(before2), len(after2))
	}
}

func TestGCWithMissingManifests(t *testing.T) {
	ctx := context.Background()
	d := inmemory.New()

	registry := createRegistry(t, d)
	repo := makeRepository(t, registry, "testrepo")
	_, err := testutil.UploadRandomSchema1Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema1 image: %v", err)
	}

	// Simulate a missing _manifests directory
	revPath, err := pathFor(manifestRevisionsPathSpec{"testrepo"})
	if err != nil {
		t.Fatal(err)
	}

	_manifestsPath := path.Dir(revPath)
	err = d.Delete(ctx, _manifestsPath)
	if err != nil {
		t.Fatal(err)
	}

	err = MarkAndSweep(context.Background(), d, registry, GCOpts{
		DryRun:         false,
		RemoveUntagged: false,
	})
	if err != nil {
		t.Fatalf("Failed mark and sweep: %v", err)
	}

	blobs := allBlobs(t, registry)
	if len(blobs) > 0 {
		t.Errorf("unexpected blobs after gc")
	}
}

func TestDeletionHasEffect(t *testing.T) {
	ctx := context.Background()
	inmemoryDriver := inmemory.New()

	registry := createRegistry(t, inmemoryDriver)
	repo := makeRepository(t, registry, "komnenos")
	manifests, _ := repo.Manifests(ctx)

	image1, err := testutil.UploadRandomSchema1Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema1 image: %v", err)
	}
	image2, err := testutil.UploadRandomSchema1Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema1 image: %v", err)
	}
	image3, err := testutil.UploadRandomSchema2Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema2 image: %v", err)
	}

	manifests.Delete(ctx, image2.ManifestDigest)
	manifests.Delete(ctx, image3.ManifestDigest)

	// Run GC
	err = MarkAndSweep(context.Background(), inmemoryDriver, registry, GCOpts{
		DryRun:         false,
		RemoveUntagged: false,
	})
	if err != nil {
		t.Fatalf("Failed mark and sweep: %v", err)
	}

	blobs := allBlobs(t, registry)

	// check that the image1 manifest and all the layers are still in blobs
	if _, ok := blobs[image1.ManifestDigest]; !ok {
		t.Fatalf("First manifest is missing")
	}

	for layer := range image1.Layers {
		if _, ok := blobs[layer]; !ok {
			t.Fatalf("manifest 1 layer is missing: %v", layer)
		}
	}

	// check that image2 and image3 layers are not still around
	for layer := range image2.Layers {
		if _, ok := blobs[layer]; ok {
			t.Fatalf("manifest 2 layer is present: %v", layer)
		}
	}

	for layer := range image3.Layers {
		if _, ok := blobs[layer]; ok {
			t.Fatalf("manifest 3 layer is present: %v", layer)
		}
	}
}

func getAnyKey(digests map[digest.Digest]io.ReadSeeker) (d digest.Digest) {
	for d = range digests {
		break
	}
	return
}

func getKeys(digests map[digest.Digest]io.ReadSeeker) (ds []digest.Digest) {
	for d := range digests {
		ds = append(ds, d)
	}
	return
}

func TestDeletionWithSharedLayer(t *testing.T) {
	ctx := context.Background()
	inmemoryDriver := inmemory.New()

	registry := createRegistry(t, inmemoryDriver)
	repo := makeRepository(t, registry, "tzimiskes")

	// Create random layers
	randomLayers1, err := testutil.CreateRandomLayers(3)
	if err != nil {
		t.Fatalf("failed to make layers: %v", err)
	}

	randomLayers2, err := testutil.CreateRandomLayers(3)
	if err != nil {
		t.Fatalf("failed to make layers: %v", err)
	}

	// Upload all layers
	err = testutil.UploadBlobs(repo, randomLayers1)
	if err != nil {
		t.Fatalf("failed to upload layers: %v", err)
	}

	err = testutil.UploadBlobs(repo, randomLayers2)
	if err != nil {
		t.Fatalf("failed to upload layers: %v", err)
	}

	// Construct manifests
	manifest1, err := testutil.MakeSchema1Manifest(getKeys(randomLayers1))
	if err != nil {
		t.Fatalf("failed to make manifest: %v", err)
	}

	sharedKey := getAnyKey(randomLayers1)
	manifest2, err := testutil.MakeSchema2Manifest(repo, append(getKeys(randomLayers2), sharedKey))
	if err != nil {
		t.Fatalf("failed to make manifest: %v", err)
	}

	manifestService, err := testutil.MakeManifestService(repo)
	if err != nil {
		t.Fatalf("failed to make manifest service: %v", err)
	}

	// Upload manifests
	_, err = manifestService.Put(ctx, manifest1)
	if err != nil {
		t.Fatalf("manifest upload failed: %v", err)
	}

	manifestDigest2, err := manifestService.Put(ctx, manifest2)
	if err != nil {
		t.Fatalf("manifest upload failed: %v", err)
	}

	// delete
	err = manifestService.Delete(ctx, manifestDigest2)
	if err != nil {
		t.Fatalf("manifest deletion failed: %v", err)
	}

	// check that all of the layers in layer 1 are still there
	blobs := allBlobs(t, registry)
	for dgst := range randomLayers1 {
		if _, ok := blobs[dgst]; !ok {
			t.Fatalf("random layer 1 blob missing: %v", dgst)
		}
	}
}

func TestOrphanBlobDeleted(t *testing.T) {
	inmemoryDriver := inmemory.New()

	registry := createRegistry(t, inmemoryDriver)
	repo := makeRepository(t, registry, "michael_z_doukas")

	digests, err := testutil.CreateRandomLayers(1)
	if err != nil {
		t.Fatalf("Failed to create random digest: %v", err)
	}

	if err = testutil.UploadBlobs(repo, digests); err != nil {
		t.Fatalf("Failed to upload blob: %v", err)
	}

	// formality to create the necessary directories
	testutil.UploadRandomSchema2Image(repo)
	if err != nil {
		t.Fatalf("failed to upload random schema2 image: %v", err)
	}

	// Run GC
	err = MarkAndSweep(context.Background(), inmemoryDriver, registry, GCOpts{
		DryRun:         false,
		RemoveUntagged: false,
	})
	if err != nil {
		t.Fatalf("Failed mark and sweep: %v", err)
	}

	blobs := allBlobs(t, registry)

	// check that orphan blob layers are not still around
	for dgst := range digests {
		if _, ok := blobs[dgst]; ok {
			t.Fatalf("Orphan layer is present: %v", dgst)
		}
	}
}

// TestGarbageCollectAfterLastTagRemoved was added to validate the scenario in which the last tag from the repository
// is removed which in turn removes the <repository>/_manifests/tags folder. This was throwing a distribution.ErrRepositoryUnknown
// error that is now being captured in garbagecollect.MarkAndSweep.
// https://gitlab.com/gitlab-org/gitlab/issues/28201
func TestGarbageCollectAfterLastTagRemoved(t *testing.T) {
	ctx := context.Background()
	inmemoryDriver := inmemory.New()

	registry := createRegistry(t, inmemoryDriver)
	repo := makeRepository(t, registry, "testgarbagecollectafterlasttagremoved")

	manifestService, err := repo.Manifests(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize the manifestService: %v", err)
	}

	// Setup for tests
	randomLayers1, err := testutil.CreateRandomLayers(3)
	if err != nil {
		t.Fatalf("failed to make layers: %v", err)
	}

	err = testutil.UploadBlobs(repo, randomLayers1)
	if err != nil {
		t.Fatalf("failed to upload layers: %v", err)
	}

	manifest1, err := testutil.MakeSchema1Manifest(getKeys(randomLayers1))
	if err != nil {
		t.Fatalf("failed to make manifest: %v", err)
	}

	_, err = manifestService.Put(ctx, manifest1)
	if err != nil {
		t.Fatalf("manifest upload failed: %v", err)
	}

	manifestEnumerator, _ := manifestService.(distribution.ManifestEnumerator)
	manifestEnumerator.Enumerate(ctx, func(dgst digest.Digest) error {
		repo.Tags(ctx).Tag(ctx, "testTag", distribution.Descriptor{Digest: dgst})
		return nil
	})
	// -- End setup

	// Delete the repository's _manifests/tags path
	tagsPath, err := pathFor(manifestTagsPathSpec{"testgarbagecollectafterlasttagremoved"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf(tagsPath)
	err = inmemoryDriver.Delete(ctx, tagsPath)
	if err != nil {
		t.Fatal(err)
	}

	// Run garbage collection with tags folder removed to validate error handling
	err = MarkAndSweep(context.Background(), inmemoryDriver, registry, GCOpts{
		DryRun:         false,
		RemoveUntagged: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Assert that no blobs or manifests were left behind (because the only manifest has no tags path)
	afterBlobs := allBlobs(t, registry)
	if len(afterBlobs) != 0 {
		t.Fatalf("No blobs should be left behind: %d remaining", len(afterBlobs))
	}

	afterManifests := allManifests(t, manifestService)
	if len(afterManifests) != 0 {
		t.Fatalf("No manifests should be left behind: %d remaining", len(afterManifests))
	}
}
