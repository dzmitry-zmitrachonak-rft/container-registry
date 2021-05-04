// +build integration

package datastore_test

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/datastore/models"
	"github.com/docker/distribution/registry/datastore/testutil"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func randomDigest(t testing.TB) digest.Digest {
	t.Helper()

	rand.Seed(time.Now().UnixNano())
	data := make([]byte, 100)
	_, err := rand.Read(data)
	require.NoError(t, err)

	return digest.FromBytes(data)
}

func randomBlob(t testing.TB) *models.Blob {
	t.Helper()

	rand.Seed(time.Now().UnixNano())
	return &models.Blob{
		MediaType: "application/octet-stream",
		Digest:    randomDigest(t),
		Size:      rand.Int63(),
	}
}

func randomRepository(t testing.TB) *models.Repository {
	t.Helper()

	rand.Seed(time.Now().UnixNano())
	n := strconv.Itoa(rand.Int())
	return &models.Repository{
		Name: n,
		Path: n,
	}
}

func randomManifest(t testing.TB, r *models.Repository, configBlob *models.Blob) *models.Manifest {
	t.Helper()

	m := &models.Manifest{
		NamespaceID:   r.NamespaceID,
		RepositoryID:  r.ID,
		SchemaVersion: 2,
		MediaType:     schema2.MediaTypeManifest,
		Digest:        randomDigest(t),
		Payload:       models.Payload(`{"foo": "bar"}`),
	}
	if configBlob != nil {
		m.Configuration = &models.Configuration{
			MediaType: schema2.MediaTypeImageConfig,
			Digest:    configBlob.Digest,
			Payload:   models.Payload(`{"foo": "bar"}`),
		}
	}

	return m
}

func TestGC_TrackBlobUploads(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err := bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// Check that a corresponding task was created and scheduled for 1 day ahead. This is done by the
	// `gc_track_blob_uploads` trigger/function
	brs := datastore.NewGCBlobTaskStore(suite.db)
	rr, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))
	require.Equal(t, &models.GCBlobTask{
		ReviewAfter: b.CreatedAt.Add(24 * time.Hour),
		ReviewCount: 0,
		Digest:      b.Digest,
	}, rr[0])
}

func TestGC_TrackBlobUploads_PostponeReviewOnConflict(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err := bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// delete it
	err = bs.Delete(suite.ctx, b.Digest)
	require.NoError(t, err)

	// grab existing review record (should be preserved, despite the blob deletion)
	brs := datastore.NewGCBlobTaskStore(suite.db)
	rr, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))

	// re-create blob
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// check that we still have only one review record but its due date was postponed to now (re-create time) + 1 day
	rr2, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr2))
	require.Equal(t, rr[0].ReviewCount, rr2[0].ReviewCount)
	require.Equal(t, rr[0].Digest, rr2[0].Digest)
	// this is fast, so review_after is only a few milliseconds ahead of the original time
	require.True(t, rr2[0].ReviewAfter.After(rr[0].ReviewAfter))
	require.WithinDuration(t, rr[0].ReviewAfter, rr2[0].ReviewAfter, 200*time.Millisecond)
}

func TestGC_TrackBlobUploads_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackBlobUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// check that no review records were created
	brs := datastore.NewGCBlobTaskStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackConfigurationBlobs(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create config blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, b)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// Check that a corresponding task was created and scheduled for 1 day ahead. This is done by the
	// `gc_track_configuration_blobs` trigger/function
	brs := datastore.NewGCConfigLinkStore(suite.db)
	rr, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))
	require.NotEmpty(t, rr[0].ID)
	require.Equal(t, r.ID, rr[0].RepositoryID)
	require.Equal(t, m.ID, rr[0].ManifestID)
	require.Equal(t, b.Digest, rr[0].Digest)
}

func TestGC_TrackConfigurationBlobs_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackConfigurationBlobsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create config blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, b)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// check that no records were created
	brs := datastore.NewGCConfigLinkStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackLayerBlobs(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create layer blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// associate layer with manifest
	err = ms.AssociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// Check that a corresponding row was created. This is done by the `gc_track_layer_blobs` trigger/function
	brs := datastore.NewGCLayerLinkStore(suite.db)
	ll, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(ll))
	require.NotEmpty(t, ll[0].ID)
	require.Equal(t, r.ID, ll[0].RepositoryID)
	require.Equal(t, int64(1), ll[0].LayerID)
	require.Equal(t, b.Digest, ll[0].Digest)
}

func TestGC_TrackLayerBlobs_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackLayerBlobsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create layer blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// associate layer with manifest
	err = ms.AssociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// check that no records were created
	brs := datastore.NewGCConfigLinkStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackManifestUploads(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repository
	rs := datastore.NewRepositoryStore(suite.db)
	r := randomRepository(t)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// Check that a corresponding task was created and scheduled for 1 day ahead. This is done by the
	// `gc_track_manifest_uploads` trigger/function
	brs := datastore.NewGCManifestTaskStore(suite.db)
	tt, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(tt))
	require.Equal(t, &models.GCManifestTask{
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
		ReviewAfter:  m.CreatedAt.Add(24 * time.Hour),
		ReviewCount:  0,
	}, tt[0])
}

func TestGC_TrackManifestUploads_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repository
	rs := datastore.NewRepositoryStore(suite.db)
	r := randomRepository(t)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// check that no review records were created
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := mrs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackDeletedManifests(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// disable other triggers that also insert on gc_blob_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackConfigurationBlobsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()
	enable, err = testutil.GCTrackBlobUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create config blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)
	err = rs.LinkBlob(suite.ctx, r, b.Digest)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, b)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// confirm that the review queue remains empty
	brs := datastore.NewGCBlobTaskStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	// delete manifest
	ok, err := rs.DeleteManifest(suite.ctx, r, m.Digest)
	require.NoError(t, err)
	require.True(t, ok)

	// check that a corresponding task was created for the config blob and scheduled for 1 day ahead
	tt, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(tt))
	require.Equal(t, 0, tt[0].ReviewCount)
	require.Equal(t, b.Digest, tt[0].Digest)
	// ignore the few milliseconds between blob creation and queueing for review in response to the manifest delete
	require.WithinDuration(t, tt[0].ReviewAfter, b.CreatedAt.Add(24*time.Hour), 200*time.Millisecond)
}

func TestGC_TrackDeletedManifests_PostponeReviewOnConflict(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create config blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)
	err = rs.LinkBlob(suite.ctx, r, b.Digest)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, b)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// grab existing review record (created by the gc_track_blob_uploads_trigger trigger)
	brs := datastore.NewGCBlobTaskStore(suite.db)
	rr, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))

	// delete manifest
	ok, err := rs.DeleteManifest(suite.ctx, r, m.Digest)
	require.NoError(t, err)
	require.True(t, ok)

	// check that we still have only one review record but its due date was postponed to now (delete time) + 1 day
	rr2, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr2))
	require.Equal(t, rr[0].ReviewCount, rr2[0].ReviewCount)
	require.Equal(t, rr[0].Digest, rr2[0].Digest)
	// this is fast, so review_after is only a few milliseconds ahead of the original time
	require.True(t, rr2[0].ReviewAfter.After(rr[0].ReviewAfter))
	require.LessOrEqual(t, rr2[0].ReviewAfter.Sub(rr[0].ReviewAfter).Milliseconds(), int64(200))
}

func TestGC_TrackDeletedManifests_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackDeletedManifestsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()
	// disable other triggers that also insert on gc_blob_review_queue so that they don't interfere with this test
	enable, err = testutil.GCTrackConfigurationBlobsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()
	enable, err = testutil.GCTrackBlobUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create config blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)
	err = rs.LinkBlob(suite.ctx, r, b.Digest)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, b)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// delete manifest
	ok, err := rs.DeleteManifest(suite.ctx, r, m.Digest)
	require.NoError(t, err)
	require.True(t, ok)

	// check that no review records were created
	brs := datastore.NewGCBlobTaskStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackDeletedLayers(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// disable other triggers that also insert on gc_blob_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackBlobUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create layer blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)
	err = rs.LinkBlob(suite.ctx, r, b.Digest)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// associate layer with manifest
	err = ms.AssociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// confirm that the review queue remains empty
	brs := datastore.NewGCBlobTaskStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	// dissociate layer blob
	err = ms.DissociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// check that a corresponding task was created for the layer blob and scheduled for 1 day ahead
	tt, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(tt))
	require.Equal(t, 0, tt[0].ReviewCount)
	require.Equal(t, b.Digest, tt[0].Digest)
	// ignore the few milliseconds between blob creation and queueing for review in response to the layer dissociation
	require.WithinDuration(t, tt[0].ReviewAfter, b.CreatedAt.Add(24*time.Hour), 200*time.Millisecond)
}

func TestGC_TrackDeletedLayers_PostponeReviewOnConflict(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create layer blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)
	err = rs.LinkBlob(suite.ctx, r, b.Digest)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// associate layer with manifest
	err = ms.AssociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// grab existing review record (created by the gc_track_blob_uploads_trigger trigger)
	brs := datastore.NewGCBlobTaskStore(suite.db)
	rr, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))

	// dissociate layer blob
	err = ms.DissociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// check that we still have only one review record but its due date was postponed to now (delete time) + 1 day
	rr2, err := brs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr2))
	require.Equal(t, rr[0].ReviewCount, rr2[0].ReviewCount)
	require.Equal(t, rr[0].Digest, rr2[0].Digest)
	// this is fast, so review_after is only a few milliseconds ahead of the original time
	require.True(t, rr2[0].ReviewAfter.After(rr[0].ReviewAfter))
	require.LessOrEqual(t, rr2[0].ReviewAfter.Sub(rr[0].ReviewAfter).Milliseconds(), int64(200))
}

func TestGC_TrackDeletedLayers_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackDeletedLayersTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()
	// disable other triggers that also insert on gc_blob_review_queue so that they don't interfere with this test
	enable, err = testutil.GCTrackBlobUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create layer blob
	bs := datastore.NewBlobStore(suite.db)
	b := randomBlob(t)
	err = bs.Create(suite.ctx, b)
	require.NoError(t, err)
	err = rs.LinkBlob(suite.ctx, r, b.Digest)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// associate layer with manifest
	err = ms.AssociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// dissociate layer blob
	err = ms.DissociateLayerBlob(suite.ctx, m, b)
	require.NoError(t, err)

	// check that no review records were created
	brs := datastore.NewGCBlobTaskStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackDeletedManifestLists(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// create manifest list
	ml := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, ml)
	require.NoError(t, err)
	err = ms.AssociateManifest(suite.ctx, ml, m)
	require.NoError(t, err)

	// confirm that the review queue remains empty
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := mrs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	// delete manifest list
	ok, err := rs.DeleteManifest(suite.ctx, r, ml.Digest)
	require.NoError(t, err)
	require.True(t, ok)

	// Check that a corresponding task was created and scheduled for 1 day ahead. This is done by the
	// `gc_track_deleted_manifest_lists` trigger/function
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))
	require.Equal(t, r.ID, rr[0].RepositoryID)
	require.Equal(t, m.ID, rr[0].ManifestID)
	require.Equal(t, 0, rr[0].ReviewCount)
	// ignore the few milliseconds between now and queueing for review in response to the manifest list delete
	require.WithinDuration(t, rr[0].ReviewAfter, time.Now().Add(24*time.Hour), 100*time.Millisecond)
}

func TestGC_TrackDeletedManifestLists_PostponeReviewOnConflict(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// create manifest list
	ml := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, ml)
	require.NoError(t, err)
	err = ms.AssociateManifest(suite.ctx, ml, m)
	require.NoError(t, err)

	// Grab existing review records, one for the manifest and another for the manifest list (created by the
	// gc_track_manifest_uploads trigger)
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 2, len(rr))

	// Grab the review record for the child manifest
	require.Equal(t, m.ID, rr[0].ManifestID)

	// delete manifest list
	ok, err := rs.DeleteManifest(suite.ctx, r, ml.Digest)
	require.NoError(t, err)
	require.True(t, ok)

	// check that we still have only one review record for m but its due date was postponed to now (delete time) + 1 day
	rr2, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr2)) // the manifest list delete cascaded and deleted its review record as well
	require.Equal(t, rr[0].RepositoryID, rr2[0].RepositoryID)
	require.Equal(t, rr[0].ManifestID, rr2[0].ManifestID)
	require.Equal(t, rr[0].ReviewCount, rr2[0].ReviewCount)
	// review_after should be a few milliseconds ahead of the original time
	require.True(t, rr2[0].ReviewAfter.After(rr[0].ReviewAfter))
	require.WithinDuration(t, rr2[0].ReviewAfter, rr[0].ReviewAfter, 200*time.Millisecond)
}

func TestGC_TrackDeletedManifestLists_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackDeletedManifestListsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()
	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err = testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// create manifest list
	ml := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, ml)
	require.NoError(t, err)
	err = ms.AssociateManifest(suite.ctx, ml, m)
	require.NoError(t, err)

	// delete manifest list
	ok, err := rs.DeleteManifest(suite.ctx, r, ml.Digest)
	require.NoError(t, err)
	require.True(t, ok)

	// check that no review records were created
	brs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := brs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackSwitchedTags(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest
	ts := datastore.NewTagStore(suite.db)
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
	})
	require.NoError(t, err)

	// confirm that the review queue remains empty
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := mrs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	// create another manifest
	m2 := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m2)
	require.NoError(t, err)

	// switch tag to new manifest
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m2.ID,
	})
	require.NoError(t, err)

	// check that a corresponding task was created for the manifest and scheduled for 1 day ahead
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))
	require.Equal(t, r.ID, rr[0].RepositoryID)
	require.Equal(t, m.ID, rr[0].ManifestID)
	require.Equal(t, 0, rr[0].ReviewCount)
	// ignore the few milliseconds between manifest creation and queueing for review in response to the tag deletion
	require.WithinDuration(t, rr[0].ReviewAfter, m.CreatedAt.Add(24*time.Hour), 200*time.Millisecond)
}

func TestGC_TrackSwitchedTags_PostponeReviewOnConflict(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest
	ts := datastore.NewTagStore(suite.db)
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
	})
	require.NoError(t, err)

	// grab existing review record (created by the gc_track_manifest_uploads trigger)
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))

	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create another manifest
	m2 := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m2)
	require.NoError(t, err)

	// switch tag to new manifest
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m2.ID,
	})
	require.NoError(t, err)

	// check that we still have only one review record but its due date was postponed to now (delete time) + 1 day
	rr2, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr2))
	require.Equal(t, rr[0].RepositoryID, rr2[0].RepositoryID)
	require.Equal(t, rr[0].ManifestID, rr2[0].ManifestID)
	require.Equal(t, 0, rr2[0].ReviewCount)
	// review_after is only a few milliseconds ahead of the original time
	require.True(t, rr2[0].ReviewAfter.After(rr[0].ReviewAfter))
	require.WithinDuration(t, rr[0].ReviewAfter, rr2[0].ReviewAfter, 100*time.Millisecond)
}

func TestGC_TrackSwitchedTags_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackSwitchedTagsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()
	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err = testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest
	ts := datastore.NewTagStore(suite.db)
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
	})
	require.NoError(t, err)

	// create another manifest
	m2 := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m2)
	require.NoError(t, err)

	// switch tag to new manifest
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m2.ID,
	})
	require.NoError(t, err)

	// check that no review records were created
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := mrs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGC_TrackDeletedTags(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest
	ts := datastore.NewTagStore(suite.db)
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
	})
	require.NoError(t, err)

	// confirm that the review queue remains empty
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := mrs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	// delete tag
	ok, err := rs.DeleteTagByName(suite.ctx, r, "latest")
	require.NoError(t, err)
	require.True(t, ok)

	// check that a corresponding task was created for the manifest and scheduled for 1 day ahead
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))
	require.Equal(t, r.ID, rr[0].RepositoryID)
	require.Equal(t, m.ID, rr[0].ManifestID)
	require.Equal(t, 0, rr[0].ReviewCount)
	// ignore the few milliseconds between manifest creation and queueing for review in response to the tag deletion
	require.WithinDuration(t, rr[0].ReviewAfter, m.CreatedAt.Add(24*time.Hour), 100*time.Millisecond)
}

func TestGC_TrackDeletedTags_MultipleTags(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest twice
	ts := datastore.NewTagStore(suite.db)
	tags := []string{"1.0.0", "latest"}
	for _, tag := range tags {
		err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
			Name:         tag,
			NamespaceID:  r.NamespaceID,
			RepositoryID: r.ID,
			ManifestID:   m.ID,
		})
		require.NoError(t, err)
	}

	// confirm that the review queue remains empty
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := mrs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	// delete tags
	for _, tag := range tags {
		ok, err := rs.DeleteTagByName(suite.ctx, r, tag)
		require.NoError(t, err)
		require.True(t, ok)
	}

	// check that a single corresponding task was created for the manifest and scheduled for 1 day ahead
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))
	require.Equal(t, r.ID, rr[0].RepositoryID)
	require.Equal(t, m.ID, rr[0].ManifestID)
	require.Equal(t, 0, rr[0].ReviewCount)
	// ignore the few milliseconds between manifest creation and queueing for review in response to the tag deletion
	require.WithinDuration(t, rr[0].ReviewAfter, m.CreatedAt.Add(24*time.Hour), 200*time.Millisecond)
}

func TestGC_TrackDeletedTags_ManifestDeleteCascade(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err := testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest
	ts := datastore.NewTagStore(suite.db)
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
	})
	require.NoError(t, err)

	// delete manifest (cascades to tags)
	ok, err := rs.DeleteManifest(suite.ctx, r, m.Digest)
	require.NoError(t, err)
	require.True(t, ok)

	// check that no task was created, as the corresponding manifest no longer exists
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Empty(t, rr)
}

func TestGC_TrackDeletedTags_PostponeReviewOnConflict(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err := rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest
	ts := datastore.NewTagStore(suite.db)
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
	})
	require.NoError(t, err)

	// grab existing review record (created by the gc_track_manifest_uploads trigger)
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	rr, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr))

	// delete tag
	ok, err := rs.DeleteTagByName(suite.ctx, r, "latest")
	require.NoError(t, err)
	require.True(t, ok)

	// check that we still have only one review record but its due date was postponed to now (delete time) + 1 day
	rr2, err := mrs.FindAll(suite.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, len(rr2))
	require.Equal(t, rr[0].RepositoryID, rr2[0].RepositoryID)
	require.Equal(t, rr[0].ManifestID, rr2[0].ManifestID)
	require.Equal(t, 0, rr2[0].ReviewCount)
	// review_after is only a few milliseconds ahead of the original time
	require.True(t, rr2[0].ReviewAfter.After(rr[0].ReviewAfter))
	require.WithinDuration(t, rr[0].ReviewAfter, rr2[0].ReviewAfter, 100*time.Millisecond)
}

func TestGC_TrackDeletedTags_DoesNothingIfTriggerDisabled(t *testing.T) {
	require.NoError(t, testutil.TruncateAllTables(suite.db))

	enable, err := testutil.GCTrackDeletedTagsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()
	// disable other triggers that also insert on gc_manifest_review_queue so that they don't interfere with this test
	enable, err = testutil.GCTrackManifestUploadsTrigger.Disable(suite.db)
	require.NoError(t, err)
	defer enable()

	// create repo
	r := randomRepository(t)
	rs := datastore.NewRepositoryStore(suite.db)
	r, err = rs.CreateByPath(suite.ctx, r.Path)
	require.NoError(t, err)

	// create manifest
	ms := datastore.NewManifestStore(suite.db)
	m := randomManifest(t, r, nil)
	err = ms.Create(suite.ctx, m)
	require.NoError(t, err)

	// tag manifest
	ts := datastore.NewTagStore(suite.db)
	err = ts.CreateOrUpdate(suite.ctx, &models.Tag{
		Name:         "latest",
		NamespaceID:  r.NamespaceID,
		RepositoryID: r.ID,
		ManifestID:   m.ID,
	})
	require.NoError(t, err)

	// delete tag
	ok, err := rs.DeleteTagByName(suite.ctx, r, "latest")
	require.NoError(t, err)
	require.True(t, ok)

	// check that no review records were created
	mrs := datastore.NewGCManifestTaskStore(suite.db)
	count, err := mrs.Count(suite.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
