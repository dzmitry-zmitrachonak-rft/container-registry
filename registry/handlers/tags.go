package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/api/errcode"
	"github.com/docker/distribution/registry/api/v2"
	"github.com/gorilla/handlers"
	"github.com/opencontainers/go-digest"
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

func (th *tagsHandler) filterTags(mediaType string, tags []string) ([]string, error) {
	if mediaType == "" {
		return tags, nil
	}

	dcontext.GetLoggerWithField(th, "media_type", mediaType).Debug("filtering tags by media type")

	var matchingTags []string

	manifestService, err := th.Repository.Manifests(th)
	if err != nil {
		th.Errors = append(th.Errors, err)
		return matchingTags, err
	}

	var mediaTypeCache map[digest.Digest]string

	tagService := th.Repository.Tags(th)

	for _, tag := range tags {
		desc, err := tagService.Get(th, tag)
		if err != nil {
			return matchingTags, err
		}

		tagMediaType, ok := mediaTypeCache[desc.Digest]
		if !ok {
			manifest, err := manifestService.Get(th, desc.Digest)
			if err != nil {
				return matchingTags, err
			}

			descriptors := manifest.References()
			tagMediaType = descriptors[0].MediaType
		}

		dcontext.GetLoggerWithFields(th, map[interface{}]interface{}{
			"tag":        tag,
			"media_type": tagMediaType,
		}).Debug("tag media type detected")

		if tagMediaType == mediaType {
			matchingTags = append(matchingTags, tag)
		}
	}

	return matchingTags, nil
}

// GetTags returns a json list of tags for a specific image name.
func (th *tagsHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

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

	mediaType := r.URL.Query().Get("media_type")

	tags, err = th.filterTags(mediaType, tags)
	if err != nil {
		th.Errors = append(th.Errors, err)
		return
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
