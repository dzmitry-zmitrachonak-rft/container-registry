package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_migrationContext_Value(t *testing.T) {
	ctx := context.WithValue(context.Background(), "foo", "bar")
	mc := migrationContext{
		Context: ctx,
	}

	b, ok := mc.Value(EligibilityKey).(bool)
	require.True(t, ok)
	require.False(t, b)

	mc.eligible = true
	b, ok = mc.Value(EligibilityKey).(bool)
	require.True(t, ok)
	require.True(t, b)

	s, ok := mc.Value("foo").(string)
	require.True(t, ok)
	require.Equal(t, "bar", s)
}

func Test_migrationContext_WithEligibility(t *testing.T) {
	ctx := context.WithValue(context.Background(), "foo", "bar")
	mc := WithEligibility(ctx, true)

	b, ok := mc.Value(EligibilityKey).(bool)
	require.True(t, ok)
	require.True(t, b)

	s, ok := mc.Value("foo").(string)
	require.True(t, ok)
	require.Equal(t, "bar", s)
}

func Test_migrationContext_HasEligibilityFlag(t *testing.T) {
	ctx := context.Background()
	require.False(t, HasEligibilityFlag(ctx))

	mCtx := WithEligibility(ctx, false)
	require.True(t, HasEligibilityFlag(mCtx))

	mCtx = WithEligibility(ctx, true)
	require.True(t, HasEligibilityFlag(mCtx))
}

func Test_migrationContext_IsEligible(t *testing.T) {
	ctx := context.Background()
	require.False(t, IsEligible(ctx))

	mCtx := WithEligibility(ctx, false)
	require.False(t, IsEligible(mCtx))

	mCtx = WithEligibility(ctx, true)
	require.True(t, IsEligible(mCtx))
}

func Test_CodePathVal_String(t *testing.T) {
	require.Equal(t, "", UnknownCodePath.String())
	require.Equal(t, "old", OldCodePath.String())
	require.Equal(t, "new", NewCodePath.String())
}

func Test_migrationContext_WithCodePath(t *testing.T) {
	ctx := context.WithValue(context.Background(), "foo", "bar")
	mc := WithCodePath(ctx, OldCodePath)

	v, ok := mc.Value(CodePathKey).(CodePathVal)
	require.True(t, ok)
	require.Equal(t, OldCodePath, v)

	s, ok := mc.Value("foo").(string)
	require.True(t, ok)
	require.Equal(t, "bar", s)
}

func Test_migrationContext_CodePath(t *testing.T) {
	require.Equal(t, UnknownCodePath, CodePath(context.Background()))

	mc := WithCodePath(context.Background(), OldCodePath)
	require.Equal(t, OldCodePath, CodePath(mc))

	mc = WithCodePath(context.Background(), NewCodePath)
	require.Equal(t, NewCodePath, CodePath(mc))
}
