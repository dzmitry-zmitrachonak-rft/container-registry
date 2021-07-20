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

// Status enum for repository migration status.
type Status int

// Ineligible statuses.
const (
	StatusUnknown Status = iota // Always first to catch uninitialized values.
	StatusMigrationDisabled
	StatusError
	StatusOldRepo
	StatusAuthEligibilityNotSet
	StatusNotEligible
	StatusNonRepositoryScopedRequest
)

const eligibilityCutoff = 1000

// Eligible statuses.
const (
	StatusEligible = iota + eligibilityCutoff
	StatusAuthEligibilityDisabled
)

func (m Status) String() string {
	msg := map[Status]string{
		StatusUnknown:                    "Unknown",
		StatusMigrationDisabled:          "MigrationDisabled",
		StatusError:                      "Error",
		StatusOldRepo:                    "OldRepo",
		StatusAuthEligibilityNotSet:      "AuthEligibilityNotSet",
		StatusNotEligible:                "NotEligible",
		StatusNonRepositoryScopedRequest: "NonRepositoryScopedRequest",
		StatusEligible:                   "Eligible",
		StatusAuthEligibilityDisabled:    "AuthEligibilityDisabled",
	}

	s, ok := msg[m]
	if !ok {
		return msg[StatusUnknown]
	}

	return s
}

// Description returns a human readable description of the migration status.
func (m Status) Description() string {
	msg := map[Status]string{
		StatusUnknown:                    "unknown migration status",
		StatusMigrationDisabled:          "migration mode is disabled in registry config",
		StatusError:                      "error determining migration status",
		StatusOldRepo:                    "repository is old, serving via old code path",
		StatusAuthEligibilityNotSet:      "migration eligibility not set, serving new repository via old code path",
		StatusNotEligible:                "new repository flagged as not eligible for migration, serving via old code path",
		StatusNonRepositoryScopedRequest: "request is not scoped to single repository",
		StatusEligible:                   "new repository flagged as eligible for migration, serving via new code path",
		StatusAuthEligibilityDisabled:    "migration auth eligibility is disabled in registry config, serving new repository via new code path",
	}

	s, ok := msg[m]
	if !ok {
		return msg[StatusUnknown]
	}

	return s
}

// ShouldMigrate determines if a repository should be served via the new code path.
func (m Status) ShouldMigrate() bool {
	return m >= eligibilityCutoff
}
