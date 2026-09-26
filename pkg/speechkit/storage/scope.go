package storage

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// LocalInstallID is the install identifier [NormalizeScope] assigns to a
// scope with no install, device, user, or tenant: the no-config local
// default, keyed "install:local".
const LocalInstallID = "local"

// Errors returned by [ScopePolicy.Validate].
var (
	// ErrScopeUserRequired reports a scope without a UserID under
	// [ScopeUserRequired].
	ErrScopeUserRequired = errors.New("speechkit storage: user scope is required")
	// ErrScopeTenantRequired reports a scope without a TenantID under
	// [ScopeTenantRequired].
	ErrScopeTenantRequired = errors.New("speechkit storage: tenant scope is required")
)

// Scope identifies who owns a stored record: the installation, device,
// user, and tenant, plus free-form Labels (for example workspace or
// session) that partition data further. Every field is optional; the
// empty Scope is the local default.
type Scope struct {
	InstallID string            `json:"installId,omitempty"`
	DeviceID  string            `json:"deviceId,omitempty"`
	UserID    string            `json:"userId,omitempty"`
	TenantID  string            `json:"tenantId,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// NormalizeScope trims every identifier and label, drops labels with a
// blank key or value, and sets InstallID to [LocalInstallID] when no
// install, device, user, or tenant is given.
func NormalizeScope(scope Scope) Scope {
	scope.InstallID = strings.TrimSpace(scope.InstallID)
	scope.DeviceID = strings.TrimSpace(scope.DeviceID)
	scope.UserID = strings.TrimSpace(scope.UserID)
	scope.TenantID = strings.TrimSpace(scope.TenantID)
	if scope.InstallID == "" && scope.DeviceID == "" && scope.UserID == "" && scope.TenantID == "" {
		scope.InstallID = LocalInstallID
	}
	if len(scope.Labels) > 0 {
		labels := make(map[string]string, len(scope.Labels))
		for key, value := range scope.Labels {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			labels[key] = value
		}
		scope.Labels = labels
	}
	return scope
}

// Key returns a stable, collision-free string identity for the normalized
// scope: "tenant:", "user:", "device:", "install:", and sorted
// "label:k=v" parts joined by "|", each query-escaped so separators inside
// values cannot collide with the delimiters.
func (s Scope) Key() string {
	s = NormalizeScope(s)
	parts := make([]string, 0, 5)
	if s.TenantID != "" {
		parts = append(parts, "tenant:"+escapeScopeKeyPart(s.TenantID))
	}
	if s.UserID != "" {
		parts = append(parts, "user:"+escapeScopeKeyPart(s.UserID))
	}
	if s.DeviceID != "" {
		parts = append(parts, "device:"+escapeScopeKeyPart(s.DeviceID))
	}
	if s.InstallID != "" {
		parts = append(parts, "install:"+escapeScopeKeyPart(s.InstallID))
	}
	if len(s.Labels) > 0 {
		keys := make([]string, 0, len(s.Labels))
		for key := range s.Labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, "label:"+escapeScopeKeyPart(key)+"="+escapeScopeKeyPart(s.Labels[key]))
		}
	}
	return strings.Join(parts, "|")
}

func escapeScopeKeyPart(value string) string {
	return url.QueryEscape(value)
}

type scopeContextKey struct{}

// WithScope returns a child of ctx carrying the normalized scope; a nil
// ctx starts from [context.Background]. Stores read it back with
// [ScopeFromContext].
func WithScope(ctx context.Context, scope Scope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, scopeContextKey{}, NormalizeScope(scope))
}

// ScopeFromContext returns the normalized scope stored by [WithScope], or
// the normalized local default when ctx is nil or carries none.
func ScopeFromContext(ctx context.Context) Scope {
	if ctx == nil {
		return NormalizeScope(Scope{})
	}
	if scope, ok := ctx.Value(scopeContextKey{}).(Scope); ok {
		return NormalizeScope(scope)
	}
	return NormalizeScope(Scope{})
}

// ScopePolicy states which scope identity a backend requires before it
// stores or reads user data. The zero value behaves like [ScopeOptional].
type ScopePolicy string

// Scope policies a backend can declare.
const (
	// ScopeOptional accepts any scope, including the local default.
	ScopeOptional ScopePolicy = "optional"
	// ScopeUserRequired rejects scopes without a UserID.
	ScopeUserRequired ScopePolicy = "user-required"
	// ScopeTenantRequired rejects scopes without a TenantID.
	ScopeTenantRequired ScopePolicy = "tenant-required"
)

// Validate normalizes scope and returns [ErrScopeUserRequired] or
// [ErrScopeTenantRequired] when the identity the policy requires is
// missing. Unknown policies, including the zero value, accept every scope.
func (p ScopePolicy) Validate(scope Scope) error {
	scope = NormalizeScope(scope)
	switch p {
	case ScopeOptional:
	case ScopeUserRequired:
		if scope.UserID == "" {
			return ErrScopeUserRequired
		}
	case ScopeTenantRequired:
		if scope.TenantID == "" {
			return ErrScopeTenantRequired
		}
	}
	return nil
}
