/*
Package migration provides utilities to support the GitLab.com migration Phase 1, as
described in https://gitlab.com/gitlab-org/container-registry/-/issues/374.
*/
package migration

import "context"

// EligibilityKey is used to get the migration eligibility flag from a context.
const EligibilityKey = "migration.eligible"

type migrationContext struct {
	context.Context
	eligible bool
}

// Value implements context.Context.
func (mc migrationContext) Value(key interface{}) interface{} {
	if key == EligibilityKey {
		return mc.eligible
	}
	return mc.Context.Value(key)
}

// WithMigrationEligibility returns a context with the migration eligibility info.
func WithMigrationEligibility(ctx context.Context, eligible bool) context.Context {
	return migrationContext{
		Context:  ctx,
		eligible: eligible,
	}
}

// HasEligibilityFlag determines if the context has the eligibility flag set.
func HasEligibilityFlag(ctx context.Context) bool {
	if ctx.Value(EligibilityKey) != nil {
		return true
	}
	return false
}

// IsEligible determines whether the given request context was marked as eligible for migration or not.
func IsEligible(ctx context.Context) bool {
	if v, ok := ctx.Value(EligibilityKey).(bool); ok {
		return v
	}
	return false
}
