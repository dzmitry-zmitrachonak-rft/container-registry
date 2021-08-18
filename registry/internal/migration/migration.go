/*
Package migration provides utilities to support the GitLab.com migration Phase 1, as
described in https://gitlab.com/gitlab-org/container-registry/-/issues/374.
*/
package migration

import "context"

const (
	// EligibilityKey is used to get the migration eligibility flag from a context.
	EligibilityKey = "migration.eligible"
	// CodePathKey is used to get the migration code path from a context.
	CodePathKey = "migration.path"
	// CodePathHeader is used to set/get the migration code path HTTP response header.
	CodePathHeader = "Gitlab-Migration-Path"

	// UnknownCodePath is used when no code path was provided/found.
	UnknownCodePath CodePathVal = iota
	// OldCodePath is used to identify the old code path.
	OldCodePath
	// NewCodePath is used to identify the new code path.
	NewCodePath
)

// CodePathVal is used to define the possible code path values.
type CodePathVal int

// String implements fmt.Stringer.
func (v CodePathVal) String() string {
	switch v {
	case OldCodePath:
		return "old"
	case NewCodePath:
		return "new"
	default:
		return ""
	}
}

type migrationContext struct {
	context.Context
	eligible bool
	path     CodePathVal
}

// Value implements context.Context.
func (mc migrationContext) Value(key interface{}) interface{} {
	switch key {
	case EligibilityKey:
		return mc.eligible
	case CodePathKey:
		return mc.path
	default:
		return mc.Context.Value(key)
	}
}

// WithEligibility returns a context with the migration eligibility info.
func WithEligibility(ctx context.Context, eligible bool) context.Context {
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

// WithCodePath returns a context with the migration code path info.
func WithCodePath(ctx context.Context, path CodePathVal) context.Context {
	return migrationContext{
		Context: ctx,
		path:    path,
	}
}

// CodePath extracts the migration code path info from a context.
func CodePath(ctx context.Context) CodePathVal {
	if v, ok := ctx.Value(CodePathKey).(CodePathVal); ok {
		return v
	}
	return UnknownCodePath
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
	StatusNewRepo
)

func (m Status) String() string {
	msg := map[Status]string{
		StatusUnknown:                    "Unknown",
		StatusMigrationDisabled:          "MigrationDisabled",
		StatusError:                      "Error",
		StatusOldRepo:                    "OldRepo",
		StatusNewRepo:                    "NewRepo",
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
		StatusNewRepo:                    "repository is new, serving via new code path",
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
