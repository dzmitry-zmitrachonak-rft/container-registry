package testutil

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TempRoot creates and manages the lifecycle of a temporary directory for
// driver testing.
func TempRoot(tb testing.TB) string {
	tb.Helper()

	d, err := ioutil.TempDir("", "driver-")
	require.NoError(tb, err)

	tb.Cleanup(func() {
		os.RemoveAll(d)
	})

	return d
}
