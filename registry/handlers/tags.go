package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/gorilla/handlers"
)

// tagsDispatcher constructs the tags handler api endpoint.
func tagsDispatcher(ctx *Context, r *http.Request) http.Handler {
	tagsHandler := &tagsHandler{
		Context: ctx,
	}

	return handlers.MethodHandler{
		"GET": http.HandlerFunc(tagsHandler.GetTags),
	}
}

// tagsHandler handles requests for lists of tags under a repository name.
type tagsHandler struct {
	*Context
}

type tagsAPIResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// GetTags returns a json list of tags for a specific image name.
func (th *tagsHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	mediaType := r.URL.Query().Get("media_type")
	dcontext.GetLogger(th).Debugf("media type is %s", mediaType)

	tagService := th.Repository.Tags(th)
	tags, err := tagService.All(th)
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

	manifestService, err := th.Repository.Manifests(th)
	if err != nil {
		th.Errors = append(th.Errors, err)
		return
	}

	for _, tag := range tags {
		desc, err := tagService.Get(th, tag)
		if err != nil {
			th.Errors = append(th.Errors, err)
			return
		}

		manifest, err := manifestService.Get(th, desc.Digest)
		if err != nil {
			th.Errors = append(th.Errors, err)
			return
		}

		descriptors := manifest.References()

		for _, descriptor := range descriptors {
			dcontext.GetLogger(th).Debugf("media type is %s", descriptor.MediaType)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	enc := json.NewEncoder(w)
	if err := enc.Encode(tagsAPIResponse{
		Name: th.Repository.Named().Name(),
		Tags: tags,
	}); err != nil {
		th.Errors = append(th.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}
}
