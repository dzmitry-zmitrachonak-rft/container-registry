package datastore

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound is returned when a row is not found on the metadata database.
	ErrNotFound = errors.New("not found")
	// ErrManifestNotFound is returned when a manifest is not found on the metadata database.
	ErrManifestNotFound = fmt.Errorf("manifest %w", ErrNotFound)
	// ErrRefManifestNotFound is returned when a manifest referenced by a list/index is not found on the metadata database.
	ErrRefManifestNotFound = fmt.Errorf("referenced %w", ErrManifestNotFound)
	// ErrManifestReferencedInList is returned when attempting to delete a manifest referenced in at least one list.
	ErrManifestReferencedInList = errors.New("manifest referenced by manifest list")
)

// ErrUnknownMediaType is returned when attempting to save a manifest containing references with unknown media types.
type ErrUnknownMediaType struct {
	// MediaType is the offending media type
	MediaType string
}

// Error implements error.
func (err ErrUnknownMediaType) Error() string {
	return fmt.Sprintf("unknown media type: %s", err.MediaType)
}
