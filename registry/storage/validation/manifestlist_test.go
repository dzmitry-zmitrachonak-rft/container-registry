package validation_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/schema2"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestVerifyManifest_ManifestList(t *testing.T) {
	ctx := context.Background()

	registry := createRegistry(t)
	repo := makeRepository(t, registry, "test")

	descriptors := []manifestlist.ManifestDescriptor{
		makeManifestDescriptor(t, repo),
	}

	dml, err := manifestlist.FromDescriptors(descriptors)
	require.NoError(t, err)

	v := manifestlistValidator(t, repo, false, 0)

	err = v.Validate(ctx, dml)
	require.NoError(t, err)
}

func TestVerifyManifest_ManifestList_MissingManifest(t *testing.T) {
	ctx := context.Background()

	registry := createRegistry(t)
	repo := makeRepository(t, registry, "test")

	descriptors := []manifestlist.ManifestDescriptor{
		makeManifestDescriptor(t, repo),
		{Descriptor: distribution.Descriptor{Digest: digest.FromString("fake-digest"), MediaType: schema2.MediaTypeManifest}},
	}

	dml, err := manifestlist.FromDescriptors(descriptors)
	require.NoError(t, err)

	v := manifestlistValidator(t, repo, false, 0)

	err = v.Validate(ctx, dml)
	require.EqualError(t, err, fmt.Sprintf("errors verifying manifest: unknown blob %s on manifest", digest.FromString("fake-digest")))

	// Ensure that this error is not reported if SkipDependencyVerification is true
	v = manifestlistValidator(t, repo, true, 0)

	err = v.Validate(ctx, dml)
	require.NoError(t, err)
}

func TestVerifyManifest_ManifestList_InvalidSchemaVersion(t *testing.T) {
	ctx := context.Background()

	registry := createRegistry(t)
	repo := makeRepository(t, registry, "test")

	descriptors := []manifestlist.ManifestDescriptor{}

	dml, err := manifestlist.FromDescriptors(descriptors)
	require.NoError(t, err)

	dml.ManifestList.Versioned.SchemaVersion = 9001

	v := manifestlistValidator(t, repo, false, 0)

	err = v.Validate(ctx, dml)
	require.EqualError(t, err, fmt.Sprintf("unrecognized manifest list schema version %d", dml.ManifestList.Versioned.SchemaVersion))
}

// Docker buildkit uses OCI Image Indexes to store lists of layer blobs.
// Ideally, we would not permit this behavior, but due to
// https://gitlab.com/gitlab-org/container-registry/-/commit/06a098c632aee74619a06f88c23a06140f442a6f,
// not being strictly backwards looking, historically it was possible to
// retrieve a blob digest using manifest services during the validation step of
// manifest puts, preventing the validation logic from rejecting these
// manifests. Since buildkit is a fairly popular official docker tool, we
// should allow only these manifest lists to contain layer blobs,
// and reject all others.
//
// https://github.com/distribution/distribution/pull/864
func TestVerifyManifest_ManifestList_BuildkitCacheManifest(t *testing.T) {
	ctx := context.Background()

	registry := createRegistry(t)
	repo := makeRepository(t, registry, "test")

	descriptors := []manifestlist.ManifestDescriptor{
		makeImageCacheLayerDescriptor(t, repo),
		makeImageCacheLayerDescriptor(t, repo),
		makeImageCacheLayerDescriptor(t, repo),
		makeImageCacheConfigDescriptor(t, repo),
	}

	dml, err := manifestlist.FromDescriptors(descriptors)
	require.NoError(t, err)

	v := manifestlistValidator(t, repo, false, 0)

	err = v.Validate(ctx, dml)
	require.NoError(t, err)
}

func TestVerifyManifest_ManifestList_ManifestListWithBlobReferences(t *testing.T) {
	ctx := context.Background()

	registry := createRegistry(t)
	repo := makeRepository(t, registry, "test")

	descriptors := []manifestlist.ManifestDescriptor{
		makeImageCacheLayerDescriptor(t, repo),
		makeImageCacheLayerDescriptor(t, repo),
		makeImageCacheLayerDescriptor(t, repo),
		makeImageCacheLayerDescriptor(t, repo),
	}

	dml, err := manifestlist.FromDescriptors(descriptors)
	require.NoError(t, err)

	v := manifestlistValidator(t, repo, false, 0)

	err = v.Validate(ctx, dml)
	vErr := &distribution.ErrManifestVerification{}
	require.True(t, errors.As(err, vErr))

	// Ensure each later digest is included in the error with the proper error message.
	for _, l := range descriptors {
		require.Contains(t, vErr.Error(), fmt.Sprintf("unknown blob %s", l.Digest))
	}
}

func TestVerifyManifest_ManifestList_ReferenceLimits(t *testing.T) {
	ctx := context.Background()

	registry := createRegistry(t)
	repo := makeRepository(t, registry, "test")

	var tests = []struct {
		name                       string
		manifests                  int
		refLimit                   int
		wantErr                    bool
		skipDependencyVerification bool
	}{
		{
			name:                       "no reference limit",
			manifests:                  10,
			refLimit:                   0,
			wantErr:                    false,
			skipDependencyVerification: false,
		},
		{
			name:                       "reference limit greater than number of references",
			manifests:                  10,
			refLimit:                   150,
			wantErr:                    false,
			skipDependencyVerification: false,
		},
		{
			name:                       "reference limit equal to number of references",
			manifests:                  10,
			refLimit:                   10,
			wantErr:                    false,
			skipDependencyVerification: false,
		},
		{
			name:                       "reference limit less than number of references",
			manifests:                  400,
			refLimit:                   179,
			wantErr:                    true,
			skipDependencyVerification: false,
		},
		{
			name:                       "reference limit less than number of references skip verification",
			manifests:                  4,
			refLimit:                   2,
			wantErr:                    true,
			skipDependencyVerification: true,
		},
		{
			name:                       "negative reference limit",
			manifests:                  8,
			refLimit:                   -17,
			wantErr:                    false,
			skipDependencyVerification: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			descriptors := []manifestlist.ManifestDescriptor{}

			// Create a random manifest for each of the specified manifests.
			for i := 0; i < tt.manifests; i++ {
				descriptors = append(descriptors, makeManifestDescriptor(t, repo))
			}

			dml, err := manifestlist.FromDescriptors(descriptors)
			require.NoError(t, err)

			v := manifestlistValidator(t, repo, tt.skipDependencyVerification, tt.refLimit)

			err = v.Validate(ctx, dml)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
