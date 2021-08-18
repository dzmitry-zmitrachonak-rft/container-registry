package eligibilitymock

import (
	"context"
	"testing"

	"github.com/docker/distribution/registry/auth"
	"github.com/docker/distribution/registry/internal/migration"
	"github.com/stretchr/testify/require"
)

func TestAuthorized(t *testing.T) {
	var tests = []struct {
		name                   string
		enabled                bool
		eligible               bool
		expectedHasEligibility bool
		expectedEligibility    bool
	}{
		{
			name:                   "not enabled not eligible",
			enabled:                false,
			eligible:               false,
			expectedHasEligibility: false,
			expectedEligibility:    false,
		},
		{
			name:                   "not enabled but eligible",
			enabled:                false,
			eligible:               true,
			expectedHasEligibility: false,
			expectedEligibility:    false,
		},
		{
			name:                   "enabled and eligible",
			enabled:                true,
			eligible:               true,
			expectedHasEligibility: true,
			expectedEligibility:    true,
		},
		{
			name:                   "enabled but not eligible",
			enabled:                true,
			eligible:               false,
			expectedHasEligibility: true,
			expectedEligibility:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := &accessController{enabled: tt.enabled, eligible: tt.eligible}

			ctx, err := ac.Authorized(context.Background(), auth.Access{})
			require.NoError(t, err)

			require.Equal(t, tt.expectedHasEligibility, migration.HasEligibilityFlag(ctx))
			require.Equal(t, tt.expectedEligibility, migration.IsEligible(ctx))
		})
	}
}
