// +build integration

package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/distribution"
	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/docker/distribution/migrations"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/docker/distribution/registry/datastore"
	"github.com/docker/distribution/registry/datastore/models"
	dbtestutil "github.com/docker/distribution/registry/datastore/testutil"
	registryhandlers "github.com/docker/distribution/registry/handlers"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/driver/factory"
	"github.com/docker/distribution/registry/storage/driver/inmemory"
	_ "github.com/docker/distribution/registry/storage/driver/testdriver"
	"github.com/docker/distribution/testutil"
	"github.com/docker/distribution/version"
	"github.com/docker/libtrust"
	"github.com/gorilla/handlers"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func init() {
	factory.Register("sharedinmemorydriver", &sharedInMemoryDriverFactory{make(map[string]*inmemory.Driver)})
	factory.Register("schema1Preseededinmemorydriver", &schema1PreseededInMemoryDriverFactory{})
}

type configOpt func(*configuration.Configuration)

func withDelete(config *configuration.Configuration) {
	config.Storage["delete"] = configuration.Parameters{"enabled": true}
}

func withAccessLog(config *configuration.Configuration) {
	config.Log.AccessLog.Disabled = false
}

func withReadOnly(config *configuration.Configuration) {
	if _, ok := config.Storage["maintenance"]; !ok {
		config.Storage["maintenance"] = configuration.Parameters{}
	}

	config.Storage["maintenance"]["readonly"] = map[interface{}]interface{}{"enabled": true}
}

func disableMirrorFS(config *configuration.Configuration) {
	config.Migration.DisableMirrorFS = true
}

func withSharedInMemoryDriver(name string) configOpt {
	return func(config *configuration.Configuration) {
		config.Storage["sharedinmemorydriver"] = configuration.Parameters{"name": name}
	}
}

func withSchema1PreseededInMemoryDriver(config *configuration.Configuration) {
	config.Storage["schema1Preseededinmemorydriver"] = configuration.Parameters{}
}

func withDBHostAndPort(host string, port int) configOpt {
	return func(config *configuration.Configuration) {
		config.Database.Host = host
		config.Database.Port = port
	}
}

func withDBConnectTimeout(d time.Duration) configOpt {
	return func(config *configuration.Configuration) {
		config.Database.ConnectTimeout = d
	}
}

func withDBPoolMaxOpen(n int) configOpt {
	return func(config *configuration.Configuration) {
		config.Database.Pool.MaxOpen = n
	}
}

var headerConfig = http.Header{
	"X-Content-Type-Options": []string{"nosniff"},
}

type tagsAPIResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// digestSha256EmptyTar is the canonical sha256 digest of empty data
const digestSha256EmptyTar = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// TestCheckAPI hits the base endpoint (/v2/) ensures we return the specified
// 200 OK response.
func TestCheckAPI(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()
	baseURL, err := env.builder.BuildBaseURL()
	if err != nil {
		t.Fatalf("unexpected error building base url: %v", err)
	}

	resp, err := http.Get(baseURL)
	if err != nil {
		t.Fatalf("unexpected error issuing request: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, "issuing api base check", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Content-Type":                       []string{"application/json"},
		"Content-Length":                     []string{"2"},
		"Gitlab-Container-Registry-Version":  []string{strings.TrimPrefix(version.Version, "v")},
		"Gitlab-Container-Registry-Features": []string{version.ExtFeatures},
	})

	p, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("unexpected error reading response body: %v", err)
	}

	if string(p) != "{}" {
		t.Fatalf("unexpected response body: %v", string(p))
	}
}

type catalogAPIResponse struct {
	Repositories []string `json:"repositories"`
}

// catalog_Get tests the /v2/_catalog endpoint
func catalog_Get(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	sortedRepos := []string{
		"2j2ar",
		"asj9e/ieakg",
		"dcsl6/xbd1z/9t56s",
		"hpgkt/bmawb",
		"jyi7b",
		"jyi7b/sgv2q/d5a2f",
		"jyi7b/sgv2q/fxt1v",
		"kb0j5/pic0i",
		"n343n",
		"sb71y",
	}

	// shuffle repositories to make sure results are consistent regardless of creation order (it matters when running
	// against the metadata database)
	shuffledRepos := shuffledCopy(sortedRepos)

	for _, repo := range shuffledRepos {
		createRepository(t, env, repo, "latest")
	}

	tt := []struct {
		name               string
		queryParams        url.Values
		expectedBody       catalogAPIResponse
		expectedLinkHeader string
	}{
		{
			name:         "no query parameters",
			expectedBody: catalogAPIResponse{Repositories: sortedRepos},
		},
		{
			name:         "empty last query parameter",
			queryParams:  url.Values{"last": []string{""}},
			expectedBody: catalogAPIResponse{Repositories: sortedRepos},
		},
		{
			name:         "empty n query parameter",
			queryParams:  url.Values{"n": []string{""}},
			expectedBody: catalogAPIResponse{Repositories: sortedRepos},
		},
		{
			name:         "empty last and n query parameters",
			queryParams:  url.Values{"last": []string{""}, "n": []string{""}},
			expectedBody: catalogAPIResponse{Repositories: sortedRepos},
		},
		{
			name:         "non integer n query parameter",
			queryParams:  url.Values{"n": []string{"foo"}},
			expectedBody: catalogAPIResponse{Repositories: sortedRepos},
		},
		{
			name:        "1st page",
			queryParams: url.Values{"n": []string{"4"}},
			expectedBody: catalogAPIResponse{Repositories: []string{
				"2j2ar",
				"asj9e/ieakg",
				"dcsl6/xbd1z/9t56s",
				"hpgkt/bmawb",
			}},
			expectedLinkHeader: `</v2/_catalog?last=hpgkt%2Fbmawb&n=4>; rel="next"`,
		},
		{
			name:        "nth page",
			queryParams: url.Values{"last": []string{"hpgkt/bmawb"}, "n": []string{"4"}},
			expectedBody: catalogAPIResponse{Repositories: []string{
				"jyi7b",
				"jyi7b/sgv2q/d5a2f",
				"jyi7b/sgv2q/fxt1v",
				"kb0j5/pic0i",
			}},
			expectedLinkHeader: `</v2/_catalog?last=kb0j5%2Fpic0i&n=4>; rel="next"`,
		},
		{
			name:        "last page",
			queryParams: url.Values{"last": []string{"kb0j5/pic0i"}, "n": []string{"4"}},
			expectedBody: catalogAPIResponse{Repositories: []string{
				"n343n",
				"sb71y",
			}},
		},
		{
			name:         "zero page size",
			queryParams:  url.Values{"n": []string{"0"}},
			expectedBody: catalogAPIResponse{Repositories: sortedRepos},
		},
		{
			name:         "page size bigger than full list",
			queryParams:  url.Values{"n": []string{"100"}},
			expectedBody: catalogAPIResponse{Repositories: sortedRepos},
		},
		{
			name:        "after marker",
			queryParams: url.Values{"last": []string{"kb0j5/pic0i"}},
			expectedBody: catalogAPIResponse{Repositories: []string{
				"n343n",
				"sb71y",
			}},
		},
		{
			name:        "after non existent marker",
			queryParams: url.Values{"last": []string{"does-not-exist"}},
			expectedBody: catalogAPIResponse{Repositories: []string{
				"hpgkt/bmawb",
				"jyi7b",
				"jyi7b/sgv2q/d5a2f",
				"jyi7b/sgv2q/fxt1v",
				"kb0j5/pic0i",
				"n343n",
				"sb71y",
			}},
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			catalogURL, err := env.builder.BuildCatalogURL(test.queryParams)
			require.NoError(t, err)

			resp, err := http.Get(catalogURL)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)

			var body catalogAPIResponse
			dec := json.NewDecoder(resp.Body)
			err = dec.Decode(&body)
			require.NoError(t, err)

			require.Equal(t, test.expectedBody, body)
			require.Equal(t, test.expectedLinkHeader, resp.Header.Get("Link"))
		})
	}

	// If the database is enabled, disable it and rerun the tests again with the
	// database to check that the filesystem mirroring worked correctly.
	if env.config.Database.Enabled && !env.config.Migration.DisableMirrorFS {
		env.config.Database.Enabled = false
		defer func() { env.config.Database.Enabled = true }()

		for _, test := range tt {
			t.Run(fmt.Sprintf("%s filesystem mirroring", test.name), func(t *testing.T) {
				catalogURL, err := env.builder.BuildCatalogURL(test.queryParams)
				require.NoError(t, err)

				resp, err := http.Get(catalogURL)
				require.NoError(t, err)
				defer resp.Body.Close()

				require.Equal(t, http.StatusOK, resp.StatusCode)

				var body catalogAPIResponse
				dec := json.NewDecoder(resp.Body)
				err = dec.Decode(&body)
				require.NoError(t, err)

				require.Equal(t, test.expectedBody, body)
				require.Equal(t, test.expectedLinkHeader, resp.Header.Get("Link"))
			})
		}
	}
}

func catalog_Get_Empty(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	catalogURL, err := env.builder.BuildCatalogURL()
	require.NoError(t, err)

	resp, err := http.Get(catalogURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body catalogAPIResponse
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&body)
	require.NoError(t, err)

	require.Len(t, body.Repositories, 0)
	require.Empty(t, resp.Header.Get("Link"))
}

func newConfig(opts ...configOpt) configuration.Configuration {
	config := &configuration.Configuration{
		Storage: configuration.Storage{
			"maintenance": configuration.Parameters{
				"uploadpurging": map[interface{}]interface{}{"enabled": false},
			},
		},
	}
	config.HTTP.Headers = headerConfig

	if os.Getenv("REGISTRY_DATABASE_ENABLED") == "true" {
		dsn, err := dbtestutil.NewDSNFromEnv()
		if err != nil {
			panic(fmt.Sprintf("error creating dsn: %v", err))
		}

		config.Database = configuration.Database{
			Enabled:     true,
			Host:        dsn.Host,
			Port:        dsn.Port,
			User:        dsn.User,
			Password:    dsn.Password,
			DBName:      dsn.DBName,
			SSLMode:     dsn.SSLMode,
			SSLCert:     dsn.SSLCert,
			SSLKey:      dsn.SSLKey,
			SSLRootCert: dsn.SSLRootCert,
		}
	}

	for _, o := range opts {
		o(config)
	}

	// If no driver was configured, default to test driver, if multiple drivers
	// were configured, this will panic.
	if config.Storage.Type() == "" {
		config.Storage["testdriver"] = configuration.Parameters{}
	}

	return *config
}

// sharedInMemoryDriverFactory implements the factory.StorageDriverFactory interface.
type sharedInMemoryDriverFactory struct {
	drivers map[string]*inmemory.Driver
}

// Create returns a shared instance of the inmemory storage driver by name,
// or creates a new one if it does not exist.
func (factory *sharedInMemoryDriverFactory) Create(parameters map[string]interface{}) (storagedriver.StorageDriver, error) {
	n, ok := parameters["name"]
	if !ok {
		return nil, errors.New("sharedInMemoryDriverFactory: parameter 'name' must be specified")
	}

	name, ok := n.(string)
	if !ok {
		return nil, errors.New("sharedInMemoryDriverFactory: parameter 'name' must be a string")
	}

	if _, ok := factory.drivers[name]; !ok {
		factory.drivers[name] = inmemory.New()
	}

	return factory.drivers[name], nil
}

var (
	preseededSchema1RepoPath = "schema1/preseeded"
	preseededSchema1TagName  = "schema1preseededtag"
	preseededSchema1Digest   digest.Digest
)

// schema1PreseededInMemoryDriverFactory implements the factory.StorageDriverFactory interface.
type schema1PreseededInMemoryDriverFactory struct{}

// Create returns a shared instance of the inmemory storage driver with a
// preseeded schema1 manifest. This allows us to test GETs against schema1
// manifests even though we are unable to PUT schema1 manifests via the API.
func (factory *schema1PreseededInMemoryDriverFactory) Create(parameters map[string]interface{}) (storagedriver.StorageDriver, error) {
	d := inmemory.New()

	unsignedManifest := &schema1.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 1,
		},
		Name:    preseededSchema1RepoPath,
		Tag:     preseededSchema1TagName,
		History: []schema1.History{},
	}

	pk, err := libtrust.GenerateECP256PrivateKey()
	if err != nil {
		return nil, err
	}

	sm, err := schema1.Sign(unsignedManifest, pk)
	if err != nil {
		return nil, err
	}

	dgst := digest.FromBytes(sm.Canonical)
	preseededSchema1Digest = dgst

	manifestTagCurrentPath := filepath.Clean(fmt.Sprintf("/docker/registry/v2/repositories/%s/_manifests/tags/%s/current/link", preseededSchema1RepoPath, preseededSchema1TagName))
	manifestRevisionLinkPath := filepath.Clean(fmt.Sprintf("/docker/registry/v2/repositories/%s/_manifests/revisions/sha256/%s/link", preseededSchema1RepoPath, dgst.Hex()))
	blobDataPath := filepath.Clean(fmt.Sprintf("/docker/registry/v2/blobs/sha256/%s/%s/data", dgst.Hex()[0:2], dgst.Hex()))

	ctx := context.Background()

	d.PutContent(ctx, manifestTagCurrentPath, []byte(dgst))
	d.PutContent(ctx, manifestRevisionLinkPath, []byte(dgst))
	d.PutContent(ctx, blobDataPath, sm.Canonical)

	return d, nil
}

func TestURLPrefix(t *testing.T) {
	config := newConfig()
	config.HTTP.Prefix = "/test/"

	env := newTestEnvWithConfig(t, &config)
	defer env.Shutdown()

	baseURL, err := env.builder.BuildBaseURL()
	if err != nil {
		t.Fatalf("unexpected error building base url: %v", err)
	}

	parsed, _ := url.Parse(baseURL)
	if !strings.HasPrefix(parsed.Path, config.HTTP.Prefix) {
		t.Fatalf("Prefix %v not included in test url %v", config.HTTP.Prefix, baseURL)
	}

	resp, err := http.Get(baseURL)
	if err != nil {
		t.Fatalf("unexpected error issuing request: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, "issuing api base check", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Content-Type":   []string{"application/json"},
		"Content-Length": []string{"2"},
	})
}

type blobArgs struct {
	imageName   reference.Named
	layerFile   io.ReadSeeker
	layerDigest digest.Digest
}

func makeBlobArgs(t *testing.T) blobArgs {
	layerFile, layerDigest, err := testutil.CreateRandomTarFile()
	if err != nil {
		t.Fatalf("error creating random layer file: %v", err)
	}

	args := blobArgs{
		layerFile:   layerFile,
		layerDigest: layerDigest,
	}
	args.imageName, _ = reference.WithName("foo/bar")
	return args
}

// TestBlobAPI conducts a full test of the of the blob api.
func TestBlobAPI(t *testing.T) {
	env1 := newTestEnv(t)
	args := makeBlobArgs(t)
	testBlobAPI(t, env1, args)
	env1.Shutdown()

	env2 := newTestEnv(t, withDelete)
	args = makeBlobArgs(t)
	testBlobAPI(t, env2, args)
	env2.Shutdown()
}

func blob_Get(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	blobURL := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// fetch layer
	res, err := http.Get(blobURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	// verify response headers
	_, err = args.layerFile.Seek(0, io.SeekStart)
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(args.layerFile)
	require.NoError(t, err)

	require.Equal(t, res.Header.Get("Content-Length"), strconv.Itoa(buf.Len()))
	require.Equal(t, res.Header.Get("Content-Type"), "application/octet-stream")
	require.Equal(t, res.Header.Get("Docker-Content-Digest"), args.layerDigest.String())
	require.Equal(t, res.Header.Get("ETag"), fmt.Sprintf(`"%s"`, args.layerDigest))
	require.Equal(t, res.Header.Get("Cache-Control"), "max-age=31536000")

	// verify response body
	v := args.layerDigest.Verifier()
	_, err = io.Copy(v, res.Body)
	require.NoError(t, err)
	require.True(t, v.Verified())
}

func TestBlobAPI_Get_BlobNotInDatabase(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	// Disable the database so writes only go to the filesytem.
	env.config.Database.Enabled = false

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	blobURL := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// Enable the database again so that reads first check the database.
	env.config.Database.Enabled = true

	// fetch layer
	res, err := http.Get(blobURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func blob_Get_RepositoryNotFound(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	args := makeBlobArgs(t)
	ref, err := reference.WithDigest(args.imageName, args.layerDigest)
	require.NoError(t, err)

	blobURL, err := env.builder.BuildBlobURL(ref)
	require.NoError(t, err)

	resp, err := http.Get(blobURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	checkBodyHasErrorCodes(t, "repository not found", resp, v2.ErrorCodeBlobUnknown)
}

func blob_Get_BlobNotFound(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	location := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// delete blob link from repository
	res, err := httpDelete(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, res.StatusCode)

	// test
	res, err = http.Get(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	checkBodyHasErrorCodes(t, "blob not found", res, v2.ErrorCodeBlobUnknown)
}

func TestBlobAPI_GetBlobFromFilesystemAfterDatabaseWrites(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	blobURL := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// Disable the database to check that the filesystem mirroring worked correctly.
	env.config.Database.Enabled = false

	// fetch layer
	res, err := http.Get(blobURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	// verify response headers
	_, err = args.layerFile.Seek(0, io.SeekStart)
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(args.layerFile)
	require.NoError(t, err)

	require.Equal(t, res.Header.Get("Content-Length"), strconv.Itoa(buf.Len()))
	require.Equal(t, res.Header.Get("Content-Type"), "application/octet-stream")
	require.Equal(t, res.Header.Get("Docker-Content-Digest"), args.layerDigest.String())
	require.Equal(t, res.Header.Get("ETag"), fmt.Sprintf(`"%s"`, args.layerDigest))
	require.Equal(t, res.Header.Get("Cache-Control"), "max-age=31536000")

	// verify response body
	v := args.layerDigest.Verifier()
	_, err = io.Copy(v, res.Body)
	require.NoError(t, err)
	require.True(t, v.Verified())
}

func TestBlobAPI_GetBlobFromFilesystemAfterDatabaseWrites_DisableMirrorFS(t *testing.T) {
	env := newTestEnv(t, disableMirrorFS)
	defer env.Shutdown()

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	blobURL := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// Disable the database to check that the filesystem mirroring was disabled.
	env.config.Database.Enabled = false

	// fetch layer
	res, err := http.Get(blobURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func blob_Head(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	blobURL := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// check if layer exists
	res, err := http.Head(blobURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)

	// verify headers
	_, err = args.layerFile.Seek(0, io.SeekStart)
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(args.layerFile)
	require.NoError(t, err)

	require.Equal(t, res.Header.Get("Content-Type"), "application/octet-stream")
	require.Equal(t, res.Header.Get("Content-Length"), strconv.Itoa(buf.Len()))
	require.Equal(t, res.Header.Get("Docker-Content-Digest"), args.layerDigest.String())
	require.Equal(t, res.Header.Get("ETag"), fmt.Sprintf(`"%s"`, args.layerDigest))
	require.Equal(t, res.Header.Get("Cache-Control"), "max-age=31536000")

	// verify body
	require.Equal(t, http.NoBody, res.Body)
}

func blob_Head_RepositoryNotFound(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	args := makeBlobArgs(t)
	ref, err := reference.WithDigest(args.imageName, args.layerDigest)
	require.NoError(t, err)

	blobURL, err := env.builder.BuildBlobURL(ref)
	require.NoError(t, err)

	res, err := http.Head(blobURL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.Equal(t, http.NoBody, res.Body)
}

func blob_Head_BlobNotFound(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	location := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// delete blob link from repository
	res, err := httpDelete(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, res.StatusCode)

	// test
	res, err = http.Head(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.Equal(t, http.NoBody, res.Body)
}

func blob_Delete_Disabled(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	location := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// Attempt to delete blob link from repository.
	res, err := httpDelete(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)
}

func blob_Delete_AlreadyDeleted(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// create repository with a layer
	args := makeBlobArgs(t)
	uploadURLBase, _ := startPushLayer(t, env, args.imageName)
	location := pushLayer(t, env.builder, args.imageName, args.layerDigest, uploadURLBase, args.layerFile)

	// delete blob link from repository
	res, err := httpDelete(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, res.StatusCode)

	// test
	res, err = http.Head(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.Equal(t, http.NoBody, res.Body)

	// Attempt to delete blob link from repository again.
	res, err = httpDelete(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func blob_Delete_UnknownRepository(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// Create url for a blob whose repository does not exist.
	args := makeBlobArgs(t)

	digester := digest.Canonical.Digester()
	sha256Dgst := digester.Digest()

	ref, err := reference.WithDigest(args.imageName, sha256Dgst)
	require.NoError(t, err)

	location, err := env.builder.BuildBlobURL(ref)
	require.NoError(t, err)

	// delete blob link from repository
	res, err := httpDelete(location)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestBlobDelete(t *testing.T) {
	env := newTestEnv(t, withDelete)
	defer env.Shutdown()

	args := makeBlobArgs(t)
	env = testBlobAPI(t, env, args)
	testBlobDelete(t, env, args)
}

func TestRelativeURL(t *testing.T) {
	config := newConfig()
	config.HTTP.RelativeURLs = false
	env := newTestEnvWithConfig(t, &config)
	defer env.Shutdown()
	ref, _ := reference.WithName("foo/bar")
	uploadURLBaseAbs, _ := startPushLayer(t, env, ref)

	u, err := url.Parse(uploadURLBaseAbs)
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAbs() {
		t.Fatal("Relative URL returned from blob upload chunk with non-relative configuration")
	}

	args := makeBlobArgs(t)
	resp, err := doPushLayer(t, env.builder, ref, args.layerDigest, uploadURLBaseAbs, args.layerFile)
	if err != nil {
		t.Fatalf("unexpected error doing layer push relative url: %v", err)
	}
	checkResponse(t, "relativeurl blob upload", resp, http.StatusCreated)
	u, err = url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAbs() {
		t.Fatal("Relative URL returned from blob upload with non-relative configuration")
	}

	config.HTTP.RelativeURLs = true
	args = makeBlobArgs(t)
	uploadURLBaseRelative, _ := startPushLayer(t, env, ref)
	u, err = url.Parse(uploadURLBaseRelative)
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAbs() {
		t.Fatal("Absolute URL returned from blob upload chunk with relative configuration")
	}

	// Start a new upload in absolute mode to get a valid base URL
	config.HTTP.RelativeURLs = false
	uploadURLBaseAbs, _ = startPushLayer(t, env, ref)
	u, err = url.Parse(uploadURLBaseAbs)
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAbs() {
		t.Fatal("Relative URL returned from blob upload chunk with non-relative configuration")
	}

	// Complete upload with relative URLs enabled to ensure the final location is relative
	config.HTTP.RelativeURLs = true
	resp, err = doPushLayer(t, env.builder, ref, args.layerDigest, uploadURLBaseAbs, args.layerFile)
	if err != nil {
		t.Fatalf("unexpected error doing layer push relative url: %v", err)
	}

	checkResponse(t, "relativeurl blob upload", resp, http.StatusCreated)
	u, err = url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAbs() {
		t.Fatal("Relative URL returned from blob upload with non-relative configuration")
	}
}

func TestBlobDeleteDisabled(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()
	args := makeBlobArgs(t)

	imageName := args.imageName
	layerDigest := args.layerDigest
	ref, _ := reference.WithDigest(imageName, layerDigest)
	layerURL, err := env.builder.BuildBlobURL(ref)
	if err != nil {
		t.Fatalf("error building url: %v", err)
	}

	resp, err := httpDelete(layerURL)
	if err != nil {
		t.Fatalf("unexpected error deleting when disabled: %v", err)
	}

	checkResponse(t, "status of disabled delete", resp, http.StatusMethodNotAllowed)
}

func testBlobAPI(t *testing.T, env *testEnv, args blobArgs) *testEnv {
	// TODO(stevvooe): This test code is complete junk but it should cover the
	// complete flow. This must be broken down and checked against the
	// specification *before* we submit the final to docker core.
	imageName := args.imageName
	layerFile := args.layerFile
	layerDigest := args.layerDigest

	ref, _ := reference.WithDigest(imageName, layerDigest)
	layerURL, err := env.builder.BuildBlobURL(ref)
	if err != nil {
		t.Fatalf("error building url: %v", err)
	}

	// ------------------------------------------
	// Start an upload, check the status then cancel
	uploadURLBase, uploadUUID := startPushLayer(t, env, imageName)

	// A status check should work
	resp, err := http.Get(uploadURLBase)
	if err != nil {
		t.Fatalf("unexpected error getting upload status: %v", err)
	}
	checkResponse(t, "status of deleted upload", resp, http.StatusNoContent)
	checkHeaders(t, resp, http.Header{
		"Location":           []string{"*"},
		"Range":              []string{"0-0"},
		"Docker-Upload-UUID": []string{uploadUUID},
	})

	req, err := http.NewRequest("DELETE", uploadURLBase, nil)
	if err != nil {
		t.Fatalf("unexpected error creating delete request: %v", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error sending delete request: %v", err)
	}

	checkResponse(t, "deleting upload", resp, http.StatusNoContent)

	// A status check should result in 404
	resp, err = http.Get(uploadURLBase)
	if err != nil {
		t.Fatalf("unexpected error getting upload status: %v", err)
	}
	checkResponse(t, "status of deleted upload", resp, http.StatusNotFound)

	// -----------------------------------------
	// Do layer push with an empty body and different digest
	uploadURLBase, _ = startPushLayer(t, env, imageName)
	resp, err = doPushLayer(t, env.builder, imageName, layerDigest, uploadURLBase, bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("unexpected error doing bad layer push: %v", err)
	}

	checkResponse(t, "bad layer push", resp, http.StatusBadRequest)
	checkBodyHasErrorCodes(t, "bad layer push", resp, v2.ErrorCodeDigestInvalid)

	// -----------------------------------------
	// Do layer push with an empty body and correct digest
	zeroDigest, err := digest.FromReader(bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("unexpected error digesting empty buffer: %v", err)
	}

	uploadURLBase, _ = startPushLayer(t, env, imageName)
	pushLayer(t, env.builder, imageName, zeroDigest, uploadURLBase, bytes.NewReader([]byte{}))

	// -----------------------------------------
	// Do layer push with an empty body and correct digest

	// This is a valid but empty tarfile!
	emptyTar := bytes.Repeat([]byte("\x00"), 1024)
	emptyDigest, err := digest.FromReader(bytes.NewReader(emptyTar))
	if err != nil {
		t.Fatalf("unexpected error digesting empty tar: %v", err)
	}

	uploadURLBase, _ = startPushLayer(t, env, imageName)
	pushLayer(t, env.builder, imageName, emptyDigest, uploadURLBase, bytes.NewReader(emptyTar))

	// ------------------------------------------
	// Now, actually do successful upload.
	layerLength, _ := layerFile.Seek(0, io.SeekEnd)
	layerFile.Seek(0, io.SeekStart)

	uploadURLBase, _ = startPushLayer(t, env, imageName)
	pushLayer(t, env.builder, imageName, layerDigest, uploadURLBase, layerFile)

	// ------------------------------------------
	// Now, push just a chunk
	layerFile.Seek(0, 0)

	canonicalDigester := digest.Canonical.Digester()
	if _, err := io.Copy(canonicalDigester.Hash(), layerFile); err != nil {
		t.Fatalf("error copying to digest: %v", err)
	}
	canonicalDigest := canonicalDigester.Digest()

	layerFile.Seek(0, 0)
	uploadURLBase, _ = startPushLayer(t, env, imageName)
	uploadURLBase, dgst := pushChunk(t, env.builder, imageName, uploadURLBase, layerFile, layerLength)
	finishUpload(t, env.builder, imageName, uploadURLBase, dgst)

	// ------------------------
	// Use a head request to see if the layer exists.
	resp, err = http.Head(layerURL)
	if err != nil {
		t.Fatalf("unexpected error checking head on existing layer: %v", err)
	}

	checkResponse(t, "checking head on existing layer", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Content-Length":        []string{fmt.Sprint(layerLength)},
		"Docker-Content-Digest": []string{canonicalDigest.String()},
	})

	// ----------------
	// Fetch the layer!
	resp, err = http.Get(layerURL)
	if err != nil {
		t.Fatalf("unexpected error fetching layer: %v", err)
	}

	checkResponse(t, "fetching layer", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Content-Length":        []string{fmt.Sprint(layerLength)},
		"Docker-Content-Digest": []string{canonicalDigest.String()},
	})

	// Verify the body
	verifier := layerDigest.Verifier()
	io.Copy(verifier, resp.Body)

	if !verifier.Verified() {
		t.Fatalf("response body did not pass verification")
	}

	// ----------------
	// Fetch the layer with an invalid digest
	badURL := strings.Replace(layerURL, "sha256", "sha257", 1)
	resp, err = http.Get(badURL)
	if err != nil {
		t.Fatalf("unexpected error fetching layer: %v", err)
	}

	checkResponse(t, "fetching layer bad digest", resp, http.StatusBadRequest)

	// Cache headers
	resp, err = http.Get(layerURL)
	if err != nil {
		t.Fatalf("unexpected error fetching layer: %v", err)
	}

	checkResponse(t, "fetching layer", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Content-Length":        []string{fmt.Sprint(layerLength)},
		"Docker-Content-Digest": []string{canonicalDigest.String()},
		"ETag":                  []string{fmt.Sprintf(`"%s"`, canonicalDigest)},
		"Cache-Control":         []string{"max-age=31536000"},
	})

	// Matching etag, gives 304
	etag := resp.Header.Get("Etag")
	req, err = http.NewRequest("GET", layerURL, nil)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}
	req.Header.Set("If-None-Match", etag)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}

	checkResponse(t, "fetching layer with etag", resp, http.StatusNotModified)

	// Non-matching etag, gives 200
	req, err = http.NewRequest("GET", layerURL, nil)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}
	req.Header.Set("If-None-Match", "")
	resp, _ = http.DefaultClient.Do(req)
	checkResponse(t, "fetching layer with invalid etag", resp, http.StatusOK)

	// Missing tests:
	//	- Upload the same tar file under and different repository and
	//       ensure the content remains uncorrupted.
	return env
}

func testBlobDelete(t *testing.T, env *testEnv, args blobArgs) {
	// Upload a layer
	imageName := args.imageName
	layerFile := args.layerFile
	layerDigest := args.layerDigest

	ref, _ := reference.WithDigest(imageName, layerDigest)
	layerURL, err := env.builder.BuildBlobURL(ref)
	if err != nil {
		t.Fatalf(err.Error())
	}
	// ---------------
	// Delete a layer
	resp, err := httpDelete(layerURL)
	if err != nil {
		t.Fatalf("unexpected error deleting layer: %v", err)
	}

	checkResponse(t, "deleting layer", resp, http.StatusAccepted)
	checkHeaders(t, resp, http.Header{
		"Content-Length": []string{"0"},
	})

	// ---------------
	// Try and get it back
	// Use a head request to see if the layer exists.
	resp, err = http.Head(layerURL)
	if err != nil {
		t.Fatalf("unexpected error checking head on existing layer: %v", err)
	}

	checkResponse(t, "checking existence of deleted layer", resp, http.StatusNotFound)

	// Delete already deleted layer
	resp, err = httpDelete(layerURL)
	if err != nil {
		t.Fatalf("unexpected error deleting layer: %v", err)
	}

	checkResponse(t, "deleting layer", resp, http.StatusNotFound)

	// ----------------
	// Attempt to delete a layer with an invalid digest
	badURL := strings.Replace(layerURL, "sha256", "sha257", 1)
	resp, err = httpDelete(badURL)
	if err != nil {
		t.Fatalf("unexpected error fetching layer: %v", err)
	}

	checkResponse(t, "deleting layer bad digest", resp, http.StatusBadRequest)

	// ----------------
	// Reupload previously deleted blob
	layerFile.Seek(0, io.SeekStart)

	uploadURLBase, _ := startPushLayer(t, env, imageName)
	pushLayer(t, env.builder, imageName, layerDigest, uploadURLBase, layerFile)

	layerFile.Seek(0, io.SeekStart)
	canonicalDigester := digest.Canonical.Digester()
	if _, err := io.Copy(canonicalDigester.Hash(), layerFile); err != nil {
		t.Fatalf("error copying to digest: %v", err)
	}
	canonicalDigest := canonicalDigester.Digest()

	// ------------------------
	// Use a head request to see if it exists
	resp, err = http.Head(layerURL)
	if err != nil {
		t.Fatalf("unexpected error checking head on existing layer: %v", err)
	}

	layerLength, _ := layerFile.Seek(0, io.SeekEnd)
	checkResponse(t, "checking head on reuploaded layer", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Content-Length":        []string{fmt.Sprint(layerLength)},
		"Docker-Content-Digest": []string{canonicalDigest.String()},
	})
}

func TestDeleteDisabled(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	imageName, _ := reference.WithName("foo/bar")
	// "build" our layer file
	layerFile, layerDigest, err := testutil.CreateRandomTarFile()
	if err != nil {
		t.Fatalf("error creating random layer file: %v", err)
	}

	ref, _ := reference.WithDigest(imageName, layerDigest)
	layerURL, err := env.builder.BuildBlobURL(ref)
	if err != nil {
		t.Fatalf("Error building blob URL")
	}
	uploadURLBase, _ := startPushLayer(t, env, imageName)
	pushLayer(t, env.builder, imageName, layerDigest, uploadURLBase, layerFile)

	resp, err := httpDelete(layerURL)
	if err != nil {
		t.Fatalf("unexpected error deleting layer: %v", err)
	}

	checkResponse(t, "deleting layer with delete disabled", resp, http.StatusMethodNotAllowed)
}

func TestDeleteReadOnly(t *testing.T) {
	setupEnv := newTestEnv(t, withSharedInMemoryDriver("TestDeleteReadOnlyDriver"))
	defer setupEnv.Shutdown()

	imageName, _ := reference.WithName("foo/bar")
	// "build" our layer file
	layerFile, layerDigest, err := testutil.CreateRandomTarFile()
	if err != nil {
		t.Fatalf("error creating random layer file: %v", err)
	}

	ref, _ := reference.WithDigest(imageName, layerDigest)
	uploadURLBase, _ := startPushLayer(t, setupEnv, imageName)
	pushLayer(t, setupEnv.builder, imageName, layerDigest, uploadURLBase, layerFile)

	// Reconfigure environment with withReadOnly enabled.
	setupEnv.Shutdown()
	env := newTestEnv(t, withSharedInMemoryDriver("TestDeleteReadOnlyDriver"), withReadOnly)
	defer env.Shutdown()

	layerURL, err := env.builder.BuildBlobURL(ref)
	if err != nil {
		t.Fatalf("Error building blob URL")
	}

	resp, err := httpDelete(layerURL)
	if err != nil {
		t.Fatalf("unexpected error deleting layer: %v", err)
	}

	checkResponse(t, "deleting layer in read-only mode", resp, http.StatusMethodNotAllowed)
}

func TestStartPushReadOnly(t *testing.T) {
	env := newTestEnv(t, withDelete, withReadOnly)
	defer env.Shutdown()

	imageName, _ := reference.WithName("foo/bar")

	layerUploadURL, err := env.builder.BuildBlobUploadURL(imageName)
	if err != nil {
		t.Fatalf("unexpected error building layer upload url: %v", err)
	}

	resp, err := http.Post(layerUploadURL, "", nil)
	if err != nil {
		t.Fatalf("unexpected error starting layer push: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, "starting push in read-only mode", resp, http.StatusMethodNotAllowed)
}

func httpDelete(url string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	//	defer resp.Body.Close()
	return resp, err
}

func httpOptions(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, err
}

type manifestArgs struct {
	imageName reference.Named
	mediaType string
	manifest  distribution.Manifest
	dgst      digest.Digest
}

// storageManifestErrDriverFactory implements the factory.StorageDriverFactory interface.
type storageManifestErrDriverFactory struct{}

const (
	repositoryWithManifestNotFound    = "manifesttagnotfound"
	repositoryWithManifestInvalidPath = "manifestinvalidpath"
	repositoryWithManifestBadLink     = "manifestbadlink"
	repositoryWithGenericStorageError = "genericstorageerr"
)

func (factory *storageManifestErrDriverFactory) Create(parameters map[string]interface{}) (storagedriver.StorageDriver, error) {
	// Initialize the mock driver
	var errGenericStorage = errors.New("generic storage error")
	return &mockErrorDriver{
		returnErrs: []mockErrorMapping{
			{
				pathMatch: fmt.Sprintf("%s/_manifests/tags", repositoryWithManifestNotFound),
				content:   nil,
				err:       storagedriver.PathNotFoundError{},
			},
			{
				pathMatch: fmt.Sprintf("%s/_manifests/tags", repositoryWithManifestInvalidPath),
				content:   nil,
				err:       storagedriver.InvalidPathError{},
			},
			{
				pathMatch: fmt.Sprintf("%s/_manifests/tags", repositoryWithManifestBadLink),
				content:   []byte("this is a bad sha"),
				err:       nil,
			},
			{
				pathMatch: fmt.Sprintf("%s/_manifests/tags", repositoryWithGenericStorageError),
				content:   nil,
				err:       errGenericStorage,
			},
		},
	}, nil
}

type mockErrorMapping struct {
	pathMatch string
	content   []byte
	err       error
}

// mockErrorDriver implements StorageDriver to force storage error on manifest request
type mockErrorDriver struct {
	storagedriver.StorageDriver
	returnErrs []mockErrorMapping
}

func (dr *mockErrorDriver) GetContent(ctx context.Context, path string) ([]byte, error) {
	for _, returns := range dr.returnErrs {
		if strings.Contains(path, returns.pathMatch) {
			return returns.content, returns.err
		}
	}
	return nil, errors.New("Unknown storage error")
}

func TestGetManifestWithStorageError(t *testing.T) {
	factory.Register("storagemanifesterror", &storageManifestErrDriverFactory{})
	config := configuration.Configuration{
		Storage: configuration.Storage{
			"storagemanifesterror": configuration.Parameters{},
			"maintenance": configuration.Parameters{"uploadpurging": map[interface{}]interface{}{
				"enabled": false,
			}},
		},
	}
	config.HTTP.Headers = headerConfig
	env1 := newTestEnvWithConfig(t, &config)
	defer env1.Shutdown()

	repo, _ := reference.WithName(repositoryWithManifestNotFound)
	testManifestWithStorageError(t, env1, repo, http.StatusNotFound, v2.ErrorCodeManifestUnknown)

	repo, _ = reference.WithName(repositoryWithGenericStorageError)
	testManifestWithStorageError(t, env1, repo, http.StatusInternalServerError, errcode.ErrorCodeUnknown)

	repo, _ = reference.WithName(repositoryWithManifestInvalidPath)
	testManifestWithStorageError(t, env1, repo, http.StatusInternalServerError, errcode.ErrorCodeUnknown)

	repo, _ = reference.WithName(repositoryWithManifestBadLink)
	testManifestWithStorageError(t, env1, repo, http.StatusNotFound, v2.ErrorCodeManifestUnknown)
}

func testManifestWithStorageError(t *testing.T, env *testEnv, imageName reference.Named, expectedStatusCode int, expectedErrorCode errcode.ErrorCode) {
	tag := "latest"
	tagRef, _ := reference.WithTag(imageName, tag)
	manifestURL, err := env.builder.BuildManifestURL(tagRef)
	if err != nil {
		t.Fatalf("unexpected error getting manifest url: %v", err)
	}

	// -----------------------------
	// Attempt to fetch the manifest
	resp, err := http.Get(manifestURL)
	if err != nil {
		t.Fatalf("unexpected error getting manifest: %v", err)
	}
	defer resp.Body.Close()
	checkResponse(t, "getting non-existent manifest", resp, expectedStatusCode)
	checkBodyHasErrorCodes(t, "getting non-existent manifest", resp, expectedErrorCode)
	return
}

// TestAPIConformance runs a variety of tests against different environments
// where the external behavior of the API is expected to be equivalent.
func TestAPIConformance(t *testing.T) {
	var testFuncs = []func(*testing.T, ...configOpt){
		manifest_Put_Schema1_ByTag,
		manifest_Put_Schema2_ByDigest,
		manifest_Put_Schema2_ByDigest_ConfigNotAssociatedWithRepository,
		manifest_Put_Schema2_ByDigest_LayersNotAssociatedWithRepository,
		manifest_Put_Schema2_ByTag,
		manifest_Put_Schema2_ByTag_IsIdempotent,
		manifest_Put_Schema2_MissingConfig,
		manifest_Put_Schema2_MissingConfigAndLayers,
		manifest_Put_Schema2_MissingLayers,
		manifest_Put_Schema2_ReuseTagManifestToManifest,
		manifest_Head_Schema2,
		manifest_Head_Schema2_MissingManifest,
		manifest_Get_Schema2_ByDigest_MissingManifest,
		manifest_Get_Schema2_ByDigest_MissingRepository,
		manifest_Get_Schema2_NoAcceptHeaders,
		manifest_Get_Schema2_ByDigest_NotAssociatedWithRepository,
		manifest_Get_Schema2_ByTag_MissingRepository,
		manifest_Get_Schema2_ByTag_MissingTag,
		manifest_Get_Schema2_ByTag_NotAssociatedWithRepository,
		manifest_Get_Schema2_MatchingEtag,
		manifest_Get_Schema2_NonMatchingEtag,
		manifest_Delete_Schema2,
		manifest_Delete_Schema2_AlreadyDeleted,
		manifest_Delete_Schema2_Reupload,
		manifest_Delete_Schema2_MissingManifest,
		manifest_Delete_Schema2_ClearsTags,
		manifest_Delete_Schema2_DeleteDisabled,

		manifest_Put_OCI_ByDigest,
		manifest_Put_OCI_ByTag,
		manifest_Get_OCI_MatchingEtag,
		manifest_Get_OCI_NonMatchingEtag,

		manifest_Put_OCIImageIndex_ByDigest,
		manifest_Put_OCIImageIndex_ByTag,
		manifest_Get_OCIIndex_MatchingEtag,
		manifest_Get_OCIIndex_NonMatchingEtag,

		manifest_Get_ManifestList_FallbackToSchema2,

		blob_Head,
		blob_Head_BlobNotFound,
		blob_Head_RepositoryNotFound,
		blob_Get,
		blob_Get_BlobNotFound,
		blob_Get_RepositoryNotFound,
		blob_Delete_AlreadyDeleted,
		blob_Delete_Disabled,
		blob_Delete_UnknownRepository,

		tags_Get,
		tags_Get_EmptyRepository,
		tags_Get_RepositoryNotFound,
		tags_Delete,
		tags_Delete_AllowedMethods,
		tags_Delete_AllowedMethodsReadOnly,
		tags_Delete_ReadOnly,
		tags_Delete_Unknown,
		tags_Delete_UnknownRepository,
		tags_Delete_WithSameImageID,

		catalog_Get,
		catalog_Get_Empty,
	}

	type envOpt struct {
		name string
		opts []configOpt
	}

	var envOpts = []envOpt{
		{
			"with filesystem mirroring",
			[]configOpt{},
		},
	}

	if os.Getenv("REGISTRY_DATABASE_ENABLED") == "true" {
		envOpts = append(envOpts, envOpt{
			"with filesystem mirroring disabled",
			[]configOpt{disableMirrorFS},
		})
	}

	for _, f := range testFuncs {
		for _, o := range envOpts {
			t.Run(funcName(f)+" "+o.name, func(t *testing.T) {
				f(t, o.opts...)
			})
		}
	}
}

func funcName(f func(*testing.T, ...configOpt)) string {
	ptr := reflect.ValueOf(f).Pointer()
	name := runtime.FuncForPC(ptr).Name()
	segments := strings.Split(name, ".")

	return segments[len(segments)-1]
}

func manifest_Put_Schema2_ByTag_IsIdempotent(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "idempotentag"
	repoPath := "schema2/idempotent"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath)

	// Build URLs and headers.
	manifestURL := buildManifestTagURL(t, env, repoPath, tagName)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	// Put the same manifest twice to test idempotentcy.
	resp := putManifest(t, "putting manifest by tag no error", manifestURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))
	require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))

	resp = putManifest(t, "putting manifest by tag no error", manifestURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))
	require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
}

func manifest_Put_Schema2_ReuseTagManifestToManifest(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "replacesmanifesttag"
	repoPath := "schema2/replacesmanifest"

	seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Fetch original manifest by tag name
	manifestURL := buildManifestTagURL(t, env, repoPath, tagName)

	req, err := http.NewRequest("GET", manifestURL, nil)
	require.NoError(t, err)

	req.Header.Set("Accept", schema2.MediaTypeManifest)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "fetching uploaded manifest", resp, http.StatusOK)

	var fetchedOriginalManifest schema2.DeserializedManifest
	dec := json.NewDecoder(resp.Body)

	err = dec.Decode(&fetchedOriginalManifest)
	require.NoError(t, err)

	_, originalPayload, err := fetchedOriginalManifest.Payload()
	require.NoError(t, err)

	// Create a new manifest and push it up with the same tag.
	newManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Fetch new manifest by tag name
	req, err = http.NewRequest("GET", manifestURL, nil)
	require.NoError(t, err)

	req.Header.Set("Accept", schema2.MediaTypeManifest)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "fetching uploaded manifest", resp, http.StatusOK)

	var fetchedNewManifest schema2.DeserializedManifest
	dec = json.NewDecoder(resp.Body)

	err = dec.Decode(&fetchedNewManifest)
	require.NoError(t, err)

	// Ensure that we pulled down the new manifest by the same tag.
	require.Equal(t, *newManifest, fetchedNewManifest)

	// Ensure that the tag refered to different manifests over time.
	require.NotEqual(t, fetchedOriginalManifest, fetchedNewManifest)

	_, newPayload, err := fetchedNewManifest.Payload()
	require.NoError(t, err)

	require.NotEqual(t, originalPayload, newPayload)
}

func manifest_Put_Schema2_ByTag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2happypathtag"
	repoPath := "schema2/happypath"

	// seedRandomSchema2Manifest with putByTag tests that the manifest put
	// happened without issue.
	seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

}

func manifest_Put_Schema2_ByDigest(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	repoPath := "schema2/happypath"

	// seedRandomSchema2Manifest with putByDigest tests that the manifest put
	// happened without issue.
	seedRandomSchema2Manifest(t, env, repoPath, putByDigest)
}

func manifest_Get_Schema2_NonMatchingEtag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2happypathtag"
	repoPath := "schema2/happypath"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
		{
			name:        "by tag non matching etag",
			manifestURL: tagURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by digest non matching etag",
			manifestURL: digestURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by tag malformed etag",
			manifestURL: tagURL,
			etag:        "bad etag",
		},
		{
			name:        "by digest malformed etag",
			manifestURL: digestURL,
			etag:        "bad etag",
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", schema2.MediaTypeManifest)
			if test.etag != "" {
				req.Header.Set("If-None-Match", test.etag)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
			require.Equal(t, fmt.Sprintf(`"%s"`, dgst), resp.Header.Get("ETag"))

			var fetchedManifest *schema2.DeserializedManifest
			dec := json.NewDecoder(resp.Body)

			err = dec.Decode(&fetchedManifest)
			require.NoError(t, err)

			require.EqualValues(t, deserializedManifest, fetchedManifest)
		})
	}
}

func manifest_Get_Schema2_MatchingEtag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2happypathtag"
	repoPath := "schema2/happypath"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag quoted etag",
			manifestURL: tagURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by digest quoted etag",
			manifestURL: digestURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by tag non quoted etag",
			manifestURL: tagURL,
			etag:        dgst.String(),
		},
		{
			name:        "by digest non quoted etag",
			manifestURL: digestURL,
			etag:        dgst.String(),
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", schema2.MediaTypeManifest)
			req.Header.Set("If-None-Match", test.etag)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusNotModified, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, http.NoBody, resp.Body)
		})
	}
}

func Benchmark_Get_Schema2_MatchingEtag(b *testing.B) {
	env := newTestEnv(b)
	defer env.Shutdown()

	tagName := "schema2happypathtag"
	repoPath := "schema2/happypath"

	deserializedManifest := seedRandomSchema2Manifest(b, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(b, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(b, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(b, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag quoted etag",
			manifestURL: tagURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by digest quoted etag",
			manifestURL: digestURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by tag non quoted etag",
			manifestURL: tagURL,
			etag:        dgst.String(),
		},
		{
			name:        "by digest non quoted etag",
			manifestURL: digestURL,
			etag:        dgst.String(),
		},
	}

	for _, test := range tt {
		b.Run(test.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				req, err := http.NewRequest("GET", test.manifestURL, nil)
				require.NoError(b, err)

				req.Header.Set("If-None-Match", test.etag)

				b.StartTimer()
				resp, err := http.DefaultClient.Do(req)
				require.NoError(b, err)
				defer resp.Body.Close()

				require.Equal(b, http.StatusNotModified, resp.StatusCode)
			}
		})
	}
}

func Benchmark_Get_Schema2_NonMatchingEtag(b *testing.B) {
	env := newTestEnv(b)
	defer env.Shutdown()

	tagName := "schema2happypathtag"
	repoPath := "schema2/happypath"

	deserializedManifest := seedRandomSchema2Manifest(b, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(b, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(b, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
		{
			name:        "by tag non matching etag",
			manifestURL: tagURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by digest non matching etag",
			manifestURL: digestURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by tag malformed etag",
			manifestURL: tagURL,
			etag:        "bad etag",
		},
		{
			name:        "by digest malformed etag",
			manifestURL: digestURL,
			etag:        "bad etag",
		},
	}

	for _, test := range tt {
		b.Run(test.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				req, err := http.NewRequest("GET", test.manifestURL, nil)
				require.NoError(b, err)

				req.Header.Set("Accept", schema2.MediaTypeManifest)
				if test.etag != "" {
					req.Header.Set("If-None-Match", test.etag)
				}

				b.StartTimer()
				resp, err := http.DefaultClient.Do(req)
				require.NoError(b, err)
				defer resp.Body.Close()

				require.Equal(b, http.StatusOK, resp.StatusCode)
			}
		})
	}
}

func TestManifestAPI_Get_Schema2LayersAndConfigNotInDatabase(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	tagName := "schema2fallbacktag"
	repoPath := "schema2/fallback"

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName), writeToFilesystemOnly)

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", schema2.MediaTypeManifest)
			if test.etag != "" {
				req.Header.Set("If-None-Match", test.etag)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

func manifest_Put_Schema2_MissingConfig(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2missingconfigtag"
	repoPath := "schema2/missingconfig"

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	manifest := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
	}

	// Create a manifest config but do not push up its content.
	_, cfgDesc := schema2Config()
	manifest.Config = cfgDesc

	// Create and push up 2 random layers.
	manifest.Layers = make([]distribution.Descriptor, 2)

	for i := range manifest.Layers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, repoRef)
		pushLayer(t, env.builder, repoRef, dgst, uploadURLBase, rs)

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: schema2.MediaTypeLayer,
		}
	}

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)

	deserializedManifest, err := schema2.FromStruct(*manifest)
	require.NoError(t, err)

	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {

			// Push up the manifest with only the layer blobs pushed up.
			resp := putManifest(t, "putting missing config manifest", test.manifestURL, schema2.MediaTypeManifest, manifest)
			defer resp.Body.Close()
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))

			// Test that we have one missing blob.
			_, p, counts := checkBodyHasErrorCodes(t, "putting missing config manifest", resp, v2.ErrorCodeManifestBlobUnknown)
			expectedCounts := map[errcode.ErrorCode]int{v2.ErrorCodeManifestBlobUnknown: 1}

			require.EqualValuesf(t, expectedCounts, counts, "response body: %s", p)
		})
	}
}

func manifest_Put_Schema2_MissingLayers(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2missinglayerstag"
	repoPath := "schema2/missinglayers"

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	manifest := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
	}

	// Create a manifest config and push up its content.
	cfgPayload, cfgDesc := schema2Config()
	uploadURLBase, _ := startPushLayer(t, env, repoRef)
	pushLayer(t, env.builder, repoRef, cfgDesc.Digest, uploadURLBase, bytes.NewReader(cfgPayload))
	manifest.Config = cfgDesc

	// Create and push up 2 random layers, but do not push their content.
	manifest.Layers = make([]distribution.Descriptor, 2)

	for i := range manifest.Layers {
		_, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: schema2.MediaTypeLayer,
		}
	}

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)

	deserializedManifest, err := schema2.FromStruct(*manifest)
	require.NoError(t, err)

	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {

			// Push up the manifest with only the config blob pushed up.
			resp := putManifest(t, "putting missing layers", test.manifestURL, schema2.MediaTypeManifest, manifest)
			defer resp.Body.Close()
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))

			// Test that we have two missing blobs, one for each layer.
			_, p, counts := checkBodyHasErrorCodes(t, "putting missing config manifest", resp, v2.ErrorCodeManifestBlobUnknown)
			expectedCounts := map[errcode.ErrorCode]int{v2.ErrorCodeManifestBlobUnknown: 2}

			require.EqualValuesf(t, expectedCounts, counts, "response body: %s", p)
		})
	}
}

func manifest_Put_Schema2_MissingConfigAndLayers(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2missingconfigandlayerstag"
	repoPath := "schema2/missingconfigandlayers"

	manifest := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
	}

	// Create a random layer and push up its content to ensure repository
	// exists and that we are only testing missing manifest references.
	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	rs, dgstStr, err := testutil.CreateRandomTarFile()
	require.NoError(t, err)

	uploadURLBase, _ := startPushLayer(t, env, repoRef)
	pushLayer(t, env.builder, repoRef, digest.Digest(dgstStr), uploadURLBase, rs)

	// Create a manifest config, but do not push up its content.
	_, cfgDesc := schema2Config()
	manifest.Config = cfgDesc

	// Create and push up 2 random layers, but do not push thier content.
	manifest.Layers = make([]distribution.Descriptor, 2)

	for i := range manifest.Layers {
		_, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: schema2.MediaTypeLayer,
		}
	}

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)

	deserializedManifest, err := schema2.FromStruct(*manifest)
	require.NoError(t, err)

	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {

			// Push up the manifest with only the config blob pushed up.
			resp := putManifest(t, "putting missing layers", test.manifestURL, schema2.MediaTypeManifest, manifest)
			defer resp.Body.Close()
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))

			// Test that we have two missing blobs, one for each layer, and one for the config.
			_, p, counts := checkBodyHasErrorCodes(t, "putting missing config manifest", resp, v2.ErrorCodeManifestBlobUnknown)
			expectedCounts := map[errcode.ErrorCode]int{v2.ErrorCodeManifestBlobUnknown: 3}

			require.EqualValuesf(t, expectedCounts, counts, "response body: %s", p)
		})
	}
}

func TestManifestAPI_Put_Schema2LayersNotAssociatedWithRepositoryButArePresentInDatabase(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	tagName := "schema2missinglayerstag"
	repoPath := "schema2/missinglayers"

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	manifest := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
	}

	// Create a manifest config and push up its content.
	cfgPayload, cfgDesc := schema2Config()
	uploadURLBase, _ := startPushLayer(t, env, repoRef)
	pushLayer(t, env.builder, repoRef, cfgDesc.Digest, uploadURLBase, bytes.NewReader(cfgPayload))
	manifest.Config = cfgDesc

	// Create and push up 2 random layers to an unrelated repo so that they are
	// present within the database, but not associated with the manifest's repository.
	// Then push them to the normal repository with the database disabled.
	manifest.Layers = make([]distribution.Descriptor, 2)

	fakeRepoRef, err := reference.WithName("fakerepo")
	require.NoError(t, err)

	for i := range manifest.Layers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		// Save the layer content as pushLayer exhausts the io.ReadSeeker
		layerBytes, err := ioutil.ReadAll(rs)
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, fakeRepoRef)
		pushLayer(t, env.builder, fakeRepoRef, dgst, uploadURLBase, bytes.NewReader(layerBytes))

		// Disable the database so writes only go to the filesytem.
		env.config.Database.Enabled = false

		uploadURLBase, _ = startPushLayer(t, env, repoRef)
		pushLayer(t, env.builder, repoRef, dgst, uploadURLBase, bytes.NewReader(layerBytes))

		// Enable the database again so that reads first check the database.
		env.config.Database.Enabled = true

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: schema2.MediaTypeLayer,
		}
	}

	deserializedManifest, err := schema2.FromStruct(*manifest)
	require.NoError(t, err)

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)

	resp := putManifest(t, "putting manifest, layers not associated with repository", tagURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func manifest_Get_Schema2_ByDigest_MissingManifest(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "missingmanifesttag"
	repoPath := "schema2/missingmanifest"

	// Push up a manifest so that the repository is created. This way we can
	// test the case where a manifest is not present in a repository, as opposed
	// to the case where an entire repository does not exist.
	seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	dgst := digest.FromString("bogus digest")

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	digestRef, err := reference.WithDigest(repoRef, dgst)
	require.NoError(t, err)

	bogusManifestDigestURL, err := env.builder.BuildManifestURL(digestRef)
	require.NoError(t, err)

	req, err := http.NewRequest("GET", bogusManifestDigestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "getting non-existent manifest", resp, http.StatusNotFound)
	checkBodyHasErrorCodes(t, "getting non-existent manifest", resp, v2.ErrorCodeManifestUnknown)
}

func manifest_Get_Schema2_ByDigest_MissingRepository(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "missingrepositorytag"
	repoPath := "schema2/missingrepository"

	// Push up a manifest so that it exists within the registry. We'll attempt to
	// get the manifest by digest from a non-existant repository, which should fail.
	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestDigestURL := buildManifestDigestURL(t, env, "fake/repo", deserializedManifest)

	req, err := http.NewRequest("GET", manifestDigestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "getting non-existent manifest", resp, http.StatusNotFound)
	checkBodyHasErrorCodes(t, "getting non-existent manifest", resp, v2.ErrorCodeManifestUnknown)
}

func manifest_Get_Schema2_ByTag_MissingRepository(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "missingrepositorytag"
	repoPath := "schema2/missingrepository"

	// Push up a manifest so that it exists within the registry. We'll attempt to
	// get the manifest by tag from a non-existant repository, which should fail.
	seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestURL := buildManifestTagURL(t, env, "fake/repo", tagName)

	req, err := http.NewRequest("GET", manifestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "getting non-existent manifest", resp, http.StatusNotFound)
	checkBodyHasErrorCodes(t, "getting non-existent manifest", resp, v2.ErrorCodeManifestUnknown)
}

func manifest_Get_Schema2_ByTag_MissingTag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "missingtagtag"
	repoPath := "schema2/missingtag"

	// Push up a manifest so that it exists within the registry. We'll attempt to
	// get the manifest by a non-existant tag, which should fail.
	seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestURL := buildManifestTagURL(t, env, repoPath, "faketag")

	req, err := http.NewRequest("GET", manifestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "getting non-existent manifest", resp, http.StatusNotFound)
	checkBodyHasErrorCodes(t, "getting non-existent manifest", resp, v2.ErrorCodeManifestUnknown)
}

func manifest_Get_Schema2_ByDigest_NotAssociatedWithRepository(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName1 := "missingrepository1tag"
	repoPath1 := "schema2/missingrepository1"

	tagName2 := "missingrepository2tag"
	repoPath2 := "schema2/missingrepository2"

	// Push up two manifests in different repositories so that they both exist
	// within the registry. We'll attempt to get a manifest by digest from the
	// repository to which it does not belong, which should fail.
	seedRandomSchema2Manifest(t, env, repoPath1, putByTag(tagName1))
	deserializedManifest2 := seedRandomSchema2Manifest(t, env, repoPath2, putByTag(tagName2))

	mismatchedManifestURL := buildManifestDigestURL(t, env, repoPath1, deserializedManifest2)

	req, err := http.NewRequest("GET", mismatchedManifestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "getting non-existent manifest", resp, http.StatusNotFound)
	checkBodyHasErrorCodes(t, "getting non-existent manifest", resp, v2.ErrorCodeManifestUnknown)
}

func manifest_Get_Schema2_ByTag_NotAssociatedWithRepository(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName1 := "missingrepository1tag"
	repoPath1 := "schema2/missingrepository1"

	tagName2 := "missingrepository2tag"
	repoPath2 := "schema2/missingrepository2"

	// Push up two manifests in different repositories so that they both exist
	// within the registry. We'll attempt to get a manifest by tag from the
	// repository to which it does not belong, which should fail.
	seedRandomSchema2Manifest(t, env, repoPath1, putByTag(tagName1))
	seedRandomSchema2Manifest(t, env, repoPath2, putByTag(tagName2))

	mismatchedManifestURL := buildManifestTagURL(t, env, repoPath1, tagName2)

	req, err := http.NewRequest("GET", mismatchedManifestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	checkResponse(t, "getting non-existent manifest", resp, http.StatusNotFound)
	checkBodyHasErrorCodes(t, "getting non-existent manifest", resp, v2.ErrorCodeManifestUnknown)
}

func manifest_Put_Schema2_ByDigest_LayersNotAssociatedWithRepository(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	repoPath1 := "schema2/layersnotassociated1"
	repoPath2 := "schema2/layersnotassociated2"

	repoRef1, err := reference.WithName(repoPath1)
	require.NoError(t, err)

	repoRef2, err := reference.WithName(repoPath2)
	require.NoError(t, err)

	manifest := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
	}

	// Create a manifest config and push up its content.
	cfgPayload, cfgDesc := schema2Config()
	uploadURLBase, _ := startPushLayer(t, env, repoRef1)
	pushLayer(t, env.builder, repoRef1, cfgDesc.Digest, uploadURLBase, bytes.NewReader(cfgPayload))
	manifest.Config = cfgDesc

	// Create and push up 2 random layers.
	manifest.Layers = make([]distribution.Descriptor, 2)

	for i := range manifest.Layers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, repoRef2)
		pushLayer(t, env.builder, repoRef2, dgst, uploadURLBase, rs)

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: schema2.MediaTypeLayer,
		}
	}

	deserializedManifest, err := schema2.FromStruct(*manifest)
	require.NoError(t, err)

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath1, deserializedManifest)

	resp := putManifest(t, "putting manifest whose layers are not present in the repository", manifestDigestURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func manifest_Put_Schema2_ByDigest_ConfigNotAssociatedWithRepository(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	repoPath1 := "schema2/layersnotassociated1"
	repoPath2 := "schema2/layersnotassociated2"

	repoRef1, err := reference.WithName(repoPath1)
	require.NoError(t, err)

	repoRef2, err := reference.WithName(repoPath2)
	require.NoError(t, err)

	manifest := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
	}

	// Create a manifest config and push up its content.
	cfgPayload, cfgDesc := schema2Config()
	uploadURLBase, _ := startPushLayer(t, env, repoRef2)
	pushLayer(t, env.builder, repoRef2, cfgDesc.Digest, uploadURLBase, bytes.NewReader(cfgPayload))
	manifest.Config = cfgDesc

	// Create and push up 2 random layers.
	manifest.Layers = make([]distribution.Descriptor, 2)

	for i := range manifest.Layers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, repoRef1)
		pushLayer(t, env.builder, repoRef1, dgst, uploadURLBase, rs)

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: schema2.MediaTypeLayer,
		}
	}

	deserializedManifest, err := schema2.FromStruct(*manifest)
	require.NoError(t, err)

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath1, deserializedManifest)

	resp := putManifest(t, "putting manifest whose config is not present in the repository", manifestDigestURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func manifest_Put_Schema1_ByTag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema1tag"
	repoPath := "schema1"

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	unsignedManifest := &schema1.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 1,
		},
		Name: repoPath,
		Tag:  tagName,
		History: []schema1.History{
			{
				V1Compatibility: "",
			},
			{
				V1Compatibility: "",
			},
		},
	}

	// Create and push up 2 random layers.
	unsignedManifest.FSLayers = make([]schema1.FSLayer, 2)

	for i := range unsignedManifest.FSLayers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, repoRef)
		pushLayer(t, env.builder, repoRef, dgst, uploadURLBase, rs)

		unsignedManifest.FSLayers[i] = schema1.FSLayer{
			BlobSum: dgst,
		}
	}

	signedManifest, err := schema1.Sign(unsignedManifest, env.pk)
	require.NoError(t, err)

	manifestURL := buildManifestTagURL(t, env, repoPath, tagName)

	resp := putManifest(t, "putting schema1 manifest bad request error", manifestURL, schema1.MediaTypeManifest, signedManifest)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	checkBodyHasErrorCodes(t, "invalid manifest", resp, v2.ErrorCodeManifestInvalid)
}

func manifest_Put_Schema1_ByDigest(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	repoPath := "schema1"

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	unsignedManifest := &schema1.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 1,
		},
		Name: repoPath,
		Tag:  "",
		History: []schema1.History{
			{
				V1Compatibility: "",
			},
			{
				V1Compatibility: "",
			},
		},
	}

	// Create and push up 2 random layers.
	unsignedManifest.FSLayers = make([]schema1.FSLayer, 2)

	for i := range unsignedManifest.FSLayers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, repoRef)
		pushLayer(t, env.builder, repoRef, dgst, uploadURLBase, rs)

		unsignedManifest.FSLayers[i] = schema1.FSLayer{
			BlobSum: dgst,
		}
	}

	signedManifest, err := schema1.Sign(unsignedManifest, env.pk)
	require.NoError(t, err)

	manifestURL := buildManifestDigestURL(t, env, repoPath, signedManifest)

	resp := putManifest(t, "putting schema1 manifest bad request error", manifestURL, schema1.MediaTypeManifest, signedManifest)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	checkBodyHasErrorCodes(t, "invalid manifest", resp, v2.ErrorCodeManifestInvalid)
}

func TestManifestAPI_Get_Schema1(t *testing.T) {
	env := newTestEnv(t, withSchema1PreseededInMemoryDriver)
	defer env.Shutdown()

	// Seed manifest in database directly since schema1 manifests are unpushable.
	if env.config.Database.Enabled {
		repositoryStore := datastore.NewRepositoryStore(env.db)
		dbRepo, err := repositoryStore.CreateByPath(env.ctx, preseededSchema1RepoPath)

		mStore := datastore.NewManifestStore(env.db)

		dbManifest := &models.Manifest{
			RepositoryID:  dbRepo.ID,
			SchemaVersion: 1,
			MediaType:     schema1.MediaTypeManifest,
			Digest:        preseededSchema1Digest,
			Payload:       nil,
		}

		err = mStore.Create(env.ctx, dbManifest)
		require.NoError(t, err)

		tagStore := datastore.NewTagStore(env.db)

		dbTag := &models.Tag{
			Name:         preseededSchema1TagName,
			RepositoryID: dbRepo.ID,
			ManifestID:   dbManifest.ID,
		}

		err = tagStore.CreateOrUpdate(env.ctx, dbTag)
		require.NoError(t, err)
	}

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, preseededSchema1RepoPath, preseededSchema1TagName)

	repoRef, err := reference.WithName(preseededSchema1RepoPath)
	require.NoError(t, err)

	digestRef, err := reference.WithDigest(repoRef, preseededSchema1Digest)
	require.NoError(t, err)

	digestURL, err := env.builder.BuildManifestURL(digestRef)
	require.NoError(t, err)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			checkBodyHasErrorCodes(t, "invalid manifest", resp, v2.ErrorCodeManifestInvalid)
		})
	}
}

func manifest_Head_Schema2(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "headtag"
	repoPath := "schema2/head"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("HEAD", test.manifestURL, nil)
			require.NoError(t, err)
			req.Header.Set("Accept", schema2.MediaTypeManifest)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))

			cl, err := strconv.Atoi(resp.Header.Get("Content-Length"))
			require.NoError(t, err)
			require.EqualValues(t, len(payload), cl)

			require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
		})
	}
}

func manifest_Head_Schema2_MissingManifest(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "headtag"
	repoPath := "schema2/missingmanifest"

	// Push up a manifest so that the repository is created. This way we can
	// test the case where a manifest is not present in a repository, as opposed
	// to the case where an entire repository does not exist.
	seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	digestRef, err := reference.WithDigest(repoRef, digest.FromString("bogus digest"))
	require.NoError(t, err)

	digestURL, err := env.builder.BuildManifestURL(digestRef)
	require.NoError(t, err)

	tagURL := buildManifestTagURL(t, env, repoPath, "faketag")

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {

			req, err := http.NewRequest("HEAD", test.manifestURL, nil)
			require.NoError(t, err)
			req.Header.Set("Accept", schema2.MediaTypeManifest)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusNotFound, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		})
	}
}

func manifest_Get_Schema2_NoAcceptHeaders(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "noaccepttag"
	repoPath := "schema2/noaccept"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			// Without any accept headers we should still get a schema2 manifest since
			// schema1 support has been dropped.
			resp, err := http.Get(test.manifestURL)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
			require.Equal(t, fmt.Sprintf("%q", dgst), resp.Header.Get("ETag"))

			var fetchedManifest *schema2.DeserializedManifest
			dec := json.NewDecoder(resp.Body)

			err = dec.Decode(&fetchedManifest)
			require.NoError(t, err)

			require.EqualValues(t, deserializedManifest, fetchedManifest)
		})
	}
}

func TestManifestAPI_Get_Schema2FromFilesystemAfterDatabaseWrites(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	tagName := "schema2consistentfstag"
	repoPath := "schema2/consistentfs"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	// Disable the database to check that the filesystem mirroring worked correctly.
	env.config.Database.Enabled = false

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", schema2.MediaTypeManifest)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
			require.Equal(t, fmt.Sprintf(`"%s"`, dgst), resp.Header.Get("ETag"))

			var fetchedManifest *schema2.DeserializedManifest
			dec := json.NewDecoder(resp.Body)

			err = dec.Decode(&fetchedManifest)
			require.NoError(t, err)

			require.EqualValues(t, deserializedManifest, fetchedManifest)
		})
	}
}

func manifest_Delete_Schema2(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2deletetag"
	repoPath := "schema2/delete"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	resp, err := httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	req, err := http.NewRequest("GET", manifestDigestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	checkBodyHasErrorCodes(t, "getting freshly-deleted manifest", resp, v2.ErrorCodeManifestUnknown)
}

func manifest_Delete_Schema2_AlreadyDeleted(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2deleteagain"
	repoPath := "schema2/deleteagain"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	resp, err := httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	resp, err = httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func manifest_Delete_Schema2_Reupload(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2deletereupload"
	repoPath := "schema2/deletereupload"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	resp, err := httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Re-upload manifest by digest
	resp = putManifest(t, "reuploading manifest no error", manifestDigestURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))

	// Attempt to fetch re-uploaded deleted digest
	req, err := http.NewRequest("GET", manifestDigestURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", schema2.MediaTypeManifest)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func manifest_Delete_Schema2_MissingManifest(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	repoPath := "schema2/deletemissing"

	// Push up random manifest to ensure repo is created.
	seedRandomSchema2Manifest(t, env, repoPath, putByDigest)

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	dgst := digest.FromString("fake-manifest")

	digestRef, err := reference.WithDigest(repoRef, dgst)
	require.NoError(t, err)

	manifestDigestURL, err := env.builder.BuildManifestURL(digestRef)
	require.NoError(t, err)

	resp, err := httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func manifest_Delete_Schema2_ClearsTags(t *testing.T, opts ...configOpt) {
	opts = append(opts, withDelete)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2deletecleartag"
	repoPath := "schema2/delete"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	tagsURL, err := env.builder.BuildTagsURL(repoRef)
	require.NoError(t, err)

	// Ensure that the tag is listed.
	resp, err := http.Get(tagsURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	tagsResponse := tagsAPIResponse{}
	err = dec.Decode(&tagsResponse)
	require.NoError(t, err)

	require.Equal(t, repoPath, tagsResponse.Name)
	require.NotEmpty(t, tagsResponse.Tags)
	require.Equal(t, tagName, tagsResponse.Tags[0])

	// Delete manifest
	resp, err = httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Ensure that the tag is not listed.
	resp, err = http.Get(tagsURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	dec = json.NewDecoder(resp.Body)
	err = dec.Decode(&tagsResponse)
	require.NoError(t, err)

	require.Equal(t, repoPath, tagsResponse.Name)
	require.Empty(t, tagsResponse.Tags)
}

func manifest_Delete_Schema2_DeleteDisabled(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "schema2deletedisabled"
	repoPath := "schema2/delete"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	resp, err := httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestManifestAPI_Delete_Schema2ManifestNotInDatabase(t *testing.T) {
	env := newTestEnv(t, withDelete)
	defer env.Shutdown()

	tagName := "schema2deletetag"
	repoPath := "schema2/delete"

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	// Push a random schema 2 manifest to the repository so that it is present in
	// the database, so only the manifest is not present in the database.
	seedRandomSchema2Manifest(t, env, repoPath, putByDigest)

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName), writeToFilesystemOnly)

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	resp, err := httpDelete(manifestDigestURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func manifest_Put_OCI_ByTag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "ocihappypathtag"
	repoPath := "oci/happypath"

	// seedRandomOCIManifest with putByTag tests that the manifest put happened without issue.
	seedRandomOCIManifest(t, env, repoPath, putByTag(tagName))
}

func manifest_Put_OCI_ByDigest(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	repoPath := "oci/happypath"

	// seedRandomOCIManifest with putByDigest tests that the manifest put happened without issue.
	seedRandomOCIManifest(t, env, repoPath, putByDigest)
}

func TestManifestAPI_Put_OCIFilesystemFallbackLayersNotInDatabase(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	tagName := "ocifallbacktag"
	repoPath := "oci/fallback"

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	deserializedManifest := seedRandomOCIManifest(t, env, repoPath, writeToFilesystemOnly)

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	resp := putManifest(t, "putting manifest no error", tagURL, v1.MediaTypeImageManifest, deserializedManifest.Manifest)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	require.Equal(t, digestURL, resp.Header.Get("Location"))

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)
	dgst := digest.FromBytes(payload)
	require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
}

func manifest_Get_OCI_NonMatchingEtag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "ocihappypathtag"
	repoPath := "oci/happypath"

	deserializedManifest := seedRandomOCIManifest(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
		{
			name:        "by tag non matching etag",
			manifestURL: tagURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by digest non matching etag",
			manifestURL: digestURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by tag malformed etag",
			manifestURL: tagURL,
			etag:        "bad etag",
		},
		{
			name:        "by digest malformed etag",
			manifestURL: digestURL,
			etag:        "bad etag",
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", v1.MediaTypeImageManifest)
			if test.etag != "" {
				req.Header.Set("If-None-Match", test.etag)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
			require.Equal(t, fmt.Sprintf(`"%s"`, dgst), resp.Header.Get("ETag"))

			var fetchedManifest *ocischema.DeserializedManifest
			dec := json.NewDecoder(resp.Body)

			err = dec.Decode(&fetchedManifest)
			require.NoError(t, err)

			require.EqualValues(t, deserializedManifest, fetchedManifest)
		})
	}
}

func manifest_Get_OCI_MatchingEtag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "ocihappypathtag"
	repoPath := "oci/happypath"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag quoted etag",
			manifestURL: tagURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by digest quoted etag",
			manifestURL: digestURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by tag non quoted etag",
			manifestURL: tagURL,
			etag:        dgst.String(),
		},
		{
			name:        "by digest non quoted etag",
			manifestURL: digestURL,
			etag:        dgst.String(),
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", v1.MediaTypeImageManifest)
			req.Header.Set("If-None-Match", test.etag)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusNotModified, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, http.NoBody, resp.Body)
		})
	}
}

func Benchmark_Get_OCI_NonMatchingEtag(b *testing.B) {
	env := newTestEnv(b)
	defer env.Shutdown()

	tagName := "ocihappypathtag"
	repoPath := "oci/happypath"

	deserializedManifest := seedRandomOCIManifest(b, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(b, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(b, env, repoPath, deserializedManifest)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
		{
			name:        "by tag non matching etag",
			manifestURL: tagURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by digest non matching etag",
			manifestURL: digestURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by tag malformed etag",
			manifestURL: tagURL,
			etag:        "bad etag",
		},
		{
			name:        "by digest malformed etag",
			manifestURL: digestURL,
			etag:        "bad etag",
		},
	}

	for _, test := range tt {
		b.Run(test.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				req, err := http.NewRequest("GET", test.manifestURL, nil)
				require.NoError(b, err)

				req.Header.Set("Accept", v1.MediaTypeImageManifest)
				if test.etag != "" {
					req.Header.Set("If-None-Match", test.etag)
				}

				b.StartTimer()
				resp, err := http.DefaultClient.Do(req)
				require.NoError(b, err)
				defer resp.Body.Close()

				require.Equal(b, http.StatusOK, resp.StatusCode)
			}
		})
	}
}

func Benchmark_Get_OCI_MatchingEtag(b *testing.B) {
	env := newTestEnv(b)
	defer env.Shutdown()

	tagName := "ocihappypathtag"
	repoPath := "oci/happypath"

	deserializedManifest := seedRandomSchema2Manifest(b, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(b, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(b, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(b, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag quoted etag",
			manifestURL: tagURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by digest quoted etag",
			manifestURL: digestURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by tag non quoted etag",
			manifestURL: tagURL,
			etag:        dgst.String(),
		},
		{
			name:        "by digest non quoted etag",
			manifestURL: digestURL,
			etag:        dgst.String(),
		},
	}

	for _, test := range tt {
		b.Run(test.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				req, err := http.NewRequest("GET", test.manifestURL, nil)
				require.NoError(b, err)

				req.Header.Set("Accept", v1.MediaTypeImageManifest)
				req.Header.Set("If-None-Match", test.etag)

				b.StartTimer()
				resp, err := http.DefaultClient.Do(req)
				require.NoError(b, err)
				defer resp.Body.Close()

				require.Equal(b, http.StatusNotModified, resp.StatusCode)
			}
		})
	}
}

func manifest_Put_OCIImageIndex_ByTag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "ociindexhappypathtag"
	repoPath := "ociindex/happypath"

	// putRandomOCIImageIndex with putByTag tests that the manifest put happened without issue.
	seedRandomOCIImageIndex(t, env, repoPath, putByTag(tagName))
}

func manifest_Put_OCIImageIndex_ByDigest(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	repoPath := "ociindex/happypath"

	// putRandomOCIImageIndex with putByDigest tests that the manifest put happened without issue.
	seedRandomOCIImageIndex(t, env, repoPath, putByDigest)
}

func TestManifestAPI_Put_OCIImageIndexByTagManifestsNotPresentInDatabase(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	tagName := "ociindexmissingmanifeststag"
	repoPath := "ociindex/missingmanifests"

	// putRandomOCIImageIndex with putByTag tests that the manifest put happened without issue.
	deserializedManifest := seedRandomOCIImageIndex(t, env, repoPath, writeToFilesystemOnly)

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)

	resp := putManifest(t, "putting OCI image index missing manifests", tagURL, v1.MediaTypeImageIndex, deserializedManifest.ManifestList)
	defer resp.Body.Close()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func manifest_Get_OCIIndex_NonMatchingEtag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "ociindexhappypathtag"
	repoPath := "ociindex/happypath"

	deserializedManifest := seedRandomOCIImageIndex(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
		{
			name:        "by tag non matching etag",
			manifestURL: tagURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by digest non matching etag",
			manifestURL: digestURL,
			etag:        digest.FromString("no match").String(),
		},
		{
			name:        "by tag malformed etag",
			manifestURL: tagURL,
			etag:        "bad etag",
		},
		{
			name:        "by digest malformed etag",
			manifestURL: digestURL,
			etag:        "bad etag",
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", v1.MediaTypeImageIndex)
			if test.etag != "" {
				req.Header.Set("If-None-Match", test.etag)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
			require.Equal(t, fmt.Sprintf(`"%s"`, dgst), resp.Header.Get("ETag"))

			var fetchedManifest *manifestlist.DeserializedManifestList
			dec := json.NewDecoder(resp.Body)

			err = dec.Decode(&fetchedManifest)
			require.NoError(t, err)

			require.EqualValues(t, deserializedManifest, fetchedManifest)
		})
	}
}

func manifest_Get_OCIIndex_MatchingEtag(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "ociindexhappypathtag"
	repoPath := "ociindex/happypath"

	deserializedManifest := seedRandomOCIImageIndex(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
		etag        string
	}{
		{
			name:        "by tag quoted etag",
			manifestURL: tagURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by digest quoted etag",
			manifestURL: digestURL,
			etag:        fmt.Sprintf("%q", dgst),
		},
		{
			name:        "by tag non quoted etag",
			manifestURL: tagURL,
			etag:        dgst.String(),
		},
		{
			name:        "by digest non quoted etag",
			manifestURL: digestURL,
			etag:        dgst.String(),
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", v1.MediaTypeImageIndex)
			req.Header.Set("If-None-Match", test.etag)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusNotModified, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, http.NoBody, resp.Body)
		})
	}
}

func manifest_Get_ManifestList_FallbackToSchema2(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	tagName := "manifestlistfallbacktag"
	repoPath := "manifestlist/fallback"

	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByDigest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)
	dgst := digest.FromBytes(payload)

	manifestList := &manifestlist.ManifestList{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			// MediaType field for OCI image indexes is reserved to maintain compatibility and can be blank:
			// https://github.com/opencontainers/image-spec/blob/master/image-index.md#image-index-property-descriptions
			MediaType: "",
		},
		Manifests: []manifestlist.ManifestDescriptor{
			{
				Descriptor: distribution.Descriptor{
					Digest:    dgst,
					MediaType: schema2.MediaTypeManifest,
				},
				Platform: manifestlist.PlatformSpec{
					Architecture: "amd64",
					OS:           "linux",
				},
			},
		},
	}

	deserializedManifestList, err := manifestlist.FromDescriptors(manifestList.Manifests)
	require.NoError(t, err)

	manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifestList)
	manifestTagURL := buildManifestTagURL(t, env, repoPath, tagName)

	// Push up manifest list.
	resp := putManifest(t, "putting manifest list no error", manifestTagURL, manifestlist.MediaTypeManifestList, deserializedManifestList)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))

	_, payload, err = deserializedManifestList.Payload()
	require.NoError(t, err)

	dgst = digest.FromBytes(payload)
	require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))

	// Get manifest list with without avertising support for manifest lists.
	req, err := http.NewRequest("GET", manifestTagURL, nil)
	require.NoError(t, err)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var fetchedManifest *schema2.DeserializedManifest
	dec := json.NewDecoder(resp.Body)

	err = dec.Decode(&fetchedManifest)
	require.NoError(t, err)

	require.EqualValues(t, deserializedManifest, fetchedManifest)
}

func TestManifestAPI_Get_OCIIndexFromFilesystemAfterDatabaseWrites(t *testing.T) {
	env := newTestEnv(t)
	defer env.Shutdown()

	if !env.config.Database.Enabled {
		t.Skip("skipping test because the metadata database is not enabled")
	}

	tagName := "ociindexconsistentfstag"
	repoPath := "ociindex/consistenfs"

	deserializedManifest := seedRandomOCIImageIndex(t, env, repoPath, putByTag(tagName))

	// Build URLs.
	tagURL := buildManifestTagURL(t, env, repoPath, tagName)
	digestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	tt := []struct {
		name        string
		manifestURL string
	}{
		{
			name:        "by tag",
			manifestURL: tagURL,
		},
		{
			name:        "by digest",
			manifestURL: digestURL,
		},
	}

	// Disable the database to check that the filesystem mirroring worked correctly.
	env.config.Database.Enabled = false

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", test.manifestURL, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", v1.MediaTypeImageIndex)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
			require.Equal(t, fmt.Sprintf(`"%s"`, dgst), resp.Header.Get("ETag"))

			var fetchedManifest *manifestlist.DeserializedManifestList
			dec := json.NewDecoder(resp.Body)

			err = dec.Decode(&fetchedManifest)
			require.NoError(t, err)

			require.EqualValues(t, deserializedManifest, fetchedManifest)
		})
	}
}

func buildManifestTagURL(t testing.TB, env *testEnv, repoPath, tagName string) string {
	t.Helper()

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	tagRef, err := reference.WithTag(repoRef, tagName)
	require.NoError(t, err)

	tagURL, err := env.builder.BuildManifestURL(tagRef)
	require.NoError(t, err)

	return tagURL
}

func buildManifestDigestURL(t testing.TB, env *testEnv, repoPath string, manifest distribution.Manifest) string {
	t.Helper()

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	_, payload, err := manifest.Payload()
	require.NoError(t, err)

	dgst := digest.FromBytes(payload)

	digestRef, err := reference.WithDigest(repoRef, dgst)
	require.NoError(t, err)

	digestURL, err := env.builder.BuildManifestURL(digestRef)
	require.NoError(t, err)

	return digestURL
}

// TODO: Misc testing that's not currently covered by TestManifestAPI
// https://gitlab.com/gitlab-org/container-registry/-/issues/143
func TestManifestAPI_Get_UnknownSchema(t *testing.T) {}
func TestManifestAPI_Put_UnknownSchema(t *testing.T) {}

func TestManifestAPI_Get_UnknownMediaType(t *testing.T) {}
func TestManifestAPI_Put_UnknownMediaType(t *testing.T) {}

func TestManifestAPI_Put_ReuseTagManifestToManifestList(t *testing.T)     {}
func TestManifestAPI_Put_ReuseTagManifestListToManifest(t *testing.T)     {}
func TestManifestAPI_Put_ReuseTagManifestListToManifestList(t *testing.T) {}

func TestManifestAPI_Put_DigestReadOnly(t *testing.T) {}
func TestManifestAPI_Put_TagReadOnly(t *testing.T)    {}

type manifestOpts struct {
	manifestURL           string
	putManifest           bool
	writeToFilesystemOnly bool

	// Non-optional values which be passed through by the testing func for ease of use.
	repoPath string
}

type manifestOptsFunc func(testing.TB, *testEnv, *manifestOpts)

func putByTag(tagName string) manifestOptsFunc {
	return func(t testing.TB, env *testEnv, opts *manifestOpts) {
		opts.manifestURL = buildManifestTagURL(t, env, opts.repoPath, tagName)
		opts.putManifest = true
	}
}

func putByDigest(t testing.TB, env *testEnv, opts *manifestOpts) {
	opts.putManifest = true
}

func writeToFilesystemOnly(t testing.TB, env *testEnv, opts *manifestOpts) {
	require.True(t, env.config.Database.Enabled, "this option is only available when the database is enabled")

	opts.writeToFilesystemOnly = true
}

func schema2Config() ([]byte, distribution.Descriptor) {
	payload := []byte(`{
		"architecture": "amd64",
		"history": [
			{
				"created": "2015-10-31T22:22:54.690851953Z",
				"created_by": "/bin/sh -c #(nop) ADD file:a3bc1e842b69636f9df5256c49c5374fb4eef1e281fe3f282c65fb853ee171c5 in /"
			},
			{
				"created": "2015-10-31T22:22:55.613815829Z",
				"created_by": "/bin/sh -c #(nop) CMD [\"sh\"]"
			}
		],
		"rootfs": {
			"diff_ids": [
				"sha256:c6f988f4874bb0add23a778f753c65efe992244e148a1d2ec2a8b664fb66bbd1",
				"sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef"
			],
			"type": "layers"
		}
	}`)

	return payload, distribution.Descriptor{
		Size:      int64(len(payload)),
		MediaType: schema2.MediaTypeImageConfig,
		Digest:    digest.FromBytes(payload),
	}
}

// seedRandomSchema2Manifest generates a random schema2 manifest and puts its config and layers.
func seedRandomSchema2Manifest(t testing.TB, env *testEnv, repoPath string, opts ...manifestOptsFunc) *schema2.DeserializedManifest {
	t.Helper()

	config := &manifestOpts{
		repoPath: repoPath,
	}

	for _, o := range opts {
		o(t, env, config)
	}

	if config.writeToFilesystemOnly {
		env.config.Database.Enabled = false
		defer func() { env.config.Database.Enabled = true }()
	}

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	manifest := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
	}

	// Create a manifest config and push up its content.
	cfgPayload, cfgDesc := schema2Config()
	uploadURLBase, _ := startPushLayer(t, env, repoRef)
	pushLayer(t, env.builder, repoRef, cfgDesc.Digest, uploadURLBase, bytes.NewReader(cfgPayload))
	manifest.Config = cfgDesc

	// Create and push up 2 random layers.
	manifest.Layers = make([]distribution.Descriptor, 2)

	for i := range manifest.Layers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, repoRef)
		pushLayer(t, env.builder, repoRef, dgst, uploadURLBase, rs)

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: schema2.MediaTypeLayer,
		}
	}

	deserializedManifest, err := schema2.FromStruct(*manifest)
	require.NoError(t, err)

	if config.putManifest {
		manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

		if config.manifestURL == "" {
			config.manifestURL = manifestDigestURL
		}

		resp := putManifest(t, "putting manifest no error", config.manifestURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))

		_, payload, err := deserializedManifest.Payload()
		require.NoError(t, err)
		dgst := digest.FromBytes(payload)
		require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
	}

	return deserializedManifest
}

func ociConfig() ([]byte, distribution.Descriptor) {
	payload := []byte(`{
    "created": "2015-10-31T22:22:56.015925234Z",
    "author": "Alyssa P. Hacker <alyspdev@example.com>",
    "architecture": "amd64",
    "os": "linux",
    "config": {
        "User": "alice",
        "ExposedPorts": {
            "8080/tcp": {}
        },
        "Env": [
            "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
            "FOO=oci_is_a",
            "BAR=well_written_spec"
        ],
        "Entrypoint": [
            "/bin/my-app-binary"
        ],
        "Cmd": [
            "--foreground",
            "--config",
            "/etc/my-app.d/default.cfg"
        ],
        "Volumes": {
            "/var/job-result-data": {},
            "/var/log/my-app-logs": {}
        },
        "WorkingDir": "/home/alice",
        "Labels": {
            "com.example.project.git.url": "https://example.com/project.git",
            "com.example.project.git.commit": "45a939b2999782a3f005621a8d0f29aa387e1d6b"
        }
    },
    "rootfs": {
      "diff_ids": [
        "sha256:c6f988f4874bb0add23a778f753c65efe992244e148a1d2ec2a8b664fb66bbd1",
        "sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef"
      ],
      "type": "layers"
    },
    "history": [
      {
        "created": "2015-10-31T22:22:54.690851953Z",
        "created_by": "/bin/sh -c #(nop) ADD file:a3bc1e842b69636f9df5256c49c5374fb4eef1e281fe3f282c65fb853ee171c5 in /"
      },
      {
        "created": "2015-10-31T22:22:55.613815829Z",
        "created_by": "/bin/sh -c #(nop) CMD [\"sh\"]",
        "empty_layer": true
      }
    ]
}`)

	return payload, distribution.Descriptor{
		Size:      int64(len(payload)),
		MediaType: v1.MediaTypeImageConfig,
		Digest:    digest.FromBytes(payload),
	}
}

// seedRandomOCIManifest generates a random oci manifest and puts its config and layers.
func seedRandomOCIManifest(t testing.TB, env *testEnv, repoPath string, opts ...manifestOptsFunc) *ocischema.DeserializedManifest {
	t.Helper()

	config := &manifestOpts{
		repoPath: repoPath,
	}

	for _, o := range opts {
		o(t, env, config)
	}

	repoRef, err := reference.WithName(repoPath)
	require.NoError(t, err)

	manifest := &ocischema.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     v1.MediaTypeImageManifest,
		},
	}

	// Create a manifest config and push up its content.
	cfgPayload, cfgDesc := ociConfig()
	uploadURLBase, _ := startPushLayer(t, env, repoRef)
	pushLayer(t, env.builder, repoRef, cfgDesc.Digest, uploadURLBase, bytes.NewReader(cfgPayload))
	manifest.Config = cfgDesc

	// Create and push up 2 random layers.
	manifest.Layers = make([]distribution.Descriptor, 2)

	for i := range manifest.Layers {
		rs, dgstStr, err := testutil.CreateRandomTarFile()
		require.NoError(t, err)

		dgst := digest.Digest(dgstStr)

		uploadURLBase, _ := startPushLayer(t, env, repoRef)
		pushLayer(t, env.builder, repoRef, dgst, uploadURLBase, rs)

		manifest.Layers[i] = distribution.Descriptor{
			Digest:    dgst,
			MediaType: v1.MediaTypeImageLayer,
		}
	}

	deserializedManifest, err := ocischema.FromStruct(*manifest)
	require.NoError(t, err)

	if config.putManifest {
		manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

		if config.manifestURL == "" {
			config.manifestURL = manifestDigestURL
		}

		resp := putManifest(t, "putting manifest no error", config.manifestURL, v1.MediaTypeImageManifest, deserializedManifest)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))

		_, payload, err := deserializedManifest.Payload()
		require.NoError(t, err)
		dgst := digest.FromBytes(payload)
		require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
	}

	return deserializedManifest
}

// randomPlatformSpec generates a random platfromSpec. Arch and OS combinations
// may not strictly be valid for the Go runtime.
func randomPlatformSpec() manifestlist.PlatformSpec {
	rand.Seed(time.Now().Unix())

	architectures := []string{"amd64", "arm64", "ppc64le", "mips64", "386"}
	oses := []string{"aix", "darwin", "linux", "freebsd", "plan9"}

	return manifestlist.PlatformSpec{
		Architecture: architectures[rand.Intn(len(architectures))],
		OS:           oses[rand.Intn(len(oses))],
		// Optional values.
		OSVersion:  "",
		OSFeatures: nil,
		Variant:    "",
		Features:   nil,
	}
}

// seedRandomOCIImageIndex generates a random oci image index and puts its images.
func seedRandomOCIImageIndex(t *testing.T, env *testEnv, repoPath string, opts ...manifestOptsFunc) *manifestlist.DeserializedManifestList {
	t.Helper()

	config := &manifestOpts{
		repoPath: repoPath,
	}

	for _, o := range opts {
		o(t, env, config)
	}

	if config.writeToFilesystemOnly {
		env.config.Database.Enabled = false
		defer func() { env.config.Database.Enabled = true }()
	}

	ociImageIndex := &manifestlist.ManifestList{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			// MediaType field for OCI image indexes is reserved to maintain compatibility and can be blank:
			// https://github.com/opencontainers/image-spec/blob/master/image-index.md#image-index-property-descriptions
			MediaType: "",
		},
	}

	// Create and push up 2 random OCI images.
	ociImageIndex.Manifests = make([]manifestlist.ManifestDescriptor, 2)

	for i := range ociImageIndex.Manifests {
		deserializedManifest := seedRandomOCIManifest(t, env, repoPath, putByDigest)

		_, payload, err := deserializedManifest.Payload()
		require.NoError(t, err)
		dgst := digest.FromBytes(payload)

		ociImageIndex.Manifests[i] = manifestlist.ManifestDescriptor{
			Descriptor: distribution.Descriptor{
				Digest:    dgst,
				MediaType: v1.MediaTypeImageManifest,
			},
			Platform: randomPlatformSpec(),
		}
	}

	deserializedManifest, err := manifestlist.FromDescriptors(ociImageIndex.Manifests)
	require.NoError(t, err)

	if config.putManifest {
		manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

		if config.manifestURL == "" {
			config.manifestURL = manifestDigestURL
		}

		resp := putManifest(t, "putting oci image index no error", config.manifestURL, v1.MediaTypeImageIndex, deserializedManifest)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))

		_, payload, err := deserializedManifest.Payload()
		require.NoError(t, err)
		dgst := digest.FromBytes(payload)
		require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
	}

	return deserializedManifest
}

func testManifestAPIManifestList(t *testing.T, env *testEnv, args manifestArgs) {
	imageName := args.imageName
	tag := "manifestlisttag"

	tagRef, _ := reference.WithTag(imageName, tag)
	manifestURL, err := env.builder.BuildManifestURL(tagRef)
	if err != nil {
		t.Fatalf("unexpected error getting manifest url: %v", err)
	}

	// --------------------------------
	// Attempt to push manifest list that refers to an unknown manifest
	manifestList := &manifestlist.ManifestList{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     manifestlist.MediaTypeManifestList,
		},
		Manifests: []manifestlist.ManifestDescriptor{
			{
				Descriptor: distribution.Descriptor{
					Digest:    "sha256:1a9ec845ee94c202b2d5da74a24f0ed2058318bfa9879fa541efaecba272e86b",
					Size:      3253,
					MediaType: schema2.MediaTypeManifest,
				},
				Platform: manifestlist.PlatformSpec{
					Architecture: "amd64",
					OS:           "linux",
				},
			},
		},
	}

	resp := putManifest(t, "putting missing manifest manifestlist", manifestURL, manifestlist.MediaTypeManifestList, manifestList)
	defer resp.Body.Close()
	checkResponse(t, "putting missing manifest manifestlist", resp, http.StatusBadRequest)
	_, p, counts := checkBodyHasErrorCodes(t, "putting missing manifest manifestlist", resp, v2.ErrorCodeManifestBlobUnknown)

	expectedCounts := map[errcode.ErrorCode]int{
		v2.ErrorCodeManifestBlobUnknown: 1,
	}

	if !reflect.DeepEqual(counts, expectedCounts) {
		t.Fatalf("unexpected number of error codes encountered: %v\n!=\n%v\n---\n%s", counts, expectedCounts, string(p))
	}

	// -------------------
	// Push a manifest list that references an actual manifest
	manifestList.Manifests[0].Digest = args.dgst
	deserializedManifestList, err := manifestlist.FromDescriptors(manifestList.Manifests)
	if err != nil {
		t.Fatalf("could not create DeserializedManifestList: %v", err)
	}
	_, canonical, err := deserializedManifestList.Payload()
	if err != nil {
		t.Fatalf("could not get manifest list payload: %v", err)
	}
	dgst := digest.FromBytes(canonical)

	digestRef, _ := reference.WithDigest(imageName, dgst)
	manifestDigestURL, err := env.builder.BuildManifestURL(digestRef)
	checkErr(t, err, "building manifest url")

	resp = putManifest(t, "putting manifest list no error", manifestURL, manifestlist.MediaTypeManifestList, deserializedManifestList)
	checkResponse(t, "putting manifest list no error", resp, http.StatusCreated)
	checkHeaders(t, resp, http.Header{
		"Location":              []string{manifestDigestURL},
		"Docker-Content-Digest": []string{dgst.String()},
	})

	// --------------------
	// Push by digest -- should get same result
	resp = putManifest(t, "putting manifest list by digest", manifestDigestURL, manifestlist.MediaTypeManifestList, deserializedManifestList)
	checkResponse(t, "putting manifest list by digest", resp, http.StatusCreated)
	checkHeaders(t, resp, http.Header{
		"Location":              []string{manifestDigestURL},
		"Docker-Content-Digest": []string{dgst.String()},
	})

	// ------------------
	// Fetch by tag name
	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}
	// multiple headers in mixed list format to ensure we parse correctly server-side
	req.Header.Set("Accept", fmt.Sprintf(` %s ; q=0.8 , %s ; q=0.5 `, manifestlist.MediaTypeManifestList, v1.MediaTypeImageManifest))
	req.Header.Add("Accept", schema2.MediaTypeManifest)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error fetching manifest list: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, "fetching uploaded manifest list", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Docker-Content-Digest": []string{dgst.String()},
		"ETag":                  []string{fmt.Sprintf(`"%s"`, dgst)},
	})

	var fetchedManifestList manifestlist.DeserializedManifestList
	dec := json.NewDecoder(resp.Body)

	if err := dec.Decode(&fetchedManifestList); err != nil {
		t.Fatalf("error decoding fetched manifest list: %v", err)
	}

	_, fetchedCanonical, err := fetchedManifestList.Payload()
	if err != nil {
		t.Fatalf("error getting manifest list payload: %v", err)
	}

	if !bytes.Equal(fetchedCanonical, canonical) {
		t.Fatalf("manifest lists do not match")
	}

	// ---------------
	// Fetch by digest
	req, err = http.NewRequest("GET", manifestDigestURL, nil)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}
	req.Header.Set("Accept", manifestlist.MediaTypeManifestList)
	resp, err = http.DefaultClient.Do(req)
	checkErr(t, err, "fetching manifest list by digest")
	defer resp.Body.Close()

	checkResponse(t, "fetching uploaded manifest list", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Docker-Content-Digest": []string{dgst.String()},
		"ETag":                  []string{fmt.Sprintf(`"%s"`, dgst)},
	})

	var fetchedManifestListByDigest manifestlist.DeserializedManifestList
	dec = json.NewDecoder(resp.Body)
	if err := dec.Decode(&fetchedManifestListByDigest); err != nil {
		t.Fatalf("error decoding fetched manifest: %v", err)
	}

	_, fetchedCanonical, err = fetchedManifestListByDigest.Payload()
	if err != nil {
		t.Fatalf("error getting manifest list payload: %v", err)
	}

	if !bytes.Equal(fetchedCanonical, canonical) {
		t.Fatalf("manifests do not match")
	}

	// Get by name with etag, gives 304
	etag := resp.Header.Get("Etag")
	req, err = http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}
	req.Header.Set("If-None-Match", etag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}

	checkResponse(t, "fetching manifest by name with etag", resp, http.StatusNotModified)

	// Get by digest with etag, gives 304
	req, err = http.NewRequest("GET", manifestDigestURL, nil)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}
	req.Header.Set("If-None-Match", etag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Error constructing request: %s", err)
	}

	checkResponse(t, "fetching manifest by dgst with etag", resp, http.StatusNotModified)
}

func testManifestDelete(t *testing.T, env *testEnv, args manifestArgs) {
	imageName := args.imageName
	dgst := args.dgst
	manifest := args.manifest

	ref, _ := reference.WithDigest(imageName, dgst)
	manifestDigestURL, _ := env.builder.BuildManifestURL(ref)
	// ---------------
	// Delete by digest
	resp, err := httpDelete(manifestDigestURL)
	checkErr(t, err, "deleting manifest by digest")

	checkResponse(t, "deleting manifest", resp, http.StatusAccepted)
	checkHeaders(t, resp, http.Header{
		"Content-Length": []string{"0"},
	})

	// ---------------
	// Attempt to fetch deleted manifest
	resp, err = http.Get(manifestDigestURL)
	checkErr(t, err, "fetching deleted manifest by digest")
	defer resp.Body.Close()

	checkResponse(t, "fetching deleted manifest", resp, http.StatusNotFound)

	// ---------------
	// Delete already deleted manifest by digest
	resp, err = httpDelete(manifestDigestURL)
	checkErr(t, err, "re-deleting manifest by digest")

	checkResponse(t, "re-deleting manifest", resp, http.StatusNotFound)

	// --------------------
	// Re-upload manifest by digest
	resp = putManifest(t, "putting manifest", manifestDigestURL, args.mediaType, manifest)
	checkResponse(t, "putting manifest", resp, http.StatusCreated)
	checkHeaders(t, resp, http.Header{
		"Location":              []string{manifestDigestURL},
		"Docker-Content-Digest": []string{dgst.String()},
	})

	// ---------------
	// Attempt to fetch re-uploaded deleted digest
	resp, err = http.Get(manifestDigestURL)
	checkErr(t, err, "fetching re-uploaded manifest by digest")
	defer resp.Body.Close()

	checkResponse(t, "fetching re-uploaded manifest", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Docker-Content-Digest": []string{dgst.String()},
	})

	// ---------------
	// Attempt to delete an unknown manifest
	unknownDigest := digest.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	unknownRef, _ := reference.WithDigest(imageName, unknownDigest)
	unknownManifestDigestURL, err := env.builder.BuildManifestURL(unknownRef)
	checkErr(t, err, "building unknown manifest url")

	resp, err = httpDelete(unknownManifestDigestURL)
	checkErr(t, err, "delting unknown manifest by digest")
	checkResponse(t, "fetching deleted manifest", resp, http.StatusNotFound)

	// --------------------
	// Upload manifest by tag
	tag := "atag"
	tagRef, _ := reference.WithTag(imageName, tag)
	manifestTagURL, _ := env.builder.BuildManifestURL(tagRef)
	resp = putManifest(t, "putting manifest by tag", manifestTagURL, args.mediaType, manifest)
	checkResponse(t, "putting manifest by tag", resp, http.StatusCreated)
	checkHeaders(t, resp, http.Header{
		"Location":              []string{manifestDigestURL},
		"Docker-Content-Digest": []string{dgst.String()},
	})

	tagsURL, err := env.builder.BuildTagsURL(imageName)
	if err != nil {
		t.Fatalf("unexpected error building tags url: %v", err)
	}

	// Ensure that the tag is listed.
	resp, err = http.Get(tagsURL)
	if err != nil {
		t.Fatalf("unexpected error getting unknown tags: %v", err)
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var tagsResponse tagsAPIResponse
	if err := dec.Decode(&tagsResponse); err != nil {
		t.Fatalf("unexpected error decoding error response: %v", err)
	}

	if tagsResponse.Name != imageName.Name() {
		t.Fatalf("tags name should match image name: %v != %v", tagsResponse.Name, imageName)
	}

	if len(tagsResponse.Tags) != 1 {
		t.Fatalf("expected some tags in response: %v", tagsResponse.Tags)
	}

	if tagsResponse.Tags[0] != tag {
		t.Fatalf("tag not as expected: %q != %q", tagsResponse.Tags[0], tag)
	}

	// ---------------
	// Delete by digest
	resp, err = httpDelete(manifestDigestURL)
	checkErr(t, err, "deleting manifest by digest")

	checkResponse(t, "deleting manifest with tag", resp, http.StatusAccepted)
	checkHeaders(t, resp, http.Header{
		"Content-Length": []string{"0"},
	})

	// Ensure that the tag is not listed.
	resp, err = http.Get(tagsURL)
	if err != nil {
		t.Fatalf("unexpected error getting unknown tags: %v", err)
	}
	defer resp.Body.Close()

	dec = json.NewDecoder(resp.Body)
	if err := dec.Decode(&tagsResponse); err != nil {
		t.Fatalf("unexpected error decoding error response: %v", err)
	}

	if tagsResponse.Name != imageName.Name() {
		t.Fatalf("tags name should match image name: %v != %v", tagsResponse.Name, imageName)
	}

	if len(tagsResponse.Tags) != 0 {
		t.Fatalf("expected 0 tags in response: %v", tagsResponse.Tags)
	}
}

func shuffledCopy(s []string) []string {
	shuffled := make([]string, len(s))
	copy(shuffled, s)
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return shuffled
}

func tags_Get(t *testing.T, opts ...configOpt) {
	opts = append(opts)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	require.NoError(t, err)

	sortedTags := []string{
		"2j2ar",
		"asj9e",
		"dcsl6",
		"hpgkt",
		"jyi7b",
		"jyi7b-fxt1v",
		"jyi7b-sgv2q",
		"kb0j5",
		"n343n",
		"sb71y",
	}

	// shuffle tags to make sure results are consistent regardless of creation order (it matters when running
	// against the metadata database)
	shuffledTags := shuffledCopy(sortedTags)

	createRepositoryWithMultipleIdenticalTags(t, env, imageName.Name(), shuffledTags)

	tt := []struct {
		name                string
		runWithoutDBEnabled bool
		queryParams         url.Values
		expectedBody        tagsAPIResponse
		expectedLinkHeader  string
	}{
		{
			name:                "no query parameters",
			expectedBody:        tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags},
			runWithoutDBEnabled: true,
		},
		{
			name:         "empty last query parameter",
			queryParams:  url.Values{"last": []string{""}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags},
		},
		{
			name:         "empty n query parameter",
			queryParams:  url.Values{"n": []string{""}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags},
		},
		{
			name:         "empty last and n query parameters",
			queryParams:  url.Values{"last": []string{""}, "n": []string{""}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags},
		},
		{
			name:         "non integer n query parameter",
			queryParams:  url.Values{"n": []string{"foo"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags},
		},
		{
			name:        "1st page",
			queryParams: url.Values{"n": []string{"4"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: []string{
				"2j2ar",
				"asj9e",
				"dcsl6",
				"hpgkt",
			}},
			expectedLinkHeader: `</v2/foo/bar/tags/list?last=hpgkt&n=4>; rel="next"`,
		},
		{
			name:        "nth page",
			queryParams: url.Values{"last": []string{"hpgkt"}, "n": []string{"4"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: []string{
				"jyi7b",
				"jyi7b-fxt1v",
				"jyi7b-sgv2q",
				"kb0j5",
			}},
			expectedLinkHeader: `</v2/foo/bar/tags/list?last=kb0j5&n=4>; rel="next"`,
		},
		{
			name:        "last page",
			queryParams: url.Values{"last": []string{"kb0j5"}, "n": []string{"4"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: []string{
				"n343n",
				"sb71y",
			}},
		},
		{
			name:         "zero page size",
			queryParams:  url.Values{"n": []string{"0"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags},
		},
		{
			name:         "page size bigger than full list",
			queryParams:  url.Values{"n": []string{"100"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags},
		},
		{
			name:        "after marker",
			queryParams: url.Values{"last": []string{"kb0j5/pic0i"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: []string{
				"n343n",
				"sb71y",
			}},
		},
		{
			name:        "after non existent marker",
			queryParams: url.Values{"last": []string{"does-not-exist"}},
			expectedBody: tagsAPIResponse{Name: imageName.Name(), Tags: []string{
				"hpgkt",
				"jyi7b",
				"jyi7b-fxt1v",
				"jyi7b-sgv2q",
				"kb0j5",
				"n343n",
				"sb71y",
			}},
		},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			if !test.runWithoutDBEnabled && !env.config.Database.Enabled {
				t.Skip("skipping test because the metadata database is not enabled")
			}

			tagsURL, err := env.builder.BuildTagsURL(imageName, test.queryParams)
			require.NoError(t, err)

			resp, err := http.Get(tagsURL)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode)

			var body tagsAPIResponse
			dec := json.NewDecoder(resp.Body)
			err = dec.Decode(&body)
			require.NoError(t, err)

			require.Equal(t, test.expectedBody, body)
			require.Equal(t, test.expectedLinkHeader, resp.Header.Get("Link"))
		})
	}

	// If the database is enabled, disable it and rerun the tests again with the
	// database to check that the filesystem mirroring worked correctly.
	// All results should be the full list as the filesytem does not support pagination.
	if env.config.Database.Enabled && !env.config.Migration.DisableMirrorFS {
		env.config.Database.Enabled = false
		defer func() { env.config.Database.Enabled = true }()

		for _, test := range tt {
			t.Run(fmt.Sprintf("%s filesystem mirroring", test.name), func(t *testing.T) {
				tagsURL, err := env.builder.BuildTagsURL(imageName, test.queryParams)
				require.NoError(t, err)

				resp, err := http.Get(tagsURL)
				require.NoError(t, err)
				defer resp.Body.Close()

				require.Equal(t, http.StatusOK, resp.StatusCode)

				var body tagsAPIResponse
				dec := json.NewDecoder(resp.Body)
				err = dec.Decode(&body)
				require.NoError(t, err)

				require.Equal(t, tagsAPIResponse{Name: imageName.Name(), Tags: sortedTags}, body)
			})
		}
	}
}

func tags_Get_RepositoryNotFound(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	require.NoError(t, err)

	tagsURL, err := env.builder.BuildTagsURL(imageName)
	require.NoError(t, err)

	resp, err := http.Get(tagsURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Empty(t, resp.Header.Get("Link"))
	checkBodyHasErrorCodes(t, "repository not found", resp, v2.ErrorCodeNameUnknown)
}

func tags_Get_EmptyRepository(t *testing.T, opts ...configOpt) {
	opts = append(opts)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	require.NoError(t, err)

	// SETUP

	// create repository and then delete its only tag
	tag := "latest"
	createRepository(t, env, imageName.Name(), tag)

	ref, err := reference.WithTag(imageName, tag)
	require.NoError(t, err)

	tagURL, err := env.builder.BuildTagURL(ref)
	require.NoError(t, err)

	res, err := httpDelete(tagURL)
	require.NoError(t, err)
	defer res.Body.Close()

	require.Equal(t, http.StatusAccepted, res.StatusCode)

	// TEST

	tagsURL, err := env.builder.BuildTagsURL(imageName)
	require.NoError(t, err)

	resp, err := http.Get(tagsURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	var body tagsAPIResponse
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, resp.Header.Get("Link"))
	require.Equal(t, tagsAPIResponse{Name: imageName.Name()}, body)
}

func tags_Delete_AllowedMethods(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	checkErr(t, err, "building named object")

	ref, err := reference.WithTag(imageName, "latest")
	checkErr(t, err, "building tag reference")

	tagURL, err := env.builder.BuildTagURL(ref)
	checkErr(t, err, "building tag URL")

	checkAllowedMethods(t, tagURL, []string{"DELETE"})
}

func tags_Delete_AllowedMethodsReadOnly(t *testing.T, opts ...configOpt) {
	opts = append(opts, withReadOnly)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	checkErr(t, err, "building named object")

	ref, err := reference.WithTag(imageName, "latest")
	checkErr(t, err, "building tag reference")

	tagURL, err := env.builder.BuildTagURL(ref)
	checkErr(t, err, "building tag URL")

	resp, err := httpOptions(tagURL)
	msg := "checking allowed methods"
	checkErr(t, err, msg)

	defer resp.Body.Close()

	checkResponse(t, msg, resp, http.StatusOK)
	if resp.Header.Get("Allow") != "" {
		t.Fatal("unexpected Allow header")
	}
}

func tags_Delete(t *testing.T, opts ...configOpt) {
	opts = append(opts)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	checkErr(t, err, "building named object")

	tag := "latest"
	createRepository(t, env, imageName.Name(), tag)

	ref, err := reference.WithTag(imageName, tag)
	checkErr(t, err, "building tag reference")

	tagURL, err := env.builder.BuildTagURL(ref)
	checkErr(t, err, "building tag URL")

	resp, err := httpDelete(tagURL)
	msg := "checking tag delete"
	checkErr(t, err, msg)

	defer resp.Body.Close()

	checkResponse(t, msg, resp, http.StatusAccepted)

	if resp.Body != http.NoBody {
		t.Fatalf("unexpected response body")
	}
}

func tags_Delete_Unknown(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	// Push up a random manifest to ensure that the repository exists.
	seedRandomSchema2Manifest(t, env, "foo/bar", putByDigest)

	imageName, err := reference.WithName("foo/bar")
	checkErr(t, err, "building named object")

	ref, err := reference.WithTag(imageName, "latest")
	checkErr(t, err, "building tag reference")

	tagURL, err := env.builder.BuildTagURL(ref)
	checkErr(t, err, "building tag URL")

	resp, err := httpDelete(tagURL)
	msg := "checking unknown tag delete"
	checkErr(t, err, msg)

	defer resp.Body.Close()

	checkResponse(t, msg, resp, http.StatusNotFound)
	checkBodyHasErrorCodes(t, msg, resp, v2.ErrorCodeManifestUnknown)
}

func tags_Delete_UnknownRepository(t *testing.T, opts ...configOpt) {
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	require.NoError(t, err)

	ref, err := reference.WithTag(imageName, "latest")
	require.NoError(t, err)

	tagURL, err := env.builder.BuildTagURL(ref)
	require.NoError(t, err)

	resp, err := httpDelete(tagURL)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	checkBodyHasErrorCodes(t, "repository not found", resp, v2.ErrorCodeNameUnknown)
}

func tags_Delete_ReadOnly(t *testing.T, opts ...configOpt) {
	opts = append(opts, withSharedInMemoryDriver("TestTagsAPITagDeleteReadOnly"))
	setupEnv := newTestEnv(t, opts...)
	defer setupEnv.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	checkErr(t, err, "building named object")

	tag := "latest"
	createRepository(t, setupEnv, imageName.Name(), tag)

	// Reconfigure environment with withReadOnly enabled.
	setupEnv.Shutdown()
	opts = append(opts, withReadOnly)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	ref, err := reference.WithTag(imageName, tag)
	checkErr(t, err, "building tag reference")

	tagURL, err := env.builder.BuildTagURL(ref)
	checkErr(t, err, "building tag URL")

	resp, err := httpDelete(tagURL)
	msg := "checking tag delete"
	checkErr(t, err, msg)

	defer resp.Body.Close()

	checkResponse(t, msg, resp, http.StatusMethodNotAllowed)
}

// TestTagsAPITagDeleteWithSameImageID tests that deleting a single image tag will not cause the deletion of other tags
// pointing to the same image ID.
func tags_Delete_WithSameImageID(t *testing.T, opts ...configOpt) {
	opts = append(opts)
	env := newTestEnv(t, opts...)
	defer env.Shutdown()

	imageName, err := reference.WithName("foo/bar")
	checkErr(t, err, "building named object")

	// build two tags pointing to the same image
	tag1 := "1.0.0"
	tag2 := "latest"
	createRepositoryWithMultipleIdenticalTags(t, env, imageName.Name(), []string{tag1, tag2})

	// delete one of the tags
	ref, err := reference.WithTag(imageName, tag1)
	checkErr(t, err, "building tag reference")

	tagURL, err := env.builder.BuildTagURL(ref)
	checkErr(t, err, "building tag URL")

	resp, err := httpDelete(tagURL)
	msg := "checking tag delete"
	checkErr(t, err, msg)

	defer resp.Body.Close()

	checkResponse(t, msg, resp, http.StatusAccepted)

	// check the other tag is still there
	tagsURL, err := env.builder.BuildTagsURL(imageName)
	if err != nil {
		t.Fatalf("unexpected error building tags url: %v", err)
	}
	resp, err = http.Get(tagsURL)
	if err != nil {
		t.Fatalf("unexpected error getting tags: %v", err)
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var tagsResponse tagsAPIResponse
	if err := dec.Decode(&tagsResponse); err != nil {
		t.Fatalf("unexpected error decoding response: %v", err)
	}

	if tagsResponse.Name != imageName.Name() {
		t.Fatalf("tags name should match image name: %v != %v", tagsResponse.Name, imageName)
	}

	if len(tagsResponse.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d: %v", len(tagsResponse.Tags), tagsResponse.Tags)
	}

	if tagsResponse.Tags[0] != tag2 {
		t.Fatalf("expected tag to be %q, got %q", tagsResponse.Tags[0], tag2)
	}
}

type testEnv struct {
	pk      libtrust.PrivateKey
	ctx     context.Context
	config  *configuration.Configuration
	app     *registryhandlers.App
	server  *httptest.Server
	builder *v2.URLBuilder
	db      *datastore.DB
}

func newTestEnvMirror(t *testing.T, opts ...configOpt) *testEnv {
	config := newConfig(opts...)
	config.Proxy.RemoteURL = "http://example.com"

	return newTestEnvWithConfig(t, &config)
}

func newTestEnv(t testing.TB, opts ...configOpt) *testEnv {
	config := newConfig(opts...)

	return newTestEnvWithConfig(t, &config)
}

func newTestEnvWithConfig(t testing.TB, config *configuration.Configuration) *testEnv {
	ctx := context.Background()

	// The API test needs access to the database only to clean it up during
	// shutdown so that environments come up with a fresh copy of the database.
	var db *datastore.DB
	var err error
	if config.Database.Enabled {
		db, err = dbtestutil.NewDBFromConfig(config)
		if err != nil {
			t.Fatal(err)
		}
		m := migrations.NewMigrator(db.DB)
		if _, err = m.Up(); err != nil {
			t.Fatal(err)
		}
	}

	app := registryhandlers.NewApp(ctx, config)

	var out io.Writer
	if config.Log.AccessLog.Disabled {
		out = ioutil.Discard
	} else {
		out = os.Stderr
	}
	server := httptest.NewServer(handlers.CombinedLoggingHandler(out, app))
	builder, err := v2.NewURLBuilderFromString(server.URL+config.HTTP.Prefix, false)

	if err != nil {
		t.Fatalf("error creating url builder: %v", err)
	}

	pk, err := libtrust.GenerateECP256PrivateKey()
	if err != nil {
		t.Fatalf("unexpected error generating private key: %v", err)
	}

	return &testEnv{
		pk:      pk,
		ctx:     ctx,
		config:  config,
		app:     app,
		server:  server,
		builder: builder,
		db:      db,
	}
}

func (t *testEnv) Shutdown() {
	t.server.CloseClientConnections()
	t.server.Close()

	if t.config.Database.Enabled {
		if err := t.app.GracefulShutdown(t.ctx); err != nil {
			panic(err)
		}

		if err := dbtestutil.TruncateAllTables(t.db); err != nil {
			panic(err)
		}

		if err := t.db.Close(); err != nil {
			panic(err)
		}

		// Needed for idempotency, so that shutdowns may be defer'd without worry.
		t.config.Database.Enabled = false
	}
}

func putManifest(t testing.TB, msg, url, contentType string, v interface{}) *http.Response {
	var body []byte

	switch m := v.(type) {
	case *schema1.SignedManifest:
		_, pl, err := m.Payload()
		if err != nil {
			t.Fatalf("error getting payload: %v", err)
		}
		body = pl
	case *manifestlist.DeserializedManifestList:
		_, pl, err := m.Payload()
		if err != nil {
			t.Fatalf("error getting payload: %v", err)
		}
		body = pl
	default:
		var err error
		body, err = json.MarshalIndent(v, "", "   ")
		if err != nil {
			t.Fatalf("unexpected error marshaling %v: %v", v, err)
		}
	}

	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("error creating request for %s: %v", msg, err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("error doing put request while %s: %v", msg, err)
	}

	return resp
}

func startPushLayer(t testing.TB, env *testEnv, name reference.Named) (location string, uuid string) {
	layerUploadURL, err := env.builder.BuildBlobUploadURL(name)
	if err != nil {
		t.Fatalf("unexpected error building layer upload url: %v", err)
	}

	u, err := url.Parse(layerUploadURL)
	if err != nil {
		t.Fatalf("error parsing layer upload URL: %v", err)
	}

	base, err := url.Parse(env.server.URL)
	if err != nil {
		t.Fatalf("error parsing server URL: %v", err)
	}

	layerUploadURL = base.ResolveReference(u).String()
	resp, err := http.Post(layerUploadURL, "", nil)
	if err != nil {
		t.Fatalf("unexpected error starting layer push: %v", err)
	}

	defer resp.Body.Close()

	checkResponse(t, fmt.Sprintf("pushing starting layer push %v", name.String()), resp, http.StatusAccepted)

	u, err = url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("error parsing location header: %v", err)
	}

	uuid = path.Base(u.Path)
	checkHeaders(t, resp, http.Header{
		"Location":           []string{"*"},
		"Content-Length":     []string{"0"},
		"Docker-Upload-UUID": []string{uuid},
	})

	return resp.Header.Get("Location"), uuid
}

// doPushLayer pushes the layer content returning the url on success returning
// the response. If you're only expecting a successful response, use pushLayer.
func doPushLayer(t testing.TB, ub *v2.URLBuilder, name reference.Named, dgst digest.Digest, uploadURLBase string, body io.Reader) (*http.Response, error) {
	u, err := url.Parse(uploadURLBase)
	if err != nil {
		t.Fatalf("unexpected error parsing pushLayer url: %v", err)
	}

	u.RawQuery = url.Values{
		"_state": u.Query()["_state"],
		"digest": []string{dgst.String()},
	}.Encode()

	uploadURL := u.String()

	// Just do a monolithic upload
	req, err := http.NewRequest("PUT", uploadURL, body)
	if err != nil {
		t.Fatalf("unexpected error creating new request: %v", err)
	}

	return http.DefaultClient.Do(req)
}

// pushLayer pushes the layer content returning the url on success.
func pushLayer(t testing.TB, ub *v2.URLBuilder, name reference.Named, dgst digest.Digest, uploadURLBase string, body io.Reader) string {
	digester := digest.Canonical.Digester()

	resp, err := doPushLayer(t, ub, name, dgst, uploadURLBase, io.TeeReader(body, digester.Hash()))
	if err != nil {
		t.Fatalf("unexpected error doing push layer request: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, "putting monolithic chunk", resp, http.StatusCreated)

	if err != nil {
		t.Fatalf("error generating sha256 digest of body")
	}

	sha256Dgst := digester.Digest()

	ref, _ := reference.WithDigest(name, sha256Dgst)
	expectedLayerURL, err := ub.BuildBlobURL(ref)
	if err != nil {
		t.Fatalf("error building expected layer url: %v", err)
	}

	checkHeaders(t, resp, http.Header{
		"Location":              []string{expectedLayerURL},
		"Content-Length":        []string{"0"},
		"Docker-Content-Digest": []string{sha256Dgst.String()},
	})

	return resp.Header.Get("Location")
}

func finishUpload(t *testing.T, ub *v2.URLBuilder, name reference.Named, uploadURLBase string, dgst digest.Digest) string {
	resp, err := doPushLayer(t, ub, name, dgst, uploadURLBase, nil)
	if err != nil {
		t.Fatalf("unexpected error doing push layer request: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, "putting monolithic chunk", resp, http.StatusCreated)

	ref, _ := reference.WithDigest(name, dgst)
	expectedLayerURL, err := ub.BuildBlobURL(ref)
	if err != nil {
		t.Fatalf("error building expected layer url: %v", err)
	}

	checkHeaders(t, resp, http.Header{
		"Location":              []string{expectedLayerURL},
		"Content-Length":        []string{"0"},
		"Docker-Content-Digest": []string{dgst.String()},
	})

	return resp.Header.Get("Location")
}

func doPushChunk(t *testing.T, uploadURLBase string, body io.Reader) (*http.Response, digest.Digest, error) {
	u, err := url.Parse(uploadURLBase)
	if err != nil {
		t.Fatalf("unexpected error parsing pushLayer url: %v", err)
	}

	u.RawQuery = url.Values{
		"_state": u.Query()["_state"],
	}.Encode()

	uploadURL := u.String()

	digester := digest.Canonical.Digester()

	req, err := http.NewRequest("PATCH", uploadURL, io.TeeReader(body, digester.Hash()))
	if err != nil {
		t.Fatalf("unexpected error creating new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)

	return resp, digester.Digest(), err
}

func pushChunk(t *testing.T, ub *v2.URLBuilder, name reference.Named, uploadURLBase string, body io.Reader, length int64) (string, digest.Digest) {
	resp, dgst, err := doPushChunk(t, uploadURLBase, body)
	if err != nil {
		t.Fatalf("unexpected error doing push layer request: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, "putting chunk", resp, http.StatusAccepted)

	if err != nil {
		t.Fatalf("error generating sha256 digest of body")
	}

	checkHeaders(t, resp, http.Header{
		"Range":          []string{fmt.Sprintf("0-%d", length-1)},
		"Content-Length": []string{"0"},
	})

	return resp.Header.Get("Location"), dgst
}

func checkResponse(t testing.TB, msg string, resp *http.Response, expectedStatus int) {
	if resp.StatusCode != expectedStatus {
		t.Logf("unexpected status %s: %v != %v", msg, resp.StatusCode, expectedStatus)
		maybeDumpResponse(t, resp)

		t.FailNow()
	}

	// We expect the headers included in the configuration, unless the
	// status code is 405 (Method Not Allowed), which means the handler
	// doesn't even get called.
	if resp.StatusCode != 405 && !reflect.DeepEqual(resp.Header["X-Content-Type-Options"], []string{"nosniff"}) {
		t.Logf("missing or incorrect header X-Content-Type-Options %s", msg)
		maybeDumpResponse(t, resp)

		t.FailNow()
	}
}

// checkBodyHasErrorCodes ensures the body is an error body and has the
// expected error codes, returning the error structure, the json slice and a
// count of the errors by code.
func checkBodyHasErrorCodes(t *testing.T, msg string, resp *http.Response, errorCodes ...errcode.ErrorCode) (errcode.Errors, []byte, map[errcode.ErrorCode]int) {
	t.Helper()

	p, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)

	var errs errcode.Errors
	err = json.Unmarshal(p, &errs)
	require.NoError(t, err)

	require.NotEmpty(t, errs, "expected errors in response")

	// TODO(stevvooe): Shoot. The error setup is not working out. The content-
	// type headers are being set after writing the status code.
	// if resp.Header.Get("Content-Type") != "application/json" {
	//	t.Fatalf("unexpected content type: %v != 'application/json'",
	//		resp.Header.Get("Content-Type"))
	// }

	expected := map[errcode.ErrorCode]struct{}{}
	counts := map[errcode.ErrorCode]int{}

	// Initialize map with zeros for expected
	for _, code := range errorCodes {
		expected[code] = struct{}{}
		counts[code] = 0
	}

	for _, e := range errs {
		err, ok := e.(errcode.ErrorCoder)
		require.Truef(t, ok, "not an ErrorCoder: %#v", e)

		_, ok = expected[err.ErrorCode()]
		require.Truef(t, ok, "unexpected error code %v encountered during %s: %s ", err.ErrorCode(), msg, p)

		counts[err.ErrorCode()]++
	}

	// Ensure that counts of expected errors were all non-zero
	for code := range expected {
		require.NotZerof(t, counts[code], "expected error code %v not encountered during %s: %s", code, msg, p)
	}

	return errs, p, counts
}

func maybeDumpResponse(t testing.TB, resp *http.Response) {
	if d, err := httputil.DumpResponse(resp, true); err != nil {
		t.Logf("error dumping response: %v", err)
	} else {
		t.Logf("response:\n%s", string(d))
	}
}

// matchHeaders checks that the response has at least the headers. If not, the
// test will fail. If a passed in header value is "*", any non-zero value will
// suffice as a match.
func checkHeaders(t testing.TB, resp *http.Response, headers http.Header) {
	for k, vs := range headers {
		if resp.Header.Get(k) == "" {
			t.Fatalf("response missing header %q", k)
		}

		for _, v := range vs {
			if v == "*" {
				// Just ensure there is some value.
				if len(resp.Header[http.CanonicalHeaderKey(k)]) > 0 {
					continue
				}
			}

			for _, hv := range resp.Header[http.CanonicalHeaderKey(k)] {
				if hv != v {
					t.Fatalf("%+v %v header value not matched in response: %q != %q", resp.Header, k, hv, v)
				}
			}
		}
	}
}

func checkAllowedMethods(t *testing.T, url string, allowed []string) {
	resp, err := httpOptions(url)
	msg := "checking allowed methods"
	checkErr(t, err, msg)

	defer resp.Body.Close()

	checkResponse(t, msg, resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Allow": allowed,
	})
}

func checkErr(t *testing.T, err error, msg string) {
	if err != nil {
		t.Fatalf("unexpected error %s: %v", msg, err)
	}
}

func createRepository(t *testing.T, env *testEnv, repoPath string, tag string) digest.Digest {
	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath, putByTag(tag))

	_, payload, err := deserializedManifest.Payload()
	require.NoError(t, err)

	return digest.FromBytes(payload)
}

func createRepositoryWithMultipleIdenticalTags(t *testing.T, env *testEnv, repoPath string, tags []string) {
	deserializedManifest := seedRandomSchema2Manifest(t, env, repoPath)

	// upload a manifest per tag
	for _, tag := range tags {
		manifestTagURL := buildManifestTagURL(t, env, repoPath, tag)
		manifestDigestURL := buildManifestDigestURL(t, env, repoPath, deserializedManifest)

		resp := putManifest(t, "putting manifest no error", manifestTagURL, schema2.MediaTypeManifest, deserializedManifest.Manifest)
		defer resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		require.Equal(t, manifestDigestURL, resp.Header.Get("Location"))

		_, payload, err := deserializedManifest.Payload()
		require.NoError(t, err)
		dgst := digest.FromBytes(payload)

		require.Equal(t, dgst.String(), resp.Header.Get("Docker-Content-Digest"))
	}
}

// Test mutation operations on a registry configured as a cache.  Ensure that they return
// appropriate errors.
func TestRegistryAsCacheMutationAPIs(t *testing.T) {
	env := newTestEnvMirror(t, withDelete)
	defer env.Shutdown()

	imageName, _ := reference.WithName("foo/bar")
	tag := "latest"
	tagRef, _ := reference.WithTag(imageName, tag)
	manifestURL, err := env.builder.BuildManifestURL(tagRef)
	if err != nil {
		t.Fatalf("unexpected error building base url: %v", err)
	}

	// Manifest upload
	m := &schema2.Manifest{
		Versioned: manifest.Versioned{
			SchemaVersion: 2,
			MediaType:     schema2.MediaTypeManifest,
		},
		Layers: []distribution.Descriptor{
			{
				Digest:    digest.FromString("fake-layer"),
				MediaType: schema2.MediaTypeLayer,
			},
		},
	}

	deserializedManifest, err := schema2.FromStruct(*m)
	require.NoError(t, err)

	resp := putManifest(t, "putting manifest", manifestURL, schema2.MediaTypeManifest, deserializedManifest)
	checkResponse(t, "putting signed manifest to cache", resp, errcode.ErrorCodeUnsupported.Descriptor().HTTPStatusCode)

	// Manifest Delete
	resp, _ = httpDelete(manifestURL)
	checkResponse(t, "deleting signed manifest from cache", resp, errcode.ErrorCodeUnsupported.Descriptor().HTTPStatusCode)

	// Blob upload initialization
	layerUploadURL, err := env.builder.BuildBlobUploadURL(imageName)
	if err != nil {
		t.Fatalf("unexpected error building layer upload url: %v", err)
	}

	resp, err = http.Post(layerUploadURL, "", nil)
	if err != nil {
		t.Fatalf("unexpected error starting layer push: %v", err)
	}
	defer resp.Body.Close()

	checkResponse(t, fmt.Sprintf("starting layer push to cache %v", imageName), resp, errcode.ErrorCodeUnsupported.Descriptor().HTTPStatusCode)

	// Blob Delete
	ref, _ := reference.WithDigest(imageName, digestSha256EmptyTar)
	blobURL, _ := env.builder.BuildBlobURL(ref)
	resp, _ = httpDelete(blobURL)
	checkResponse(t, "deleting blob from cache", resp, errcode.ErrorCodeUnsupported.Descriptor().HTTPStatusCode)
}

func TestProxyManifestGetByTag(t *testing.T) {
	truthConfig := newConfig()

	imageName, _ := reference.WithName("foo/bar")
	tag := "latest"

	truthEnv := newTestEnvWithConfig(t, &truthConfig)
	defer truthEnv.Shutdown()
	// create a repository in the truth registry
	dgst := createRepository(t, truthEnv, imageName.Name(), tag)

	proxyConfig := newConfig()
	proxyConfig.Proxy.RemoteURL = truthEnv.server.URL

	proxyEnv := newTestEnvWithConfig(t, &proxyConfig)
	defer proxyEnv.Shutdown()

	digestRef, _ := reference.WithDigest(imageName, dgst)
	manifestDigestURL, err := proxyEnv.builder.BuildManifestURL(digestRef)
	checkErr(t, err, "building manifest url")

	resp, err := http.Get(manifestDigestURL)
	checkErr(t, err, "fetching manifest from proxy by digest")
	defer resp.Body.Close()

	tagRef, _ := reference.WithTag(imageName, tag)
	manifestTagURL, err := proxyEnv.builder.BuildManifestURL(tagRef)
	checkErr(t, err, "building manifest url")

	resp, err = http.Get(manifestTagURL)
	checkErr(t, err, "fetching manifest from proxy by tag (error check 1)")
	defer resp.Body.Close()
	checkResponse(t, "fetching manifest from proxy by tag (response check 1)", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Docker-Content-Digest": []string{dgst.String()},
	})

	// Create another manifest in the remote with the same image/tag pair
	newDigest := createRepository(t, truthEnv, imageName.Name(), tag)
	if dgst == newDigest {
		t.Fatalf("non-random test data")
	}

	// fetch it with the same proxy URL as before.  Ensure the updated content is at the same tag
	resp, err = http.Get(manifestTagURL)
	checkErr(t, err, "fetching manifest from proxy by tag (error check 2)")
	defer resp.Body.Close()
	checkResponse(t, "fetching manifest from proxy by tag (response check 2)", resp, http.StatusOK)
	checkHeaders(t, resp, http.Header{
		"Docker-Content-Digest": []string{newDigest.String()},
	})
}
