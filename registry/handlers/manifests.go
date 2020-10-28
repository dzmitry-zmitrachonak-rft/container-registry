package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/docker/distribution/registry/auth"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/datastore/models"
	"github.com/docker/libtrust"
	"github.com/gorilla/handlers"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// These constants determine which architecture and OS to choose from a
// manifest list when downconverting it to a schema1 manifest.
const (
	defaultArch         = "amd64"
	defaultOS           = "linux"
	maxManifestBodySize = 4 << 20
	imageClass          = "image"
)

type storageType int

const (
	manifestSchema1        storageType = iota // 0
	manifestSchema2                           // 1
	manifestlistSchema                        // 2
	ociImageManifestSchema                    // 3
	ociImageIndexSchema                       // 4
	numStorageTypes                           // 5
)

// manifestDispatcher takes the request context and builds the
// appropriate handler for handling manifest requests.
func manifestDispatcher(ctx *Context, r *http.Request) http.Handler {
	manifestHandler := &manifestHandler{
		Context: ctx,
	}
	reference := getReference(ctx)
	dgst, err := digest.Parse(reference)
	if err != nil {
		// We just have a tag
		manifestHandler.Tag = reference
	} else {
		manifestHandler.Digest = dgst
	}

	mhandler := handlers.MethodHandler{
		"GET":  http.HandlerFunc(manifestHandler.GetManifest),
		"HEAD": http.HandlerFunc(manifestHandler.GetManifest),
	}

	if !ctx.readOnly {
		mhandler["PUT"] = http.HandlerFunc(manifestHandler.PutManifest)
		mhandler["DELETE"] = http.HandlerFunc(manifestHandler.DeleteManifest)
	}

	return mhandler
}

// manifestHandler handles http operations on image manifests.
type manifestHandler struct {
	*Context

	// One of tag or digest gets set, depending on what is present in context.
	Tag    string
	Digest digest.Digest
}

// GetManifest fetches the image manifest from the storage backend, if it exists.
func (imh *manifestHandler) GetManifest(w http.ResponseWriter, r *http.Request) {
	dcontext.GetLogger(imh).Debug("GetImageManifest")
	manifestService, err := imh.Repository.Manifests(imh)
	if err != nil {
		imh.Errors = append(imh.Errors, err)
		return
	}
	var supports [numStorageTypes]bool

	// this parsing of Accept headers is not quite as full-featured as godoc.org's parser, but we don't care about "q=" values
	// https://github.com/golang/gddo/blob/e91d4165076d7474d20abda83f92d15c7ebc3e81/httputil/header/header.go#L165-L202
	for _, acceptHeader := range r.Header["Accept"] {
		// r.Header[...] is a slice in case the request contains the same header more than once
		// if the header isn't set, we'll get the zero value, which "range" will handle gracefully

		// we need to split each header value on "," to get the full list of "Accept" values (per RFC 2616)
		// https://www.w3.org/Protocols/rfc2616/rfc2616-sec14.html#sec14.1
		for _, mediaType := range strings.Split(acceptHeader, ",") {
			if mediaType, _, err = mime.ParseMediaType(mediaType); err != nil {
				continue
			}

			if mediaType == schema2.MediaTypeManifest {
				supports[manifestSchema2] = true
			}
			if mediaType == manifestlist.MediaTypeManifestList {
				supports[manifestlistSchema] = true
			}
			if mediaType == v1.MediaTypeImageManifest {
				supports[ociImageManifestSchema] = true
			}
			if mediaType == v1.MediaTypeImageIndex {
				supports[ociImageIndexSchema] = true
			}
		}
	}

	var manifest distribution.Manifest

	if imh.Tag != "" {
		var dgst digest.Digest
		var dbErr error

		if imh.Config.Database.Enabled {
			manifest, dgst, dbErr = dbGetManifestByTag(imh, imh.App.db, imh.Tag, imh.App.trustKey, imh.Repository.Named().Name())
			if dbErr != nil {
				if imh.App.Config.Database.Experimental.Fallback {
					dcontext.GetLogger(imh).WithError(dbErr).Warn("unable to fetch manifest by tag from database, falling back to filesystem")
				} else {
					// Use the common error handling code below.
					err = dbErr
				}
			}
		}

		if !imh.Config.Database.Enabled || dbErr != nil {
			var desc distribution.Descriptor

			tags := imh.Repository.Tags(imh)
			desc, err = tags.Get(imh, imh.Tag)
			dgst = desc.Digest
		}

		if err != nil {
			if errors.As(err, &distribution.ErrTagUnknown{}) ||
				errors.Is(err, digest.ErrDigestInvalidFormat) ||
				errors.As(err, &distribution.ErrManifestUnknown{}) {
				// not found or with broken current/link (invalid digest)
				imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnknown.WithDetail(err))
			} else {
				imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			}
			return
		}
		imh.Digest = dgst
	}

	if etagMatch(r, imh.Digest.String()) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// The manifest will be nil if we retrieved the tag from the filesystem or
	// the manifest is being referenced by digest.
	if manifest == nil {
		manifest, err = dbGetManifestFilesystemFallback(imh, imh.App.db, manifestService, imh.Digest, imh.App.trustKey, imh.Tag, imh.Repository.Named().Name(), imh.Config.Database.Enabled, imh.App.Config.Database.Experimental.Fallback)
		if err != nil {
			if _, ok := err.(distribution.ErrManifestUnknownRevision); ok {
				imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnknown.WithDetail(err))
			} else {
				imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			}
			return
		}
	}

	// determine the type of the returned manifest
	manifestType := manifestSchema1
	schema2Manifest, isSchema2 := manifest.(*schema2.DeserializedManifest)
	manifestList, isManifestList := manifest.(*manifestlist.DeserializedManifestList)
	if isSchema2 {
		manifestType = manifestSchema2
	} else if _, isOCImanifest := manifest.(*ocischema.DeserializedManifest); isOCImanifest {
		manifestType = ociImageManifestSchema
	} else if isManifestList {
		if manifestList.MediaType == manifestlist.MediaTypeManifestList {
			manifestType = manifestlistSchema
		} else if manifestList.MediaType == v1.MediaTypeImageIndex {
			manifestType = ociImageIndexSchema
		}
	}

	if manifestType == ociImageManifestSchema && !supports[ociImageManifestSchema] {
		imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnknown.WithMessage("OCI manifest found, but accept header does not support OCI manifests"))
		return
	}
	if manifestType == ociImageIndexSchema && !supports[ociImageIndexSchema] {
		imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnknown.WithMessage("OCI index found, but accept header does not support OCI indexes"))
		return
	}
	// Only rewrite schema2 manifests when they are being fetched by tag.
	// If they are being fetched by digest, we can't return something not
	// matching the digest.
	if imh.Tag != "" && manifestType == manifestSchema2 && !supports[manifestSchema2] {
		// Rewrite manifest in schema1 format
		log := dcontext.GetLogger(imh)
		log.Infof("rewriting manifest %s in schema1 format to support old client", imh.Digest.String())
		log.Warn("DEPRECATION WARNING: Docker Schema v1 compatibility is deprecated and will be removed by January " +
			"22nd, 2021. Please update Docker Engine to 17.12 or later and rebuild and push any v1 images you might " +
			"still have. See https://gitlab.com/gitlab-org/container-registry/-/issues/213 for more details.")

		manifest, err = imh.convertSchema2Manifest(schema2Manifest)
		if err != nil {
			return
		}
	} else if imh.Tag != "" && manifestType == manifestlistSchema && !supports[manifestlistSchema] {
		log := dcontext.GetLoggerWithFields(imh, map[interface{}]interface{}{
			"manifest_list_digest": imh.Digest.String(),
			"default_arch":         defaultArch,
			"default_os":           defaultOS})
		log.Info("client does not advertise support for manifest lists, selecting a manifest image for the default arch and os")

		// Find the image manifest corresponding to the default platform.
		var manifestDigest digest.Digest
		for _, manifestDescriptor := range manifestList.Manifests {
			if manifestDescriptor.Platform.Architecture == defaultArch && manifestDescriptor.Platform.OS == defaultOS {
				manifestDigest = manifestDescriptor.Digest
				break
			}
		}

		if manifestDigest == "" {
			imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnknown.WithDetail(
				fmt.Errorf("manifest list %s does not contain a manifest image for the platform %s/%s",
					imh.Digest, defaultOS, defaultArch)))
			return
		}

		manifest, err = dbGetManifestFilesystemFallback(imh, imh.App.db, manifestService, manifestDigest, imh.App.trustKey, "", imh.Repository.Named().Name(), imh.Config.Database.Enabled, imh.Config.Database.Experimental.Fallback)
		if err != nil {
			if _, ok := err.(distribution.ErrManifestUnknownRevision); ok {
				imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnknown.WithDetail(err))
			} else {
				imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			}
			return
		}

		// If necessary, convert the image manifest into schema1
		if schema2Manifest, isSchema2 := manifest.(*schema2.DeserializedManifest); isSchema2 && !supports[manifestSchema2] {
			log.Warn("client does not advertise support for schema2 manifests, rewriting manifest in schema1 format")
			log.Warn("DEPRECATION WARNING: Docker Schema v1 compatibility is deprecated and will be removed by January " +
				"22nd, 2021. Please update Docker Engine to 17.12 or later and rebuild and push any v1 images you might " +
				"still have. See https://gitlab.com/gitlab-org/container-registry/-/issues/213 for more details.")

			manifest, err = imh.convertSchema2Manifest(schema2Manifest)
			if err != nil {
				return
			}
		} else {
			imh.Digest = manifestDigest
		}
	}

	ct, p, err := manifest.Payload()
	if err != nil {
		return
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", fmt.Sprint(len(p)))
	w.Header().Set("Docker-Content-Digest", imh.Digest.String())
	w.Header().Set("Etag", fmt.Sprintf(`"%s"`, imh.Digest))
	w.Write(p)
}

// dbGetManifestFilesystemFallback returns a distribution manifest by digest
// for the given repository. Reads from the database are preferred, but the
// manifest will be retrieved from the filesytem if either the database is
// disabled or there was an error in retrieving the manifest from the database.
func dbGetManifestFilesystemFallback(
	ctx context.Context,
	db datastore.Queryer,
	fsManifests distribution.ManifestService,
	dgst digest.Digest,
	schema1SigningKey libtrust.PrivateKey,
	tag, path string,
	dbEnabled, fallback bool) (distribution.Manifest, error) {
	var manifest distribution.Manifest
	var err error

	if dbEnabled {
		manifest, err = dbGetManifest(ctx, db, dgst, schema1SigningKey, path)
		if err != nil {
			if !fallback {
				return nil, err
			}

			dcontext.GetLogger(ctx).WithError(err).Warn("unable to fetch manifest by digest from database, falling back to filesystem")
		}
	}

	if !dbEnabled || err != nil {
		var options []distribution.ManifestServiceOption
		if tag != "" {
			options = append(options, distribution.WithTag(tag))
		}

		manifest, err = fsManifests.Get(ctx, dgst, options...)
	}

	return manifest, err
}

func dbGetManifest(ctx context.Context, db datastore.Queryer, dgst digest.Digest, schema1SigningKey libtrust.PrivateKey, path string) (distribution.Manifest, error) {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": path, "digest": dgst})
	log.Debug("getting manifest by digest from database")

	repositoryStore := datastore.NewRepositoryStore(db)
	r, err := repositoryStore.FindByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	if r == nil {
		log.Warn("repository not found in database")
		return nil, distribution.ErrManifestUnknownRevision{
			Name:     path,
			Revision: dgst,
		}
	}

	// Find manifest by its digest
	dbManifest, err := repositoryStore.FindManifestByDigest(ctx, r, dgst)
	if err != nil {
		return nil, err
	}
	if dbManifest == nil {
		return nil, distribution.ErrManifestUnknownRevision{
			Name:     path,
			Revision: dgst,
		}
	}

	return dbPayloadToManifest(dbManifest.Payload, dbManifest.MediaType, dbManifest.SchemaVersion, schema1SigningKey)
}

func dbGetManifestByTag(ctx context.Context, db datastore.Queryer, tagName string, schema1SigningKey libtrust.PrivateKey, path string) (distribution.Manifest, digest.Digest, error) {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": path, "tag": tagName})
	log.Debug("getting manifest by tag from database")

	repositoryStore := datastore.NewRepositoryStore(db)
	r, err := repositoryStore.FindByPath(ctx, path)
	if err != nil {
		return nil, "", err
	}
	if r == nil {
		log.Warn("repository not found in database")
		return nil, "", distribution.ErrTagUnknown{Tag: tagName}
	}

	dbTag, err := repositoryStore.FindTagByName(ctx, r, tagName)
	if err != nil {
		return nil, "", err
	}
	if dbTag == nil {
		log.Warn("tag not found in database")
		return nil, "", distribution.ErrTagUnknown{Tag: tagName}
	}

	// Find manifest by its digest
	mStore := datastore.NewManifestStore(db)
	dbManifest, err := mStore.FindByID(ctx, dbTag.ManifestID)
	if err != nil {
		return nil, "", err
	}
	if dbManifest == nil {
		return nil, "", distribution.ErrManifestUnknown{Name: r.Name, Tag: dbTag.Name}
	}

	manifest, err := dbPayloadToManifest(dbManifest.Payload, dbManifest.MediaType, dbManifest.SchemaVersion, schema1SigningKey)
	if err != nil {
		return nil, "", err
	}

	return manifest, dbManifest.Digest, nil
}

func dbPayloadToManifest(payload []byte, mediaType string, schemaVersion int, schema1SigningKey libtrust.PrivateKey) (distribution.Manifest, error) {
	// TODO: Each case here is taken directly from the respective
	// registry/storage/*manifesthandler Unmarshal method. These are all relatively
	// simple with the exception of schema1. We cannot invoke them directly as
	// they are unexported. We should determine a single place for this logic
	// during refactoring https://gitlab.com/gitlab-org/container-registry/-/issues/135
	switch schemaVersion {
	case 1:
		var (
			signatures [][]byte
			err        error
		)

		jsig, err := libtrust.NewJSONSignature(payload, signatures...)
		if err != nil {
			return nil, err
		}

		if schema1SigningKey != nil {
			if err := jsig.Sign(schema1SigningKey); err != nil {
				return nil, err
			}
		}

		// Extract the pretty JWS
		raw, err := jsig.PrettySignature("signatures")
		if err != nil {
			return nil, err
		}

		var sm schema1.SignedManifest
		if err := json.Unmarshal(raw, &sm); err != nil {
			return nil, err
		}

		return &sm, nil
	case 2:
		// This can be an image manifest or a manifest list
		switch mediaType {
		case schema2.MediaTypeManifest:
			m := &schema2.DeserializedManifest{}
			if err := m.UnmarshalJSON(payload); err != nil {
				return nil, err
			}

			return m, nil
		case v1.MediaTypeImageManifest:
			m := &ocischema.DeserializedManifest{}
			if err := m.UnmarshalJSON(payload); err != nil {
				return nil, err
			}

			return m, nil
		case manifestlist.MediaTypeManifestList, v1.MediaTypeImageIndex:
			m := &manifestlist.DeserializedManifestList{}
			if err := m.UnmarshalJSON(payload); err != nil {
				return nil, err
			}

			return m, nil
		case "":
			// OCI image or image index - no media type in the content

			// First see if it looks like an image index
			resIndex := &manifestlist.DeserializedManifestList{}
			if err := resIndex.UnmarshalJSON(payload); err != nil {
				return nil, err
			}
			if resIndex.Manifests != nil {
				return resIndex, nil
			}

			// Otherwise, assume it must be an image manifest
			m := &ocischema.DeserializedManifest{}
			if err := m.UnmarshalJSON(payload); err != nil {
				return nil, err
			}

			return m, nil
		default:
			return nil, distribution.ErrManifestVerification{fmt.Errorf("unrecognized manifest content type %s", mediaType)}
		}
	}

	return nil, fmt.Errorf("unrecognized manifest schema version %d", schemaVersion)
}

func (imh *manifestHandler) convertSchema2Manifest(schema2Manifest *schema2.DeserializedManifest) (distribution.Manifest, error) {
	targetDescriptor := schema2Manifest.Target()
	blobs := imh.Repository.Blobs(imh)
	configJSON, err := blobs.Get(imh, targetDescriptor.Digest)
	if err != nil {
		if err == distribution.ErrBlobUnknown {
			imh.Errors = append(imh.Errors, v2.ErrorCodeManifestInvalid.WithDetail(err))
		} else {
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		}
		return nil, err
	}

	ref := imh.Repository.Named()

	if imh.Tag != "" {
		ref, err = reference.WithTag(ref, imh.Tag)
		if err != nil {
			imh.Errors = append(imh.Errors, v2.ErrorCodeTagInvalid.WithDetail(err))
			return nil, err
		}
	}

	builder := schema1.NewConfigManifestBuilder(imh.Repository.Blobs(imh), imh.Context.App.trustKey, ref, configJSON)
	for _, d := range schema2Manifest.Layers {
		if err := builder.AppendReference(d); err != nil {
			imh.Errors = append(imh.Errors, v2.ErrorCodeManifestInvalid.WithDetail(err))
			return nil, err
		}
	}
	manifest, err := builder.Build(imh)
	if err != nil {
		imh.Errors = append(imh.Errors, v2.ErrorCodeManifestInvalid.WithDetail(err))
		return nil, err
	}
	imh.Digest = digest.FromBytes(manifest.(*schema1.SignedManifest).Canonical)

	return manifest, nil
}

func etagMatch(r *http.Request, etag string) bool {
	for _, headerVal := range r.Header["If-None-Match"] {
		if headerVal == etag || headerVal == fmt.Sprintf(`"%s"`, etag) { // allow quoted or unquoted
			return true
		}
	}
	return false
}

// PutManifest validates and stores a manifest in the registry.
func (imh *manifestHandler) PutManifest(w http.ResponseWriter, r *http.Request) {
	dcontext.GetLogger(imh).Debug("PutImageManifest")
	manifests, err := imh.Repository.Manifests(imh)
	if err != nil {
		imh.Errors = append(imh.Errors, err)
		return
	}

	var jsonBuf bytes.Buffer
	if err := copyFullPayload(imh, w, r, &jsonBuf, maxManifestBodySize, "image manifest PUT"); err != nil {
		// copyFullPayload reports the error if necessary
		imh.Errors = append(imh.Errors, v2.ErrorCodeManifestInvalid.WithDetail(err.Error()))
		return
	}

	mediaType := r.Header.Get("Content-Type")
	manifest, desc, err := distribution.UnmarshalManifest(mediaType, jsonBuf.Bytes())
	if err != nil {
		imh.Errors = append(imh.Errors, v2.ErrorCodeManifestInvalid.WithDetail(err))
		return
	}

	if imh.Digest != "" {
		if desc.Digest != imh.Digest {
			dcontext.GetLogger(imh).Errorf("payload digest does match: %q != %q", desc.Digest, imh.Digest)
			imh.Errors = append(imh.Errors, v2.ErrorCodeDigestInvalid)
			return
		}
	} else if imh.Tag != "" {
		imh.Digest = desc.Digest
	} else {
		imh.Errors = append(imh.Errors, v2.ErrorCodeTagInvalid.WithDetail("no tag or digest specified"))
		return
	}

	isAnOCIManifest := mediaType == v1.MediaTypeImageManifest || mediaType == v1.MediaTypeImageIndex

	if isAnOCIManifest {
		dcontext.GetLogger(imh).Debug("Putting an OCI Manifest!")
	} else {
		dcontext.GetLogger(imh).Debug("Putting a Docker Manifest!")
	}

	var options []distribution.ManifestServiceOption
	if imh.Tag != "" {
		options = append(options, distribution.WithTag(imh.Tag))
	}

	if err := imh.applyResourcePolicy(manifest); err != nil {
		imh.Errors = append(imh.Errors, err)
		return
	}

	_, err = manifests.Put(imh, manifest, options...)
	if err != nil {
		// TODO(stevvooe): These error handling switches really need to be
		// handled by an app global mapper.
		if err == distribution.ErrUnsupported {
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnsupported)
			return
		}
		if err == distribution.ErrAccessDenied {
			imh.Errors = append(imh.Errors, errcode.ErrorCodeDenied)
			return
		}
		switch err := err.(type) {
		case distribution.ErrManifestVerification:
			for _, verificationError := range err {
				switch verificationError := verificationError.(type) {
				case distribution.ErrManifestBlobUnknown:
					imh.Errors = append(imh.Errors, v2.ErrorCodeManifestBlobUnknown.WithDetail(verificationError.Digest))
				case distribution.ErrManifestNameInvalid:
					imh.Errors = append(imh.Errors, v2.ErrorCodeNameInvalid.WithDetail(err))
				case distribution.ErrManifestUnverified:
					imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnverified)
				default:
					if verificationError == digest.ErrDigestInvalidFormat {
						imh.Errors = append(imh.Errors, v2.ErrorCodeDigestInvalid)
					} else {
						imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown, verificationError)
					}
				}
			}
		case errcode.Error:
			imh.Errors = append(imh.Errors, err)
		default:
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		}
		return
	}

	if imh.Config.Database.Enabled {
		manifestService, err := imh.Repository.Manifests(imh)
		if err != nil {
			imh.Errors = append(imh.Errors, err)
			return
		}

		// We're using the database and mirroring writes to the filesystem. We'll run
		// a transaction so we can revert any changes to the database in case that
		// any part of this multi-phase database operation fails.
		tx, err := imh.App.db.BeginTx(imh, nil)
		if err != nil {
			imh.Errors = append(imh.Errors,
				errcode.ErrorCodeUnknown.WithDetail(fmt.Errorf("failed to create database transaction: %v", err)))
			return
		}
		defer tx.Rollback()

		if err := dbPutManifest(
			imh,
			imh.App.db,
			imh.Repository.Blobs(imh),
			manifestService,
			imh.Digest,
			manifest,
			imh.App.trustKey,
			jsonBuf.Bytes(),
			imh.Repository.Named().Name(),
			imh.App.Config.Database.Experimental.Fallback); err != nil {
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			return
		}

		if err := tx.Commit(); err != nil {
			imh.Errors = append(imh.Errors,
				errcode.ErrorCodeUnknown.WithDetail(fmt.Errorf("failed to commit manifest to database: %v", err)))
			return
		}
	}

	// Tag this manifest
	if imh.Tag != "" {
		tags := imh.Repository.Tags(imh)
		err = tags.Tag(imh, imh.Tag, desc)
		if err != nil {
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			return
		}

		// Associate tag with manifest in database.
		if imh.Config.Database.Enabled {
			tx, err := imh.App.db.BeginTx(imh, nil)
			if err != nil {
				e := fmt.Errorf("failed to create database transaction: %v", err)
				imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(e))
				return
			}
			defer tx.Rollback()

			if err := dbTagManifest(imh, tx, imh.Digest, imh.Tag, imh.Repository.Named().Name()); err != nil {
				e := fmt.Errorf("failed to create tag in database: %v", err)
				imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(e))
				return
			}
			if err := tx.Commit(); err != nil {
				e := fmt.Errorf("failed to commit tag to database: %v", err)
				imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(e))
				return
			}
		}
	}

	// Construct a canonical url for the uploaded manifest.
	ref, err := reference.WithDigest(imh.Repository.Named(), imh.Digest)
	if err != nil {
		imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}

	location, err := imh.urlBuilder.BuildManifestURL(ref)
	if err != nil {
		// NOTE(stevvooe): Given the behavior above, this absurdly unlikely to
		// happen. We'll log the error here but proceed as if it worked. Worst
		// case, we set an empty location header.
		dcontext.GetLogger(imh).Errorf("error building manifest url from digest: %v", err)
	}

	w.Header().Set("Location", location)
	w.Header().Set("Docker-Content-Digest", imh.Digest.String())
	w.WriteHeader(http.StatusCreated)

	dcontext.GetLogger(imh).Debug("Succeeded in putting manifest!")
}

func dbPutManifest(
	ctx context.Context,
	db datastore.Queryer,
	blobService distribution.BlobService,
	manifestService distribution.ManifestService,
	dgst digest.Digest,
	manifest distribution.Manifest,
	schema1SigningKey libtrust.PrivateKey,
	payload []byte,
	repoPath string,
	fallback bool) error {
	switch reqManifest := manifest.(type) {
	case *schema1.SignedManifest:
		if err := dbPutManifestSchema1(
			ctx, db, blobService, dgst, reqManifest, repoPath, fallback); err != nil {
			return fmt.Errorf("failed to write manifest to database: %v", err)
		}
	case *schema2.DeserializedManifest:
		if err := dbPutManifestSchema2(ctx, db, blobService, dgst, reqManifest, payload, repoPath, fallback); err != nil {
			return fmt.Errorf("failed to write manifest to database: %v", err)
		}
	case *ocischema.DeserializedManifest:
		if err := dbPutManifestOCI(ctx, db, blobService, dgst, reqManifest, payload, repoPath, fallback); err != nil {
			return fmt.Errorf("failed to write manifest to database: %v", err)
		}
	case *manifestlist.DeserializedManifestList:
		if err := dbPutManifestList(ctx, db, blobService, manifestService, dgst, reqManifest, schema1SigningKey, payload, repoPath, fallback); err != nil {
			return fmt.Errorf("failed to write manifest list to database: %v", err)
		}
	default:
		dcontext.GetLoggerWithField(ctx, "manifest_class", fmt.Sprintf("%T", reqManifest)).Warn("database does not support manifest class")
	}
	return nil
}

func dbTagManifest(ctx context.Context, db datastore.Queryer, dgst digest.Digest, tagName, path string) error {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": path, "manifest_digest": dgst, "tag": tagName})
	log.Debug("tagging manifest")

	repositoryStore := datastore.NewRepositoryStore(db)
	dbRepo, err := repositoryStore.FindByPath(ctx, path)
	if err != nil {
		return err
	}

	// TODO: If we return the manifest ID from the putDatabase methods, we can
	// avoid looking up the manifest by digest.
	manifestStore := datastore.NewManifestStore(db)
	dbManifest, err := manifestStore.FindByDigest(ctx, dgst)
	if err != nil {
		return err
	}
	if dbManifest == nil {
		return fmt.Errorf("manifest %s not found in database", dgst)
	}

	tagStore := datastore.NewTagStore(db)

	dbTag, err := repositoryStore.FindTagByName(ctx, dbRepo, tagName)
	if err != nil {
		return err
	}

	if dbTag != nil {
		log.Debug("tag already exists in database")

		// Tag exists and already points to the current manifest.
		if dbTag.ManifestID == dbManifest.ID {
			log.Debug("tag already associated with current manifest")
			return nil
		}

		// Tag exists, but refers to another manifest, update the manifest to which the tag refers.
		log.Debug("updating tag with manifest ID")
		dbTag.ManifestID = dbManifest.ID

		return tagStore.Update(ctx, dbTag)
	}

	// Tag does not exist, create it.
	log.Debug("creating new tag")
	return tagStore.Create(ctx, &models.Tag{
		Name:         tagName,
		RepositoryID: dbRepo.ID,
		ManifestID:   dbManifest.ID,
	})
}

func dbPutManifestOCI(
	ctx context.Context,
	db datastore.Queryer,
	blobService distribution.BlobService,
	dgst digest.Digest,
	manifest *ocischema.DeserializedManifest,
	payload []byte,
	repoPath string,
	fallback bool) error {
	return dbPutManifestOCIOrSchema2(ctx, db, blobService, dgst, manifest.Versioned, manifest.Layers, manifest.Config, payload, repoPath, fallback)
}

func dbPutManifestSchema2(
	ctx context.Context,
	db datastore.Queryer,
	blobService distribution.BlobService,
	dgst digest.Digest,
	manifest *schema2.DeserializedManifest,
	payload []byte,
	repoPath string,
	fallback bool) error {
	return dbPutManifestOCIOrSchema2(ctx, db, blobService, dgst, manifest.Versioned, manifest.Layers, manifest.Config, payload, repoPath, fallback)
}

func dbPutManifestOCIOrSchema2(
	ctx context.Context,
	db datastore.Queryer,
	blobService distribution.BlobService,
	dgst digest.Digest,
	versioned manifest.Versioned,
	layers []distribution.Descriptor,
	cfgDesc distribution.Descriptor,
	payload []byte,
	repoPath string,
	fallback bool) error {

	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": repoPath, "manifest_digest": dgst, "schema_version": versioned.SchemaVersion})
	log.Debug("putting manifest")

	// Find the config now to ensure that the config's blob is associated with the repository.
	dbCfgBlob, err := dbFindRepositoryBlob(ctx, db, blobService, cfgDesc, repoPath, fallback)
	if err != nil {
		return err
	}
	// TODO: update the config blob media_type here, it was set to "application/octet-stream" during the upload
	// 		 but now we know its concrete type (cfgDesc.MediaType).

	cfgPayload, err := blobService.Get(ctx, dbCfgBlob.Digest)
	if err != nil {
		return err
	}

	mStore := datastore.NewManifestStore(db)
	dbManifest, err := mStore.FindByDigest(ctx, dgst)
	if err != nil {
		return err
	}
	if dbManifest == nil {
		log.Debug("manifest not found in database")

		m := &models.Manifest{
			SchemaVersion: versioned.SchemaVersion,
			MediaType:     versioned.MediaType,
			Digest:        dgst,
			Payload:       payload,
			Configuration: &models.Configuration{
				MediaType: dbCfgBlob.MediaType,
				Digest:    dbCfgBlob.Digest,
				Payload:   cfgPayload,
			},
		}

		if err := mStore.Create(ctx, m); err != nil {
			return err
		}

		dbManifest = m

		// find and associate manifest layer blobs
		for _, reqLayer := range layers {
			dbBlob, err := dbFindRepositoryBlob(ctx, db, blobService, reqLayer, repoPath, fallback)
			if err != nil {
				return err
			}

			// TODO: update the layer blob media_type here, it was set to "application/octet-stream" during the upload
			// 		 but now we know its concrete type (reqLayer.MediaType).

			if err := mStore.AssociateLayerBlob(ctx, dbManifest, dbBlob); err != nil {
				return err
			}
		}
	}

	// Associate manifest and repository.
	repositoryStore := datastore.NewRepositoryStore(db)
	dbRepo, err := repositoryStore.CreateOrFindByPath(ctx, repoPath)
	if err != nil {
		return err
	}

	if err := repositoryStore.AssociateManifest(ctx, dbRepo, dbManifest); err != nil {
		return err
	}
	return nil
}

// dbFindRepositoryBlob finds a blob which is linked to the repository.
// Optionally, the search for the blob can fallback to the filesystem, if the
// blob is not found in the database. If found, the blob will be backfilled into
// the database and associated with the repository.
func dbFindRepositoryBlob(ctx context.Context, db datastore.Queryer, blobStatter distribution.BlobStatter, desc distribution.Descriptor, repoPath string, fallback bool) (*models.Blob, error) {
	rStore := datastore.NewRepositoryStore(db)

	r, err := rStore.FindByPath(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	if r == nil {
		if !fallback {
			return nil, errors.New("source repository not found in database")
		}

		r, err = rStore.CreateByPath(ctx, repoPath)
		if err != nil {
			return nil, err
		}
	}

	repoBlobs, err := rStore.Blobs(ctx, r)
	if err != nil {
		return nil, err
	}

	for _, blob := range repoBlobs {
		if blob.Digest == desc.Digest {
			// Blob is both in the database and linked to the repository, exit now.
			return blob, nil
		}
	}

	if !fallback {
		return nil, fmt.Errorf("blob not found in database")
	}

	dcontext.GetLogger(ctx).WithField("digest", desc.Digest).Warn("blob not found in database, falling back to filesystem")

	fsBlobDesc, err := blobStatter.Stat(ctx, desc.Digest)
	if err != nil {
		return nil, err
	}

	// create or find blob
	bs := datastore.NewBlobStore(db)
	b := &models.Blob{
		MediaType: fsBlobDesc.MediaType,
		Digest:    fsBlobDesc.Digest,
		Size:      fsBlobDesc.Size,
	}
	if err = bs.CreateOrFind(ctx, b); err != nil {
		return nil, err
	}

	// link blob to repository
	if err := rStore.LinkBlob(ctx, r, b); err != nil {
		return nil, err
	}

	return b, nil
}

// dbFindManifestListManifest finds a manifest which is linked to the manifest list.
// Optionally, the search for the manifest can fallback to the filesystem, if
// the manifest is not found in the database. If found, the manifest, and its
// blobs will be backfilled into the database and associated with the repository.
func dbFindManifestListManifest(
	ctx context.Context,
	db datastore.Queryer,
	blobService distribution.BlobService,
	manifestService distribution.ManifestService,
	dgst digest.Digest,
	schema1SigningKey libtrust.PrivateKey,
	repoPath string,
	fallback bool) (*models.Manifest, error) {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": repoPath, "manifest_digest": dgst, "fallback": fallback})
	log.Debug("finding manifest list manifest")

	var dbManifest *models.Manifest

	mStore := datastore.NewManifestStore(db)
	dbManifest, err := mStore.FindByDigest(ctx, dgst)
	if err != nil {
		return nil, err
	}
	if dbManifest == nil {
		if !fallback {
			return nil, fmt.Errorf("manifest %s not found", dgst)
		}

		log.Warn("manifest not found in database, falling back to filesystem")

		fsManifest, err := dbGetManifestFilesystemFallback(ctx, db, manifestService, dgst, schema1SigningKey, "", repoPath, true, fallback)
		if err != nil {
			return nil, err
		}

		_, payload, err := fsManifest.Payload()
		if err != nil {
			return nil, err
		}

		if err := dbPutManifest(ctx, db, blobService, manifestService, dgst, fsManifest, schema1SigningKey, payload, repoPath, fallback); err != nil {
			return nil, err
		}

		dbManifest, err = mStore.FindByDigest(ctx, dgst)
		if err != nil {
			return nil, err
		}
		if dbManifest == nil {
			return nil, fmt.Errorf("manifest %s not found", dgst)
		}
	}

	return dbManifest, nil
}

func dbPutManifestSchema1(
	ctx context.Context,
	db datastore.Queryer,
	blobStatter distribution.BlobStatter,
	dgst digest.Digest,
	manifest *schema1.SignedManifest,
	repoPath string,
	fallback bool) error {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": repoPath, "manifest_digest": dgst, "schema_version": manifest.Versioned.SchemaVersion})
	log.Debug("putting manifest")

	mStore := datastore.NewManifestStore(db)
	dbManifest, err := mStore.FindByDigest(ctx, dgst)
	if err != nil {
		return err
	}
	if dbManifest == nil {
		log.Debug("manifest not found in database")

		m := &models.Manifest{
			SchemaVersion: manifest.SchemaVersion,
			MediaType:     schema1.MediaTypeSignedManifest,
			Digest:        dgst,
			Payload:       manifest.Canonical,
		}

		if err := mStore.Create(ctx, m); err != nil {
			return err
		}

		dbManifest = m

		// find and associate manifest layer blobs
		for _, layer := range manifest.FSLayers {
			dbBlob, err := dbFindRepositoryBlob(ctx, db, blobStatter, distribution.Descriptor{Digest: layer.BlobSum}, repoPath, fallback)
			if err != nil {
				return err
			}

			// TODO: update the layer blob media_type here, it was set to "application/octet-stream" during the upload
			// 		 but now we know its concrete type (reqLayer.MediaType).

			if err := mStore.AssociateLayerBlob(ctx, dbManifest, dbBlob); err != nil {
				return err
			}
		}
	}

	// Associate manifest and repository.
	repositoryStore := datastore.NewRepositoryStore(db)
	dbRepo, err := repositoryStore.CreateOrFindByPath(ctx, repoPath)
	if err != nil {
		return err
	}

	if err := repositoryStore.AssociateManifest(ctx, dbRepo, dbManifest); err != nil {
		return err
	}
	return nil
}

func dbPutManifestList(
	ctx context.Context,
	db datastore.Queryer,
	blobService distribution.BlobService,
	manifestService distribution.ManifestService,
	dgst digest.Digest,
	manifestList *manifestlist.DeserializedManifestList,
	schema1SigningKey libtrust.PrivateKey,
	payload []byte,
	repoPath string,
	fallback bool) error {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": repoPath, "manifest_digest": dgst})
	log.Debug("putting manifest list")

	mStore := datastore.NewManifestStore(db)
	dbManifestList, err := mStore.FindByDigest(ctx, dgst)
	if err != nil {
		return err
	}

	if dbManifestList == nil {
		log.Debug("manifest list not found in database")

		// Media type can be either Docker (`application/vnd.docker.distribution.manifest.list.v2+json`) or OCI (empty).
		// We need to make it explicit if empty, otherwise we're not able to distinguish between media types.
		mediaType := manifestList.MediaType
		if mediaType == "" {
			mediaType = v1.MediaTypeImageIndex
		}

		dbManifestList = &models.Manifest{
			SchemaVersion: manifestList.SchemaVersion,
			MediaType:     mediaType,
			Digest:        dgst,
			Payload:       payload,
		}
		if err := mStore.Create(ctx, dbManifestList); err != nil {
			return err
		}

		// Associate manifests to the manifest list.
		for _, m := range manifestList.Manifests {
			dbManifest, err := dbFindManifestListManifest(ctx, db, blobService, manifestService, m.Digest, schema1SigningKey, repoPath, fallback)
			if err != nil {
				return err
			}

			if err := mStore.AssociateManifest(ctx, dbManifestList, dbManifest); err != nil {
				return err
			}
		}
	}

	// Associate manifest list and repository.
	repositoryStore := datastore.NewRepositoryStore(db)
	dbRepo, err := repositoryStore.CreateOrFindByPath(ctx, repoPath)
	if err != nil {
		return err
	}

	return repositoryStore.AssociateManifest(ctx, dbRepo, dbManifestList)
}

// applyResourcePolicy checks whether the resource class matches what has
// been authorized and allowed by the policy configuration.
func (imh *manifestHandler) applyResourcePolicy(manifest distribution.Manifest) error {
	allowedClasses := imh.App.Config.Policy.Repository.Classes
	if len(allowedClasses) == 0 {
		return nil
	}

	var class string
	switch m := manifest.(type) {
	case *schema1.SignedManifest:
		class = imageClass
	case *schema2.DeserializedManifest:
		switch m.Config.MediaType {
		case schema2.MediaTypeImageConfig:
			class = imageClass
		case schema2.MediaTypePluginConfig:
			class = "plugin"
		default:
			return errcode.ErrorCodeDenied.WithMessage("unknown manifest class for " + m.Config.MediaType)
		}
	case *ocischema.DeserializedManifest:
		switch m.Config.MediaType {
		case v1.MediaTypeImageConfig:
			class = imageClass
		default:
			return errcode.ErrorCodeDenied.WithMessage("unknown manifest class for " + m.Config.MediaType)
		}
	}

	if class == "" {
		return nil
	}

	// Check to see if class is allowed in registry
	var allowedClass bool
	for _, c := range allowedClasses {
		if class == c {
			allowedClass = true
			break
		}
	}
	if !allowedClass {
		return errcode.ErrorCodeDenied.WithMessage(fmt.Sprintf("registry does not allow %s manifest", class))
	}

	resources := auth.AuthorizedResources(imh)
	n := imh.Repository.Named().Name()

	var foundResource bool
	for _, r := range resources {
		if r.Name == n {
			if r.Class == "" {
				r.Class = imageClass
			}
			if r.Class == class {
				return nil
			}
			foundResource = true
		}
	}

	// resource was found but no matching class was found
	if foundResource {
		return errcode.ErrorCodeDenied.WithMessage(fmt.Sprintf("repository not authorized for %s manifest", class))
	}

	return nil
}

// dbDeleteManifest replicates the DeleteManifest action in the metadata database. This method doesn't actually delete
// a manifest from the database (that's a task for GC, if a manifest is unreferenced), it only deletes the record that
// associates the manifest with a digest d with the repository with path repoPath. Any tags that reference the manifest
// within the repository are also deleted.
func dbDeleteManifest(ctx context.Context, db datastore.Queryer, repoPath string, d digest.Digest, fallback bool) error {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": repoPath, "digest": d})
	log.Debug("deleting manifest from repository in database")

	rStore := datastore.NewRepositoryStore(db)
	r, err := rStore.FindByPath(ctx, repoPath)
	if err != nil {
		return err
	}
	if r == nil {
		if fallback {
			log.Warn("repository not found in database, no need to unlink from the manifest")
			return nil
		}

		return fmt.Errorf("repository not found in database: %w", err)
	}

	m, err := rStore.FindManifestByDigest(ctx, r, d)
	if err != nil {
		return err
	}
	if m == nil {
		if fallback {
			log.Warn("manifest not found in database, no need to unlink it from the repository")
			return nil
		}

		return fmt.Errorf("manifest not found in database: %w", err)
	}

	log.Debug("manifest found in database")
	if err := rStore.DissociateManifest(ctx, r, m); err != nil {
		return err
	}

	return rStore.UntagManifest(ctx, r, m)
}

// DeleteManifest removes the manifest with the given digest from the registry.
func (imh *manifestHandler) DeleteManifest(w http.ResponseWriter, r *http.Request) {
	dcontext.GetLogger(imh).Debug("DeleteImageManifest")

	manifests, err := imh.Repository.Manifests(imh)
	if err != nil {
		imh.Errors = append(imh.Errors, err)
		return
	}

	err = manifests.Delete(imh, imh.Digest)
	if err != nil {
		switch err {
		case digest.ErrDigestUnsupported:
		case digest.ErrDigestInvalidFormat:
			imh.Errors = append(imh.Errors, v2.ErrorCodeDigestInvalid)
			return
		case distribution.ErrBlobUnknown:
			imh.Errors = append(imh.Errors, v2.ErrorCodeManifestUnknown)
			return
		case distribution.ErrUnsupported:
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnsupported)
			return
		default:
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown)
			return
		}
	}

	tagService := imh.Repository.Tags(imh)
	referencedTags, err := tagService.Lookup(imh, distribution.Descriptor{Digest: imh.Digest})
	if err != nil {
		imh.Errors = append(imh.Errors, err)
		return
	}

	for _, tag := range referencedTags {
		if err = tagService.Untag(imh, tag); err != nil {
			imh.Errors = append(imh.Errors, err)
			return
		}
	}

	if imh.App.Config.Database.Enabled {
		tx, err := imh.db.BeginTx(r.Context(), nil)
		if err != nil {
			e := fmt.Errorf("failed to create database transaction: %v", err)
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(e))
			return
		}
		defer tx.Rollback()

		if err = dbDeleteManifest(imh, tx, imh.Repository.Named().String(), imh.Digest, imh.App.Config.Database.Experimental.Fallback); err != nil {
			e := fmt.Errorf("failed to delete manifest in database: %v", err)
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(e))
			return
		}

		if err = tx.Commit(); err != nil {
			e := fmt.Errorf("failed to commit database transaction: %v", err)
			imh.Errors = append(imh.Errors, errcode.ErrorCodeUnknown.WithDetail(e))
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
}
