// Package eligibilitymock provides the migration mode with eligibility flags
// for testing. This enables local tests using migration auth eligibility
// without needing to stand up a real authentication service.
//
// This package is exclusively for testing — never use it in production.
package eligibilitymock

import (
	"context"
	"fmt"

	"github.com/docker/distribution/registry/auth"
	"github.com/docker/distribution/registry/internal/migration"
)

// accessController provides a simple implementation of auth.AccessController
// that simply checks for a non-empty Authorization header. It is useful for
// demonstration and testing.
type accessController struct {
	enabled  bool
	eligible bool
}

var _ auth.AccessController = &accessController{}

func newAccessController(options map[string]interface{}) (auth.AccessController, error) {
	enabled, present := options["enabled"]
	if !present {
		return nil, fmt.Errorf(`"enabled" must be set for eligibility access controller`)
	}

	enabledBool, ok := enabled.(bool)
	if !ok {
		return nil, fmt.Errorf(`"enabled" must be a boolean for eligibility access controller`)
	}

	eligible, present := options["eligible"]
	if !present {
		return nil, fmt.Errorf(`"eligible" must be set for eligibility access controller`)
	}

	eligibleBool, ok := eligible.(bool)
	if !ok {
		return nil, fmt.Errorf(`"eligible" must be a boolean for eligibility access controller`)
	}

	return &accessController{enabled: enabledBool, eligible: eligibleBool}, nil
}

// Authorized injects the auth eligibility flag, if enabled.
func (ac *accessController) Authorized(ctx context.Context, accessRecords ...auth.Access) (context.Context, error) {
	ctx = auth.WithUser(ctx, auth.UserInfo{Name: "eligibilitymock"})

	if !ac.enabled {
		return ctx, nil
	}

	return migration.WithEligibility(ctx, ac.eligible), nil
}

// Register eligibilitymock auth backend.
func init() {
	auth.Register("eligibilitymock", auth.InitFunc(newAccessController))
}
