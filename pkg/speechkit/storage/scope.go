package storage

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
)

const LocalInstallID = "local"

var (
	ErrScopeUserRequired   = errors.New("speechkit storage: user scope is required")
	ErrScopeTenantRequired = errors.New("speechkit storage: tenant scope is required")
)

type Scope struct {
	InstallID string            `json:"installId,omitempty"`
	DeviceID  string            `json:"deviceId,omitempty"`
	UserID    string            `json:"userId,omitempty"`
	TenantID  string            `json:"tenantId,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

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

func WithScope(ctx context.Context, scope Scope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, scopeContextKey{}, NormalizeScope(scope))
}

func ScopeFromContext(ctx context.Context) Scope {
	if ctx == nil {
		return NormalizeScope(Scope{})
	}
	if scope, ok := ctx.Value(scopeContextKey{}).(Scope); ok {
		return NormalizeScope(scope)
	}
	return NormalizeScope(Scope{})
}

type ScopePolicy string

const (
	ScopeOptional       ScopePolicy = "optional"
	ScopeUserRequired   ScopePolicy = "user-required"
	ScopeTenantRequired ScopePolicy = "tenant-required"
)

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
