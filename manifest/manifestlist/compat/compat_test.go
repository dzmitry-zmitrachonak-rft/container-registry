package compat

import (
	"testing"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestReferences(t *testing.T) {

	var tests = []struct {
		name              string
		descriptors       []manifestlist.ManifestDescriptor
		expectedManifests []distribution.Descriptor
		expectedBlobs     []distribution.Descriptor
	}{
		{
			name: "OCI Image Index",
			descriptors: []manifestlist.ManifestDescriptor{
				{
					Descriptor: distribution.Descriptor{
						MediaType: v1.MediaTypeImageManifest,
						Size:      2343,
						Digest:    digest.FromString("OCI Manifest 1"),
					},
				},
				{
					Descriptor: distribution.Descriptor{
						MediaType: v1.MediaTypeImageManifest,
						Size:      354,
						Digest:    digest.FromString("OCI Manifest 2"),
					},
				},
			},
			expectedManifests: []distribution.Descriptor{
				{
					MediaType: v1.MediaTypeImageManifest,
					Size:      2343,
					Digest:    digest.FromString("OCI Manifest 1"),
				},
				{
					MediaType: v1.MediaTypeImageManifest,
					Size:      354,
					Digest:    digest.FromString("OCI Manifest 2"),
				},
			},
			expectedBlobs: []distribution.Descriptor{},
		},
		{
			name: "Buildx Cache Manifest",
			descriptors: []manifestlist.ManifestDescriptor{
				{
					Descriptor: distribution.Descriptor{
						MediaType: v1.MediaTypeImageLayer,
						Size:      792343,
						Digest:    digest.FromString("OCI Layer 1"),
					},
				},
				{
					Descriptor: distribution.Descriptor{
						MediaType: v1.MediaTypeImageLayer,
						Size:      35324234,
						Digest:    digest.FromString("OCI Layer 2"),
					},
				},
				{
					Descriptor: distribution.Descriptor{
						MediaType: "application/vnd.buildkit.cacheconfig.v0",
						Size:      4233,
						Digest:    digest.FromString("Cache Config 1"),
					},
				},
			},
			expectedManifests: []distribution.Descriptor{},
			expectedBlobs: []distribution.Descriptor{
				{
					MediaType: v1.MediaTypeImageLayer,
					Size:      792343,
					Digest:    digest.FromString("OCI Layer 1"),
				},
				{
					MediaType: v1.MediaTypeImageLayer,
					Size:      35324234,
					Digest:    digest.FromString("OCI Layer 2"),
				},
				{
					MediaType: "application/vnd.buildkit.cacheconfig.v0",
					Size:      4233,
					Digest:    digest.FromString("Cache Config 1"),
				},
			},
		},
		{
			name: "Mixed Manifest List",
			descriptors: []manifestlist.ManifestDescriptor{
				{
					Descriptor: distribution.Descriptor{
						MediaType: schema2.MediaTypeManifest,
						Size:      723,
						Digest:    digest.FromString("Schema2 Manifest 1"),
					},
				},
				{
					Descriptor: distribution.Descriptor{
						MediaType: schema2.MediaTypeLayer,
						Size:      2340184,
						Digest:    digest.FromString("Schema 2 Layer 1"),
					},
				},
			},
			expectedManifests: []distribution.Descriptor{
				{
					MediaType: schema2.MediaTypeManifest,
					Size:      723,
					Digest:    digest.FromString("Schema2 Manifest 1"),
				},
			},
			expectedBlobs: []distribution.Descriptor{
				{
					MediaType: schema2.MediaTypeLayer,
					Size:      2340184,
					Digest:    digest.FromString("Schema 2 Layer 1"),
				},
			},
		},
	}

	for _, tt := range tests {
		ml, err := manifestlist.FromDescriptors(tt.descriptors)
		require.NoError(t, err)

		splitRef := References(ml)
		require.ElementsMatch(t, tt.expectedManifests, splitRef.Manifests)
		require.ElementsMatch(t, tt.expectedBlobs, splitRef.Blobs)

		allRef := append(splitRef.Manifests, splitRef.Blobs...)

		require.ElementsMatch(t, ml.References(), allRef)
	}
}
