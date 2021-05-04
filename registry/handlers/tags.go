package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/docker/distribution/registry/datastore"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"github.com/gorilla/handlers"
)

// tagsDispatcher constructs the tags handler api endpoint.
func tagsDispatcher(ctx *Context, r *http.Request) http.Handler {
	tagsHandler := &tagsHandler{
		Context: ctx,
	}
	h := handlers.MethodHandler{
		"GET": http.HandlerFunc(tagsHandler.GetTags),
	}
	return migrationWrapper(ctx, h)
}

// tagsHandler handles requests for lists of tags under a repository name.
type tagsHandler struct {
	*Context
}

type tagsAPIResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func dbGetTags(ctx context.Context, db datastore.Queryer, repoPath string, n int, last string) ([]string, bool, error) {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": repoPath, "limit": n, "marker": last})
	log.Debug("finding tags in database")

	rStore := datastore.NewRepositoryStore(db)
	r, err := rStore.FindByPath(ctx, repoPath)
	if err != nil {
		return nil, false, err
	}
	if r == nil {
		log.Warn("repository not found in database")
		return nil, false, v2.ErrorCodeNameUnknown.WithDetail(map[string]string{"name": repoPath})
	}

	tt, err := rStore.TagsPaginated(ctx, r, n, last)
	if err != nil {
		return nil, false, err
	}

	tags := make([]string, 0, len(tt))
	for _, t := range tt {
		tags = append(tags, t.Name)
	}

	var moreEntries bool
	if len(tt) > 0 {
		n, err := rStore.TagsCountAfterName(ctx, r, tt[len(tt)-1].Name)
		if err != nil {
			return nil, false, err
		}
		moreEntries = n > 0
	}

	return tags, moreEntries, nil
}

// GetTags returns a json list of tags for a specific image name.
func (th *tagsHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	// Pagination headers are currently only supported by the metadata database backend
	q := r.URL.Query()
	lastEntry := q.Get("last")
	maxEntries, err := strconv.Atoi(q.Get("n"))
	if err != nil || maxEntries <= 0 {
		maxEntries = maximumReturnedEntries
	}

	var tags []string
	var moreEntries bool

	if th.Config.Database.Enabled {
		tags, moreEntries, err = dbGetTags(th.Context, th.db, th.Repository.Named().Name(), maxEntries, lastEntry)
		if err != nil {
			th.Errors = append(th.Errors, errcode.FromUnknownError(err))
			return
		}
		if len(tags) == 0 {
			// If no tags are found, the current implementation (`else`) returns a nil slice instead of an empty one,
			// so we have to enforce the same behavior here, for consistency.
			tags = nil
		}
	} else {
		tagService := th.Repository.Tags(th)
		tags, err = tagService.All(th)
		if err != nil {
			switch err := err.(type) {
			case distribution.ErrRepositoryUnknown:
				th.Errors = append(th.Errors, v2.ErrorCodeNameUnknown.WithDetail(map[string]string{"name": th.Repository.Named().Name()}))
			case errcode.Error:
				th.Errors = append(th.Errors, err)
			default:
				th.Errors = append(th.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			}
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")

	// Add a link header if there are more entries to retrieve (only supported by the metadata database backend)
	if moreEntries {
		lastEntry = tags[len(tags)-1]
		urlStr, err := createLinkEntry(r.URL.String(), maxEntries, lastEntry)
		if err != nil {
			th.Errors = append(th.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			return
		}
		w.Header().Set("Link", urlStr)
	}

	enc := json.NewEncoder(w)
	if err := enc.Encode(tagsAPIResponse{
		Name: th.Repository.Named().Name(),
		Tags: tags,
	}); err != nil {
		th.Errors = append(th.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}
}

// tagDispatcher constructs the tag handler api endpoint.
func tagDispatcher(ctx *Context, r *http.Request) http.Handler {
	thandler := handlers.MethodHandler{}

	tagHandler := &tagHandler{
		Context: ctx,
		Tag:     getTag(ctx),
	}

	if !ctx.readOnly {
		thandler["DELETE"] = http.HandlerFunc(tagHandler.DeleteTag)
	}

	return migrationWrapper(ctx, thandler)
}

// tagHandler handles requests for a specific tag under a repository name.
type tagHandler struct {
	*Context
	Tag string
}

const (
	tagDeleteGCReviewWindow = 1 * time.Hour
	tagDeleteGCLockTimeout  = 5 * time.Second
)

func dbDeleteTag(ctx context.Context, db datastore.Handler, repoPath string, tagName string) error {
	log := dcontext.GetLoggerWithFields(ctx, map[interface{}]interface{}{"repository": repoPath, "tag": tagName})
	log.Debug("deleting tag from repository in database")

	rStore := datastore.NewRepositoryStore(db)
	r, err := rStore.FindByPath(ctx, repoPath)
	if err != nil {
		return err
	}
	if r == nil {
		return distribution.ErrRepositoryUnknown{Name: repoPath}
	}

	// We first check if the tag exists and grab the corresponding manifest ID, then we find and lock a related online
	// GC manifest review record (if any) to prevent conflicting online GC reviews, and only then delete the tag. See:
	// https://gitlab.com/gitlab-org/container-registry/-/blob/master/docs-gitlab/db/online-garbage-collection.md#deleting-the-last-referencing-tag

	t, err := rStore.FindTagByName(ctx, r, tagName)
	if err != nil {
		return err
	}
	if t == nil {
		return distribution.ErrTagUnknown{Tag: tagName}
	}

	// Prevent long running transactions by setting an upper limit of tagDeleteGCLockTimeout. If the GC is holding
	// the lock of a related review record, the processing there should be fast enough to avoid this. Regardless, we
	// should not let transactions open (and clients waiting) for too long. If this sensible timeout is exceeded, abort
	// the tag delete and let the client retry. This will bubble up and lead to a 503 Service Unavailable response.
	txCtx, cancel := context.WithTimeout(ctx, tagDeleteGCLockTimeout)
	defer cancel()

	tx, err := db.BeginTx(txCtx, nil)
	if err != nil {
		return fmt.Errorf("failed to create database transaction: %w", err)
	}
	defer tx.Rollback()

	mts := datastore.NewGCManifestTaskStore(tx)
	if _, err := mts.FindAndLockBefore(txCtx, r.NamespaceID, r.ID, t.ManifestID, time.Now().Add(tagDeleteGCReviewWindow)); err != nil {
		return err
	}

	// The `SELECT FOR UPDATE` on the review queue and the subsequent tag delete must be executed within the same
	// transaction. The tag delete will trigger `gc_track_deleted_tags`, which will attempt to acquire the same row
	// lock on the review queue in case of conflict. Not using the same transaction for both operations (i.e., using
	// `tx` for `FindAndLockBefore` and `db` for `DeleteTagByName`) would therefore result in a deadlock.
	rStore = datastore.NewRepositoryStore(tx)
	found, err := rStore.DeleteTagByName(txCtx, r, tagName)
	if err != nil {
		return err
	}
	if !found {
		return distribution.ErrTagUnknown{Tag: tagName}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit database transaction: %w", err)
	}

	return nil
}

// DeleteTag deletes a tag for a specific image name.
func (th *tagHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	dcontext.GetLogger(th).Debug("DeleteTag")

	if th.App.isCache {
		th.Errors = append(th.Errors, errcode.ErrorCodeUnsupported)
		return
	}

	if !th.App.Config.Migration.DisableMirrorFS {
		tagService := th.Repository.Tags(th)
		if err := tagService.Untag(th.Context, th.Tag); err != nil {
			th.appendDeleteTagError(err)
			return
		}
	}

	if th.App.Config.Database.Enabled {
		if err := dbDeleteTag(th.Context, th.db, th.Repository.Named().Name(), th.Tag); err != nil {
			th.appendDeleteTagError(err)
			return
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

func (th *tagHandler) appendDeleteTagError(err error) {
	switch err.(type) {
	case distribution.ErrRepositoryUnknown:
		th.Errors = append(th.Errors, v2.ErrorCodeNameUnknown)
	case distribution.ErrTagUnknown, storagedriver.PathNotFoundError:
		th.Errors = append(th.Errors, v2.ErrorCodeManifestUnknown)
	default:
		th.Errors = append(th.Errors, errcode.FromUnknownError(err))
	}
	return
}
