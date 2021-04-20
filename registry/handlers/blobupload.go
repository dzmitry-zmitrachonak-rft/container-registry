package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/datastore/models"
	"github.com/docker/distribution/registry/storage"
	"github.com/gorilla/handlers"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
)

// blobUploadDispatcher constructs and returns the blob upload handler for the
// given request context.
func blobUploadDispatcher(ctx *Context, r *http.Request) http.Handler {
	buh := &blobUploadHandler{
		Context: ctx,
		UUID:    getUploadUUID(ctx),
	}

	handler := handlers.MethodHandler{
		"GET":  http.HandlerFunc(buh.GetUploadStatus),
		"HEAD": http.HandlerFunc(buh.GetUploadStatus),
	}

	if !ctx.readOnly {
		handler["POST"] = http.HandlerFunc(buh.StartBlobUpload)
		handler["PATCH"] = http.HandlerFunc(buh.PatchBlobData)
		handler["PUT"] = http.HandlerFunc(buh.PutBlobUploadComplete)
		handler["DELETE"] = http.HandlerFunc(buh.CancelBlobUpload)
	}

	return migrationWrapper(ctx, buh.validateUpload(handler))
}

func (buh *blobUploadHandler) validateUpload(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if buh.UUID != "" {
			if h := buh.ResumeBlobUpload(buh.Context, r); h != nil {
				h.ServeHTTP(w, r)
			} else {
				h := closeResources(handler, buh.Upload)
				h.ServeHTTP(w, r)
			}
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// blobUploadHandler handles the http blob upload process.
type blobUploadHandler struct {
	*Context

	// UUID identifies the upload instance for the current request. Using UUID
	// to key blob writers since this implementation uses UUIDs.
	UUID string

	Upload distribution.BlobWriter

	State blobUploadState
}

func dbMountBlob(ctx context.Context, db datastore.Queryer, fromRepoPath, toRepoPath string, d digest.Digest) error {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{
		"source":      fromRepoPath,
		"destination": toRepoPath,
		"digest":      d,
	})
	log.Debug("cross repository blob mounting")

	// find source blob from source repository
	b, err := dbFindRepositoryBlob(ctx, db, distribution.Descriptor{Digest: d}, fromRepoPath)
	if err != nil {
		return err
	}

	rStore := datastore.NewRepositoryStore(db)
	destRepo, err := rStore.CreateOrFindByPath(ctx, toRepoPath)
	if err != nil {
		return err
	}

	// link blob (does nothing if already linked)
	return rStore.LinkBlob(ctx, destRepo, b.Digest)
}

// StartBlobUpload begins the blob upload process and allocates a server-side
// blob writer session, optionally mounting the blob from a separate repository.
func (buh *blobUploadHandler) StartBlobUpload(w http.ResponseWriter, r *http.Request) {
	var options []distribution.BlobCreateOption

	fromRepo := r.FormValue("from")
	mountDigest := r.FormValue("mount")

	if mountDigest != "" && fromRepo != "" {
		opt, err := buh.createBlobMountOption(fromRepo, mountDigest)
		if opt != nil && err == nil {
			options = append(options, opt)
		}
	}

	blobs := buh.Repository.Blobs(buh)
	upload, err := blobs.Create(buh, options...)

	if err != nil {
		if ebm, ok := err.(distribution.ErrBlobMounted); ok {
			if buh.useDatabase {
				if err = dbMountBlob(buh.Context, buh.db, ebm.From.Name(), buh.Repository.Named().Name(), ebm.Descriptor.Digest); err != nil {
					e := fmt.Errorf("failed to mount blob in database: %w", err)
					buh.Errors = append(buh.Errors, errcode.FromUnknownError(e))
					return
				}
			}
			if err = buh.writeBlobCreatedHeaders(w, ebm.Descriptor); err != nil {
				buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			}
		} else if err == distribution.ErrUnsupported {
			buh.Errors = append(buh.Errors, errcode.ErrorCodeUnsupported)
		} else {
			buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		}
		return
	}

	buh.Upload = upload

	if err := buh.blobUploadResponse(w, r, true); err != nil {
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}

	w.Header().Set("Docker-Upload-UUID", buh.Upload.ID())
	w.WriteHeader(http.StatusAccepted)
}

// GetUploadStatus returns the status of a given upload, identified by id.
func (buh *blobUploadHandler) GetUploadStatus(w http.ResponseWriter, r *http.Request) {
	if buh.Upload == nil {
		buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadUnknown)
		return
	}

	// TODO(dmcgowan): Set last argument to false in blobUploadResponse when
	// resumable upload is supported. This will enable returning a non-zero
	// range for clients to begin uploading at an offset.
	if err := buh.blobUploadResponse(w, r, true); err != nil {
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}

	w.Header().Set("Docker-Upload-UUID", buh.UUID)
	w.WriteHeader(http.StatusNoContent)
}

// PatchBlobData writes data to an upload.
func (buh *blobUploadHandler) PatchBlobData(w http.ResponseWriter, r *http.Request) {
	if buh.Upload == nil {
		buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadUnknown)
		return
	}

	ct := r.Header.Get("Content-Type")
	if ct != "" && ct != "application/octet-stream" {
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(fmt.Errorf("bad Content-Type")))
		// TODO(dmcgowan): encode error
		return
	}

	// TODO(dmcgowan): support Content-Range header to seek and write range

	if err := copyFullPayload(buh, w, r, buh.Upload, -1, "blob PATCH"); err != nil {
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err.Error()))
		return
	}

	if err := buh.blobUploadResponse(w, r, false); err != nil {
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func dbPutBlobUploadComplete(ctx context.Context, db *datastore.DB, repoPath string, desc distribution.Descriptor) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning database transaction: %w", err)
	}
	defer tx.Rollback()

	// create or find blob
	bs := datastore.NewBlobStore(tx)
	b := &models.Blob{
		MediaType: desc.MediaType,
		Digest:    desc.Digest,
		Size:      desc.Size,
	}
	if err := bs.CreateOrFind(ctx, b); err != nil {
		return err
	}

	// create or find repository
	rStore := datastore.NewRepositoryStore(tx)
	r, err := rStore.CreateOrFindByPath(ctx, repoPath)
	if err != nil {
		return err
	}

	// link blob to repository
	if err := rStore.LinkBlob(ctx, r, b.Digest); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing database transaction: %w", err)
	}

	return nil
}

// PutBlobUploadComplete takes the final request of a blob upload. The
// request may include all the blob data or no blob data. Any data
// provided is received and verified. If successful, the blob is linked
// into the blob store and 201 Created is returned with the canonical
// url of the blob.
func (buh *blobUploadHandler) PutBlobUploadComplete(w http.ResponseWriter, r *http.Request) {
	if buh.Upload == nil {
		buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadUnknown)
		return
	}

	dgstStr := r.FormValue("digest") // TODO(stevvooe): Support multiple digest parameters!

	if dgstStr == "" {
		// no digest? return error, but allow retry.
		buh.Errors = append(buh.Errors, v2.ErrorCodeDigestInvalid.WithDetail("digest missing"))
		return
	}

	dgst, err := digest.Parse(dgstStr)
	if err != nil {
		// no digest? return error, but allow retry.
		buh.Errors = append(buh.Errors, v2.ErrorCodeDigestInvalid.WithDetail("digest parsing failed"))
		return
	}

	if err := copyFullPayload(buh, w, r, buh.Upload, -1, "blob PUT"); err != nil {
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err.Error()))
		return
	}

	desc, err := buh.Upload.Commit(buh, distribution.Descriptor{
		Digest: dgst,

		// TODO(stevvooe): This isn't wildly important yet, but we should
		// really set the mediatype. For now, we can let the backend take care
		// of this.
	})

	log := dcontext.GetLogger(buh)
	if err != nil {
		switch err := err.(type) {
		case distribution.ErrBlobInvalidDigest:
			buh.Errors = append(buh.Errors, v2.ErrorCodeDigestInvalid.WithDetail(err))
		case errcode.Error:
			buh.Errors = append(buh.Errors, err)
		default:
			switch err {
			case distribution.ErrAccessDenied:
				buh.Errors = append(buh.Errors, errcode.ErrorCodeDenied)
			case distribution.ErrUnsupported:
				buh.Errors = append(buh.Errors, errcode.ErrorCodeUnsupported)
			case distribution.ErrBlobInvalidLength, distribution.ErrBlobDigestUnsupported:
				buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadInvalid.WithDetail(err))
			default:
				log.Errorf("unknown error completing upload: %v", err)
				buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			}

		}

		// Clean up the backend blob data if there was an error.
		if err := buh.Upload.Cancel(buh); err != nil {
			// If the cleanup fails, all we can do is observe and report.
			log.Errorf("error canceling upload after error: %v", err)
		}

		return
	}

	if buh.useDatabase {
		if err := dbPutBlobUploadComplete(buh.Context, buh.db, buh.Repository.Named().Name(), desc); err != nil {
			e := fmt.Errorf("failed to create blob in database: %w", err)
			buh.Errors = append(buh.Errors, errcode.FromUnknownError(e))
			return
		}
	}

	if err := buh.writeBlobCreatedHeaders(w, desc); err != nil {
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}

	log.WithFields(logrus.Fields{
		"media_type": desc.MediaType,
		"size_bytes": desc.Size,
		"digest":     desc.Digest,
	}).Info("blob uploaded")
}

// CancelBlobUpload cancels an in-progress upload of a blob.
func (buh *blobUploadHandler) CancelBlobUpload(w http.ResponseWriter, r *http.Request) {
	if buh.Upload == nil {
		buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadUnknown)
		return
	}

	w.Header().Set("Docker-Upload-UUID", buh.UUID)
	if err := buh.Upload.Cancel(buh); err != nil {
		dcontext.GetLogger(buh).Errorf("error encountered canceling upload: %v", err)
		buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
	}

	w.WriteHeader(http.StatusNoContent)
}

func (buh *blobUploadHandler) ResumeBlobUpload(ctx *Context, r *http.Request) http.Handler {
	state, err := hmacKey(ctx.Config.HTTP.Secret).unpackUploadState(r.FormValue("_state"))
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dcontext.GetLogger(ctx).Infof("error resolving upload: %v", err)
			buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadInvalid.WithDetail(err))
		})
	}
	buh.State = state

	if state.Name != ctx.Repository.Named().Name() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dcontext.GetLogger(ctx).Infof("mismatched repository name in upload state: %q != %q", state.Name, buh.Repository.Named().Name())
			buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadInvalid.WithDetail(err))
		})
	}

	if state.UUID != buh.UUID {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dcontext.GetLogger(ctx).Infof("mismatched uuid in upload state: %q != %q", state.UUID, buh.UUID)
			buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadInvalid.WithDetail(err))
		})
	}

	blobs := ctx.Repository.Blobs(buh)
	upload, err := blobs.Resume(buh, buh.UUID)
	if err != nil {
		dcontext.GetLogger(ctx).Errorf("error resolving upload: %v", err)
		if err == distribution.ErrBlobUploadUnknown {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadUnknown.WithDetail(err))
			})
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buh.Errors = append(buh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		})
	}
	buh.Upload = upload

	if size := upload.Size(); size != buh.State.Offset {
		defer upload.Close()
		dcontext.GetLogger(ctx).Errorf("upload resumed at wrong offset: %d != %d", size, buh.State.Offset)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buh.Errors = append(buh.Errors, v2.ErrorCodeBlobUploadInvalid.WithDetail(err))
			upload.Cancel(buh)
		})
	}
	return nil
}

// blobUploadResponse provides a standard request for uploading blobs and
// chunk responses. This sets the correct headers but the response status is
// left to the caller. The fresh argument is used to ensure that new blob
// uploads always start at a 0 offset. This allows disabling resumable push by
// always returning a 0 offset on check status.
func (buh *blobUploadHandler) blobUploadResponse(w http.ResponseWriter, r *http.Request, fresh bool) error {
	// TODO(stevvooe): Need a better way to manage the upload state automatically.
	buh.State.Name = buh.Repository.Named().Name()
	buh.State.UUID = buh.Upload.ID()
	buh.Upload.Close()
	buh.State.Offset = buh.Upload.Size()
	buh.State.StartedAt = buh.Upload.StartedAt()

	token, err := hmacKey(buh.Config.HTTP.Secret).packUploadState(buh.State)
	if err != nil {
		dcontext.GetLogger(buh).Infof("error building upload state token: %s", err)
		return err
	}

	uploadURL, err := buh.urlBuilder.BuildBlobUploadChunkURL(
		buh.Repository.Named(), buh.Upload.ID(),
		url.Values{
			"_state": []string{token},
		})
	if err != nil {
		dcontext.GetLogger(buh).Infof("error building upload url: %s", err)
		return err
	}

	endRange := buh.Upload.Size()
	if endRange > 0 {
		endRange = endRange - 1
	}

	w.Header().Set("Docker-Upload-UUID", buh.UUID)
	w.Header().Set("Location", uploadURL)

	w.Header().Set("Content-Length", "0")
	w.Header().Set("Range", fmt.Sprintf("0-%d", endRange))

	return nil
}

// mountBlob attempts to mount a blob from another repository by its digest. If
// successful, the blob is linked into the blob store and 201 Created is
// returned with the canonical url of the blob.
func (buh *blobUploadHandler) createBlobMountOption(fromRepo, mountDigest string) (distribution.BlobCreateOption, error) {
	dgst, err := digest.Parse(mountDigest)
	if err != nil {
		return nil, err
	}

	ref, err := reference.WithName(fromRepo)
	if err != nil {
		return nil, err
	}

	canonical, err := reference.WithDigest(ref, dgst)
	if err != nil {
		return nil, err
	}

	return storage.WithMountFrom(canonical), nil
}

// writeBlobCreatedHeaders writes the standard headers describing a newly
// created blob. A 201 Created is written as well as the canonical URL and
// blob digest.
func (buh *blobUploadHandler) writeBlobCreatedHeaders(w http.ResponseWriter, desc distribution.Descriptor) error {
	ref, err := reference.WithDigest(buh.Repository.Named(), desc.Digest)
	if err != nil {
		return err
	}
	blobURL, err := buh.urlBuilder.BuildBlobURL(ref)
	if err != nil {
		return err
	}

	w.Header().Set("Location", blobURL)
	w.Header().Set("Content-Length", "0")
	w.Header().Set("Docker-Content-Digest", desc.Digest.String())
	w.WriteHeader(http.StatusCreated)
	return nil
}
