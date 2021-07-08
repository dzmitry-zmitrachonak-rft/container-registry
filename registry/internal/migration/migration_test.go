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

func Test_migrationContext_WithMigrationEligibility(t *testing.T) {
	ctx := context.WithValue(context.Background(), "foo", "bar")
	mc := WithMigrationEligibility(ctx, true)

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

	mCtx := WithMigrationEligibility(ctx, false)
	require.True(t, HasEligibilityFlag(mCtx))

	mCtx = WithMigrationEligibility(ctx, true)
	require.True(t, HasEligibilityFlag(mCtx))
}

func Test_migrationContext_IsEligible(t *testing.T) {
	ctx := context.Background()
	require.False(t, IsEligible(ctx))

	mCtx := WithMigrationEligibility(ctx, false)
	require.False(t, IsEligible(mCtx))

	mCtx = WithMigrationEligibility(ctx, true)
	require.True(t, IsEligible(mCtx))
}
