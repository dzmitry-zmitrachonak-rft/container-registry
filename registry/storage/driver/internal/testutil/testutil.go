package testutil

import (
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TempRoot creates and manages the lifecycle of a temporary directory for
// driver testing.
func TempRoot(tb testing.TB) string {
	tb.Helper()

	d, err := os.MkdirTemp("", "driver-")
	require.NoError(tb, err)

	tb.Cleanup(func() {
		os.RemoveAll(d)
	})

	return d
}

type Opts struct {
	Defaultt          interface{}
	ParamName         string
	DriverParamName   string
	OriginalParams    map[string]interface{}
	ParseParametersFn func(map[string]interface{}) (interface{}, error)
}

func AssertByDefaultType(t *testing.T, opts Opts) {
	t.Helper()

	switch opts.Defaultt.(type) {
	case bool:
		TestBoolValue(t, opts)
	case string:
		TestStringValue(t, opts)
	}
}

func TestBoolValue(t *testing.T, opts Opts) {
	t.Helper()

	// Keep OriginalParams intact for idempotency.
	params := CopyMap(opts.OriginalParams)

	driverParams, err := opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertParam(t, driverParams, opts.DriverParamName, opts.Defaultt, "default value mismatch")

	params[opts.ParamName] = true
	driverParams, err = opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertParam(t, driverParams, opts.DriverParamName, true, "boolean true")

	params[opts.ParamName] = false
	driverParams, err = opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertParam(t, driverParams, opts.DriverParamName, false, "boolean false")

	params[opts.ParamName] = nil
	driverParams, err = opts.ParseParametersFn(params)
	require.NoError(t, err, "nil does not return: %v", err)

	AssertParam(t, driverParams, opts.DriverParamName, opts.Defaultt, "param is nil")

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

func TestStringValue(t *testing.T, opts Opts) {
	t.Helper()

	// Keep OriginalParams intact for idempotency.
	params := CopyMap(opts.OriginalParams)

	driverParams, err := opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertParam(t, driverParams, opts.DriverParamName, opts.Defaultt, "default value mismatch")

	params[opts.ParamName] = "value"
	driverParams, err = opts.ParseParametersFn(params)
	require.NoError(t, err)

	AssertParam(t, driverParams, opts.DriverParamName, true, "boolean true")

	params[opts.ParamName] = nil
	driverParams, err = opts.ParseParametersFn(params)
	require.NoError(t, err, "nil does not return: %v", err)

	AssertParam(t, driverParams, opts.DriverParamName, opts.Defaultt, "param is nil")

	params[opts.ParamName] = ""
	driverParams, err = opts.ParseParametersFn(params)
	require.Error(t, err, "empty string")

	params[opts.ParamName] = 12
	driverParams, err = opts.ParseParametersFn(params)
	require.Error(t, err, "not boolean type")
}

func AssertParam(t *testing.T, params interface{}, fieldName string, expected interface{}, msgs ...interface{}) {
	t.Helper()

	r := reflect.ValueOf(params)
	field := reflect.Indirect(r).FieldByName(fieldName)

	switch e := expected.(type) {
	case string:
		require.Equal(t, field.String(), e, msgs...)
	case bool:
		require.Equal(t, field.Bool(), e, msgs...)
	default:
		t.Fatalf("unhandled expected type: %T", e)
	}
}

func CopyMap(original map[string]interface{}) map[string]interface{} {
	newMap := make(map[string]interface{})
	for k, v := range original {
		newMap[k] = v
	}

	return newMap
}
