package swift

import (
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/docker/distribution/registry/internal/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ncw/swift/swifttest"

	"github.com/docker/distribution/context"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	dtestutil "github.com/docker/distribution/registry/storage/driver/internal/testutil"
	"github.com/docker/distribution/registry/storage/driver/testsuites"

	"gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { check.TestingT(t) }

var swiftDriverConstructor func(prefix string) (*Driver, error)

func init() {
	var (
		username           string
		password           string
		authURL            string
		tenant             string
		tenantID           string
		domain             string
		domainID           string
		tenantDomain       string
		tenantDomainID     string
		trustID            string
		container          string
		region             string
		AuthVersion        int
		endpointType       string
		insecureSkipVerify bool
		secretKey          string
		accessKey          string
		containerKey       bool
		tempURLMethods     []string

		swiftServer *swifttest.SwiftServer
		err         error
	)
	username = os.Getenv("SWIFT_USERNAME")
	password = os.Getenv("SWIFT_PASSWORD")
	authURL = os.Getenv("SWIFT_AUTH_URL")
	tenant = os.Getenv("SWIFT_TENANT_NAME")
	tenantID = os.Getenv("SWIFT_TENANT_ID")
	domain = os.Getenv("SWIFT_DOMAIN_NAME")
	domainID = os.Getenv("SWIFT_DOMAIN_ID")
	tenantDomain = os.Getenv("SWIFT_DOMAIN_NAME")
	tenantDomainID = os.Getenv("SWIFT_DOMAIN_ID")
	trustID = os.Getenv("SWIFT_TRUST_ID")
	container = os.Getenv("SWIFT_CONTAINER_NAME")
	region = os.Getenv("SWIFT_REGION_NAME")
	AuthVersion, _ = strconv.Atoi(os.Getenv("SWIFT_AUTH_VERSION"))
	endpointType = os.Getenv("SWIFT_ENDPOINT_TYPE")
	insecureSkipVerify, _ = strconv.ParseBool(os.Getenv("SWIFT_INSECURESKIPVERIFY"))
	secretKey = os.Getenv("SWIFT_SECRET_KEY")
	accessKey = os.Getenv("SWIFT_ACCESS_KEY")
	containerKey, _ = strconv.ParseBool(os.Getenv("SWIFT_TEMPURL_CONTAINERKEY"))
	tempURLMethods = strings.Split(os.Getenv("SWIFT_TEMPURL_METHODS"), ",")

	if username == "" || password == "" || authURL == "" || container == "" {
		if swiftServer, err = swifttest.NewSwiftServer("localhost"); err != nil {
			panic(err)
		}
		username = "swifttest"
		password = "swifttest"
		authURL = swiftServer.AuthURL
		container = "test"
	}

	prefix, err := os.MkdirTemp("", "driver-")
	if err != nil {
		panic(err)
	}
	defer os.Remove(prefix)

	swiftDriverConstructor = func(root string) (*Driver, error) {
		parameters := Parameters{
			username,
			password,
			authURL,
			tenant,
			tenantID,
			domain,
			domainID,
			tenantDomain,
			tenantDomainID,
			trustID,
			region,
			AuthVersion,
			container,
			root,
			endpointType,
			insecureSkipVerify,
			defaultChunkSize,
			secretKey,
			accessKey,
			containerKey,
			tempURLMethods,
		}

		return New(parameters)
	}

	driverConstructor := func() (storagedriver.StorageDriver, error) {
		return swiftDriverConstructor(prefix)
	}

	testsuites.RegisterSuite(driverConstructor, testsuites.NeverSkip)
}

func TestEmptyRootList(t *testing.T) {
	validRoot := dtestutil.TempRoot(t)

	rootedDriver, err := swiftDriverConstructor(validRoot)
	if err != nil {
		t.Fatalf("unexpected error creating rooted driver: %v", err)
	}

	emptyRootDriver, err := swiftDriverConstructor("")
	if err != nil {
		t.Fatalf("unexpected error creating empty root driver: %v", err)
	}

	slashRootDriver, err := swiftDriverConstructor("/")
	if err != nil {
		t.Fatalf("unexpected error creating slash root driver: %v", err)
	}

	filename := "/test"
	contents := []byte("contents")
	ctx := context.Background()
	err = rootedDriver.PutContent(ctx, filename, contents)
	if err != nil {
		t.Fatalf("unexpected error creating content: %v", err)
	}

	keys, _ := emptyRootDriver.List(ctx, "/")
	for _, path := range keys {
		if !storagedriver.PathRegexp.MatchString(path) {
			t.Fatalf("unexpected string in path: %q != %q", path, storagedriver.PathRegexp)
		}
	}

	keys, _ = slashRootDriver.List(ctx, "/")
	for _, path := range keys {
		if !storagedriver.PathRegexp.MatchString(path) {
			t.Fatalf("unexpected string in path: %q != %q", path, storagedriver.PathRegexp)
		}
	}

	// Create an object with a path nested under the existing object
	err = rootedDriver.PutContent(ctx, filename+"/file1", contents)
	if err != nil {
		t.Fatalf("unexpected error creating content: %v", err)
	}

	err = rootedDriver.Delete(ctx, filename)
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	keys, err = rootedDriver.List(ctx, "/")
	if err != nil {
		t.Fatalf("failed to list objects after deletion: %v", err)
	}

	if len(keys) != 0 {
		t.Fatal("delete did not remove nested objects")
	}
}

func TestFilenameChunking(t *testing.T) {
	// Test valid input and sizes
	input := []string{"a", "b", "c", "d", "e"}
	expecteds := [][][]string{
		{
			{"a"},
			{"b"},
			{"c"},
			{"d"},
			{"e"},
		},
		{
			{"a", "b"},
			{"c", "d"},
			{"e"},
		},
		{
			{"a", "b", "c"},
			{"d", "e"},
		},
		{
			{"a", "b", "c", "d"},
			{"e"},
		},
		{
			{"a", "b", "c", "d", "e"},
		},
		{
			{"a", "b", "c", "d", "e"},
		},
	}
	for i, expected := range expecteds {
		actual, err := chunkFilenames(input, i+1)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("chunk %v didn't match expected value %v", actual, expected)
		}
		if err != nil {
			t.Fatalf("unexpected error chunking filenames: %v", err)
		}
	}

	// Test nil input
	actual, err := chunkFilenames(nil, 5)
	if len(actual) != 0 {
		t.Fatal("chunks were returned when passed nil")
	}
	if err != nil {
		t.Fatalf("unexpected error chunking filenames: %v", err)
	}

	// Test 0 and < 0 sizes
	_, err = chunkFilenames(nil, 0)
	if err == nil {
		t.Fatal("expected error for size = 0")
	}
	_, err = chunkFilenames(nil, -1)
	if err == nil {
		t.Fatal("expected error for size = -1")
	}
}

func TestSwiftSegmentPath(t *testing.T) {
	d := &driver{
		Prefix: "/test/segment/path",
	}

	s1, err := d.swiftSegmentPath("foo-baz")
	if err != nil {
		t.Fatalf("unexpected error generating segment path: %v", err)
	}

	s2, err := d.swiftSegmentPath("foo-baz")
	if err != nil {
		t.Fatalf("unexpected error generating segment path: %v", err)
	}

	if !strings.HasPrefix(s1, "test/segment/path/segments/") {
		t.Fatalf("expected to be prefixed: %s", s1)
	}

	if !strings.HasPrefix(s1, "test/segment/path/segments/") {
		t.Fatalf("expected to be prefixed: %s", s2)
	}

	if len(s1) != 68 {
		t.Fatalf("unexpected segment path length, %d != %d", len(s1), 68)
	}

	if len(s2) != 68 {
		t.Fatalf("unexpected segment path length, %d != %d", len(s2), 68)
	}

	if s1 == s2 {
		t.Fatalf("expected segment paths to differ, %s == %s", s1, s2)
	}
}

func TestURLFor_Expiry(t *testing.T) {
	ctx := context.Background()
	validRoot := dtestutil.TempRoot(t)
	d, err := swiftDriverConstructor(validRoot)
	require.NoError(t, err)

	fp := "/foo"
	err = d.PutContent(ctx, fp, []byte(`bar`))
	require.NoError(t, err)

	// https://docs.openstack.org/swift/latest/api/temporary_url_middleware.html#temporary-url-format
	param := "temp_url_expires"

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
