// Package compat provides compatibility support for manifest lists containing
// blobs, such as buildx cache manifests using OCI Image Indexes. Since
// manifest lists should not include blob references, this package serves to
// seperate the code for backwards compatibility from the code which assumes
// manifest lists that conform to spec.
package compat

import (
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

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
