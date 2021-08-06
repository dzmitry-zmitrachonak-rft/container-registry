// Package compat provides compatibility support for manifest lists containing
// blobs, such as buildx cache manifests using OCI Image Indexes. Since
// manifest lists should not include blob references, this package serves to
// separate the code for backwards compatibility from the code which assumes
// manifest lists that conform to spec.
package compat

import (
	"errors"
	"fmt"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// MediaTypeBuildxCacheConfig is the mediatype associated with buildx
// cache config blobs. This should be unique to buildx.
var MediaTypeBuildxCacheConfig = "application/vnd.buildkit.cacheconfig.v0"

// SplitReferences contains two lists of manifest list references broken down
// into either blobs or manifests. The result of appending these two lists
// together should include all of the descriptors returned by
// ManifestList.References with no duplicates, additions, or omissions.
type SplitReferences struct {
	Manifests []distribution.Descriptor
	Blobs     []distribution.Descriptor
}

// References returns the references of the DeserializedManifestList split into
// manifests and layers based on the mediatype of the standard list of
// descriptors. Only known manifest mediatypes will be sorted into the manifests
// array while everything else will be sorted into blobs. Helm chart manifests
// do not include a mediatype at the time of this commit, but they are unlikely
// to be included within a manifest list.
func References(ml *manifestlist.DeserializedManifestList) SplitReferences {
	var (
		manifests = make([]distribution.Descriptor, 0)
		blobs     = make([]distribution.Descriptor, 0)
	)

	for _, r := range ml.References() {
		switch r.MediaType {
		case schema2.MediaTypeManifest,
			manifestlist.MediaTypeManifestList,
			v1.MediaTypeImageManifest,
			schema1.MediaTypeSignedManifest,
			schema1.MediaTypeManifest:

			manifests = append(manifests, r)
		default:
			blobs = append(blobs, r)
		}
	}

	return SplitReferences{Manifests: manifests, Blobs: blobs}
}

// LikelyBuildxCache returns true if the manifest list is likely a buildx cache
// manifest based on the unique buildx config mediatype.
func LikelyBuildxCache(ml *manifestlist.DeserializedManifestList) bool {
	blobs := References(ml).Blobs

	for _, desc := range blobs {
		if desc.MediaType == MediaTypeBuildxCacheConfig {
			return true
		}
	}

	return false
}

// ContainsBlobs returns true if the manifest list contains any blobs.
func ContainsBlobs(ml *manifestlist.DeserializedManifestList) bool {
	return len(References(ml).Blobs) > 0
}

// OCIManifestFromBuildkitIndex transforms a Buildkit cache index into an OCI manifest for compatibility purposes.
func OCIManifestFromBuildkitIndex(ml *manifestlist.DeserializedManifestList) (*ocischema.DeserializedManifest, error) {
	refs := References(ml)
	if len(refs.Manifests) > 0 {
		return nil, errors.New("buildkit index has unexpected manifest references")
	}

	// set "config" and "layer" references apart
	var cfg *distribution.Descriptor
	var layers []distribution.Descriptor
	for _, ref := range refs.Blobs {
		if ref.MediaType == MediaTypeBuildxCacheConfig {
			cfg = &ref
		} else {
			layers = append(layers, ref)
		}
	}

	// make sure they were found
	if cfg == nil {
		return nil, errors.New("buildkit index has no config reference")
	}
	if len(layers) == 0 {
		return nil, errors.New("buildkit index has no layer references")
	}

	m, err := ocischema.FromStruct(ocischema.Manifest{
		Versioned: ocischema.SchemaVersion,
		Config:    *cfg,
		Layers:    layers,
	})
	if err != nil {
		return nil, fmt.Errorf("building manifest from buildkit index: %w", err)
	}

	return m, nil
}
