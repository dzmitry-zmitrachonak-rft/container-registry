// +build include_gcs

package gcs

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/docker/distribution/registry/internal/testutil"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"gopkg.in/check.v1"

	dcontext "github.com/docker/distribution/context"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	dtestutil "github.com/docker/distribution/registry/storage/driver/internal/testutil"
	"github.com/docker/distribution/registry/storage/driver/testsuites"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { check.TestingT(t) }

var gcsDriverConstructor func(rootDirectory string) (storagedriver.StorageDriver, error)
var gcsTargetDriverConstructor func(rootDirectory string) (storagedriver.StorageDriver, error)
var skipGCS func() string
var skipGCSTransferTo func() string

const maxConcurrency = 10

func init() {
	bucket := os.Getenv("REGISTRY_STORAGE_GCS_BUCKET")
	migrationBucket := os.Getenv("REGISTRY_STORAGE_GCS_TARGET_BUCKET")
	credentials := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	parallelWalk := os.Getenv("GCS_PARALLEL_WALK")

	// Skip GCS storage driver tests if environment variable parameters are not provided
	skipGCS = func() string {
		if bucket == "" || credentials == "" {
			return "The following environment variables must be set to enable these tests: REGISTRY_STORAGE_GCS_BUCKET, GOOGLE_APPLICATION_CREDENTIALS"
		}
		return ""
	}

	if skipGCS() != "" {
		return
	}

	skipGCSTransferTo = func() string {
		if migrationBucket == "" {
			return "The following environment variable must be set to enable these tests: REGISTRY_STORAGE_GCS_TARGET_BUCKET"
		}
		return ""
	}

	root, err := os.MkdirTemp("", "driver-")
	if err != nil {
		panic(err)
	}
	defer os.Remove(root)

	ctx := context.Background()
	creds, err := google.FindDefaultCredentials(ctx, storage.ScopeFullControl)
	if err != nil {
		panic(fmt.Sprintf("Error reading default credentials: %s", err))
	}

	ts := creds.TokenSource
	jwtConfig, err := google.JWTConfigFromJSON(creds.JSON)
	if err != nil {
		panic(fmt.Sprintf("Error reading JWT config: %s", err))
	}
	email := jwtConfig.Email
	if email == "" {
		panic("Error reading JWT config : missing client_email property")
	}
	privateKey := jwtConfig.PrivateKey
	if len(privateKey) == 0 {
		panic("Error reading JWT config : missing private_key property")
	}

	storageClient, err := storage.NewClient(dcontext.Background(), option.WithTokenSource(ts))
	if err != nil {
		panic(fmt.Sprintf("Error creating storage client: %s", err))
	}

	var parallelWalkBool bool

	if parallelWalk != "" {
		parallelWalkBool, err = strconv.ParseBool(parallelWalk)

		if err != nil {
			panic(fmt.Sprintf("Error parsing parallelwalk: %v", err))
		}
	}

	gcsDriverConstructor = func(rootDirectory string) (storagedriver.StorageDriver, error) {
		parameters := &driverParameters{
			bucket:         bucket,
			rootDirectory:  root,
			email:          email,
			privateKey:     privateKey,
			client:         oauth2.NewClient(dcontext.Background(), ts),
			storageClient:  storageClient,
			chunkSize:      defaultChunkSize,
			maxConcurrency: maxConcurrency,
			parallelWalk:   parallelWalkBool,
		}

		return New(parameters)
	}

	gcsTargetDriverConstructor = func(rootDirectory string) (storagedriver.StorageDriver, error) {
		if migrationBucket == "" {
			return nil, errors.New("REGISTRY_STORAGE_GCS_TARGET_BUCKET must be set")
		}

		parameters := &driverParameters{
			bucket:         migrationBucket,
			rootDirectory:  root,
			email:          email,
			privateKey:     privateKey,
			client:         oauth2.NewClient(dcontext.Background(), ts),
			storageClient:  storageClient,
			chunkSize:      defaultChunkSize,
			maxConcurrency: maxConcurrency,
			parallelWalk:   parallelWalkBool,
		}

		return New(parameters)
	}

	testsuites.RegisterSuite(func() (storagedriver.StorageDriver, error) {
		return gcsDriverConstructor(root)
	}, skipGCS)
}

// Test Committing a FileWriter without having called Write
func TestCommitEmpty(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	driver := newTempDirDriver(t)

	filename := "/test"
	ctx := dcontext.Background()

	writer, err := driver.Writer(ctx, filename, false)
	defer driver.Delete(ctx, filename)
	if err != nil {
		t.Fatalf("driver.Writer: unexpected error: %v", err)
	}
	err = writer.Commit()
	if err != nil {
		t.Fatalf("writer.Commit: unexpected error: %v", err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatalf("writer.Close: unexpected error: %v", err)
	}
	if writer.Size() != 0 {
		t.Fatalf("writer.Size: %d != 0", writer.Size())
	}
	readContents, err := driver.GetContent(ctx, filename)
	if err != nil {
		t.Fatalf("driver.GetContent: unexpected error: %v", err)
	}
	if len(readContents) != 0 {
		t.Fatalf("len(driver.GetContent(..)): %d != 0", len(readContents))
	}
	fileInfo, err := driver.Stat(ctx, filename)
	if err != nil {
		t.Fatalf("driver.Stat: unexpected error: %v", err)
	}
	if fileInfo.Size() != 0 {
		t.Fatalf("stat.Size: %d != 0", fileInfo.Size())
	}
}

// Test Committing a FileWriter after having written exactly
// defaultChunksize bytes.
func TestCommit(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	driver := newTempDirDriver(t)

	filename := "/test"
	ctx := dcontext.Background()

	contents := make([]byte, defaultChunkSize)
	writer, err := driver.Writer(ctx, filename, false)
	defer driver.Delete(ctx, filename)
	if err != nil {
		t.Fatalf("driver.Writer: unexpected error: %v", err)
	}
	_, err = writer.Write(contents)
	if err != nil {
		t.Fatalf("writer.Write: unexpected error: %v", err)
	}
	err = writer.Commit()
	if err != nil {
		t.Fatalf("writer.Commit: unexpected error: %v", err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatalf("writer.Close: unexpected error: %v", err)
	}
	if writer.Size() != int64(len(contents)) {
		t.Fatalf("writer.Size: %d != %d", writer.Size(), len(contents))
	}
	readContents, err := driver.GetContent(ctx, filename)
	if err != nil {
		t.Fatalf("driver.GetContent: unexpected error: %v", err)
	}
	if len(readContents) != len(contents) {
		t.Fatalf("len(driver.GetContent(..)): %d != %d", len(readContents), len(contents))
	}
	fileInfo, err := driver.Stat(ctx, filename)
	if err != nil {
		t.Fatalf("driver.Stat: unexpected error: %v", err)
	}
	if fileInfo.Size() != int64(len(contents)) {
		t.Fatalf("driver.Stat.Size: %d != %d", fileInfo.Size(), len(contents))
	}
}

func TestRetry(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	assertError := func(expected string, observed error) {
		observedMsg := "<nil>"
		if observed != nil {
			observedMsg = observed.Error()
		}
		if observedMsg != expected {
			t.Fatalf("expected %v, observed %v\n", expected, observedMsg)
		}
	}

	err := retry(func() error {
		return &googleapi.Error{
			Code:    503,
			Message: "google api error",
		}
	})
	assertError("googleapi: Error 503: google api error", err)

	err = retry(func() error {
		return &googleapi.Error{
			Code:    404,
			Message: "google api error",
		}
	})
	assertError("googleapi: Error 404: google api error", err)

	err = retry(func() error {
		return fmt.Errorf("error")
	})
	assertError("error", err)
}

func TestEmptyRootList(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	rootedDriver := newTempDirDriver(t)

	emptyRootDriver, err := gcsDriverConstructor("")
	if err != nil {
		t.Fatalf("unexpected error creating empty root driver: %v", err)
	}

	slashRootDriver, err := gcsDriverConstructor("/")
	if err != nil {
		t.Fatalf("unexpected error creating slash root driver: %v", err)
	}

	filename := "/test"
	contents := []byte("contents")
	ctx := dcontext.Background()
	err = rootedDriver.PutContent(ctx, filename, contents)
	if err != nil {
		t.Fatalf("unexpected error creating content: %v", err)
	}
	defer func() {
		err := rootedDriver.Delete(ctx, filename)
		if err != nil {
			t.Fatalf("failed to remove %v due to %v\n", filename, err)
		}
	}()
	keys, err := emptyRootDriver.List(ctx, "/")
	for _, path := range keys {
		if !storagedriver.PathRegexp.MatchString(path) {
			t.Fatalf("unexpected string in path: %q != %q", path, storagedriver.PathRegexp)
		}
	}

	keys, err = slashRootDriver.List(ctx, "/")
	for _, path := range keys {
		if !storagedriver.PathRegexp.MatchString(path) {
			t.Fatalf("unexpected string in path: %q != %q", path, storagedriver.PathRegexp)
		}
	}
}

// Test subpaths are included properly
func TestSubpathList(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	rootedDriver := newTempDirDriver(t)

	filenames := []string{
		"/test/test1.txt",
		"/test/test2.txt",
		"/test/subpath/test3.txt",
		"/test/subpath/test4.txt",
		"/test/subpath/path/test5.txt"}
	contents := []byte("contents")
	ctx := dcontext.Background()

	for _, filename := range filenames {
		err := rootedDriver.PutContent(ctx, filename, contents)
		if err != nil {
			t.Fatalf("unexpected error creating content: %v", err)
		}
	}
	defer func() {
		for _, filename := range filenames {
			err := rootedDriver.Delete(ctx, filename)
			if err != nil {
				t.Fatalf("failed to remove %v due to %v\n", filename, err)
			}
		}
	}()

	keys, err := rootedDriver.List(ctx, "/test")
	require.NoError(t, err)

	expected := []string{"/test/test1.txt", "/test/test2.txt", "/test/subpath"}
	sort.Strings(expected)
	sort.Strings(keys)

	if !reflect.DeepEqual(expected, keys) {
		t.Fatalf("list %v does not match %v", keys, expected)
	}
}

// TestMoveDirectory checks that moving a directory returns an error.
func TestMoveDirectory(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	driver := newTempDirDriver(t)

	ctx := dcontext.Background()
	contents := []byte("contents")
	// Create a regular file.
	err := driver.PutContent(ctx, "/parent/dir/foo", contents)
	if err != nil {
		t.Fatalf("unexpected error creating content: %v", err)
	}
	defer func() {
		err := driver.Delete(ctx, "/parent")
		if err != nil {
			t.Fatalf("failed to remove /parent due to %v\n", err)
		}
	}()

	err = driver.Move(ctx, "/parent/dir", "/parent/other")
	if err == nil {
		t.Fatalf("Moving directory /parent/dir /parent/other should have return a non-nil error\n")
	}
}

func TestTransferTo(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	if skipGCSTransferTo() != "" {
		t.Skip(skipGCSTransferTo())
	}

	validRoot := dtestutil.TempRoot(t)

	srcDriver, err := gcsDriverConstructor(validRoot)
	require.NoError(t, err)

	destDriver, err := gcsTargetDriverConstructor(validRoot)
	require.NoError(t, err)

	b := make([]byte, 10)
	rand.Read(b)

	ctx := context.Background()
	path := "/happy/data/path"

	// Write content to source.
	err = srcDriver.PutContent(ctx, path, b)
	require.NoError(t, err)
	_, err = srcDriver.Stat(ctx, path)
	require.NoError(t, err)

	// Destination should not have already have content at the path.
	_, err = destDriver.Stat(ctx, path)
	require.True(t, errors.As(err, &storagedriver.PathNotFoundError{}))

	// Transfer to destination.
	err = srcDriver.TransferTo(ctx, destDriver, path, path)
	require.NoError(t, err)

	// Reading from destination should work.
	c, err := destDriver.GetContent(ctx, path)
	require.NoError(t, err)
	require.EqualValues(t, b, c)

	// Source content should be unaltered.
	c, err = srcDriver.GetContent(ctx, path)
	require.NoError(t, err)
	require.EqualValues(t, b, c)
}

func TestTransferToSameBucket(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	if skipGCSTransferTo() != "" {
		t.Skip(skipGCSTransferTo())
	}

	srcDriver := newTempDirDriver(t)

	b := make([]byte, 10)
	rand.Read(b)

	ctx := context.Background()
	path := "/same/bucket/data/path"

	// Write content to source.
	err := srcDriver.PutContent(ctx, path, b)
	require.NoError(t, err)
	_, err = srcDriver.Stat(ctx, path)
	require.NoError(t, err)

	// Transfer to destination should exit early with error.
	err = srcDriver.TransferTo(ctx, srcDriver, path, path)
	require.EqualError(t, err, "srcDriver and destDriver must not have the same bucket")
}

func TestTransferToInvalidPath(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	if skipGCSTransferTo() != "" {
		t.Skip(skipGCSTransferTo())
	}

	validRoot := dtestutil.TempRoot(t)

	srcDriver, err := gcsDriverConstructor(validRoot)
	require.NoError(t, err)

	destDriver, err := gcsTargetDriverConstructor(validRoot)
	require.NoError(t, err)

	b := make([]byte, 10)
	rand.Read(b)

	ctx := context.Background()
	srcPath := "/valid/utf8/path"
	// Not a valid UTF-8 string, transfer will fail validating the path.
	destPath := "\xC2\x7F\x80\x80"

	// Write content to source.
	err = srcDriver.PutContent(ctx, srcPath, b)
	require.NoError(t, err)
	_, err = srcDriver.Stat(ctx, srcPath)
	require.NoError(t, err)

	// Transfer to destination, we expect a partial transfer error here.
	err = srcDriver.TransferTo(ctx, destDriver, srcPath, destPath)
	e := &storagedriver.PartialTransferError{}
	require.True(t, errors.As(err, e))

	// Driver paths include the root, so check for the presence of the child paths.
	require.Contains(t, e.SourcePath, srcPath)
	require.Contains(t, e.DestinationPath, destPath)
}

func TestTransferToExistingDest(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	if skipGCSTransferTo() != "" {
		t.Skip(skipGCSTransferTo())
	}

	validRoot := dtestutil.TempRoot(t)

	srcDriver, err := gcsDriverConstructor(validRoot)
	require.NoError(t, err)

	destDriver, err := gcsTargetDriverConstructor(validRoot)
	require.NoError(t, err)

	srcContent := make([]byte, 10)
	rand.Read(srcContent)

	destContent := make([]byte, 10)
	rand.Read(destContent)

	ctx := context.Background()
	path := "/existing/data/path"

	// Write content in both locations.
	err = srcDriver.PutContent(ctx, path, srcContent)
	require.NoError(t, err)

	err = destDriver.PutContent(ctx, path, destContent)
	require.NoError(t, err)

	// Transfer should overwrite the path on the destDriver.
	err = srcDriver.TransferTo(ctx, destDriver, path, path)
	require.NoError(t, err)

	// Getting content from destination after transfer should match the srcContent.
	c, err := destDriver.GetContent(ctx, path)
	require.NoError(t, err)
	require.EqualValues(t, srcContent, c)
}

func Test_parseParameters_Bool(t *testing.T) {
	p := map[string]interface{}{
		"bucket":  "bucket",
		"keyfile": "testdata/key.json",
		// TODO: add string test cases, if needed?
	}

	testFn := func(params map[string]interface{}) (interface{}, error) {
		return parseParameters(params)
	}

	opts := dtestutil.Opts{
		Defaultt:          false,
		ParamName:         "parallelwalk",
		DriverParamName:   "parallelWalk",
		OriginalParams:    p,
		ParseParametersFn: testFn,
	}
	dtestutil.AssertByDefaultType(t, opts)
}

func newTempDirDriver(tb testing.TB) storagedriver.StorageDriver {
	tb.Helper()

	root := dtestutil.TempRoot(tb)

	d, err := gcsDriverConstructor(root)
	require.NoError(tb, err)

	return d
}

func TestURLFor_Expiry(t *testing.T) {
	if skipGCS() != "" {
		t.Skip(skipGCS())
	}

	ctx := context.Background()
	validRoot := dtestutil.TempRoot(t)
	d, err := gcsDriverConstructor(validRoot)
	require.NoError(t, err)

	fp := "/foo"
	err = d.PutContent(ctx, fp, []byte(`bar`))
	require.NoError(t, err)

	// https://cloud.google.com/storage/docs/access-control/signed-urls-v2
	param := "Expires"

	mock := clock.NewMock()
	mock.Set(time.Now())
	testutil.StubClock(t, &systemClock, mock)

	// default
	s, err := d.URLFor(ctx, fp, nil)
	require.NoError(t, err)
	u, err := url.Parse(s)
	require.NoError(t, err)

	dt := mock.Now().Add(20 * time.Minute)
	expected := fmt.Sprint(dt.Unix())
	require.Equal(t, expected, u.Query().Get(param))

	// custom
	dt = mock.Now().Add(1 * time.Hour)
	s, err = d.URLFor(ctx, fp, map[string]interface{}{"expiry": dt})
	require.NoError(t, err)

	u, err = url.Parse(s)
	require.NoError(t, err)
	expected = fmt.Sprint(dt.Unix())
	require.Equal(t, expected, u.Query().Get(param))
}
