package inmemory

import (
	"testing"

	storagedriver "gitlab.com/gitlab-org/container-registry/registry/storage/driver"
	"gitlab.com/gitlab-org/container-registry/registry/storage/driver/testsuites"
	"gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { check.TestingT(t) }

func init() {
	inmemoryDriverConstructor := func() (storagedriver.StorageDriver, error) {
		return New(), nil
	}
	testsuites.RegisterSuite(inmemoryDriverConstructor, testsuites.NeverSkip)
}
