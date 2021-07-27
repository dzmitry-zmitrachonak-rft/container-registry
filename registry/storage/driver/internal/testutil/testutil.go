package testutil

import (
	"io/ioutil"
	"os"
	"reflect"
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

type BoolOpts struct {
	Defaultt          bool
	NilReturnsError   bool
	ParamName         string
	DriverParamName   string
	OriginalParams    map[string]interface{}
	ParseParametersFn func(map[string]interface{}) (interface{}, error)
}

func TestBoolValue(t *testing.T, opts BoolOpts) {
	t.Helper()

	// Keep OriginalParams intact for idempotency.
	params := CopyMap(opts.OriginalParams)

	driverParams, err := opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertBoolParam(t, driverParams, opts.DriverParamName, opts.Defaultt, "default value mismatch")

	params[opts.ParamName] = true
	driverParams, err = opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertBoolParam(t, driverParams, opts.DriverParamName, true, "boolean true")

	params[opts.ParamName] = false
	driverParams, err = opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertBoolParam(t, driverParams, opts.DriverParamName, false, "boolean false")

	params[opts.ParamName] = nil
	driverParams, err = opts.ParseParametersFn(params)
	require.Equal(t, err != nil, opts.NilReturnsError, err)

	if !opts.NilReturnsError {
		AssertBoolParam(t, driverParams, opts.DriverParamName, opts.Defaultt, "param is nil")
	}

	params[opts.ParamName] = ""
	driverParams, err = opts.ParseParametersFn(params)
	require.Error(t, err, "empty string")

	params[opts.ParamName] = "invalid"
	driverParams, err = opts.ParseParametersFn(params)
	require.Error(t, err, "not boolean string")

	params[opts.ParamName] = 12
	driverParams, err = opts.ParseParametersFn(params)
	require.Error(t, err, "not boolean type")
}

func AssertBoolParam(t *testing.T, params interface{}, fieldName string, expected bool, msgs ...interface{}) {
	t.Helper()

	r := reflect.ValueOf(params)
	field := reflect.Indirect(r).FieldByName(fieldName)

	require.True(t, field.Bool() == expected, msgs...)
}

func CopyMap(original map[string]interface{}) map[string]interface{} {
	newMap := make(map[string]interface{})
	for k, v := range original {
		newMap[k] = v
	}

	return newMap
}
