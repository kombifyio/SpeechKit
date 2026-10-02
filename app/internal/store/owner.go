package store

import (
	"context"
	"strings"
)

// RecordOwner captures the caller identity persisted with server-owned
// records. Store does not import server middleware, so HTTP handlers translate
// auth identity into this small value.
type RecordOwner struct {
	UserID string
	OrgID  string
	Source string
}

type recordOwnerCtxKey struct{}

func WithRecordOwner(ctx context.Context, owner RecordOwner) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	owner.UserID = strings.TrimSpace(owner.UserID)
	owner.OrgID = strings.TrimSpace(owner.OrgID)
	owner.Source = strings.TrimSpace(owner.Source)
	return context.WithValue(ctx, recordOwnerCtxKey{}, owner)
}

func RecordOwnerFromContext(ctx context.Context) (RecordOwner, bool) {
	if ctx == nil {
		return RecordOwner{}, false
	}
	owner, ok := ctx.Value(recordOwnerCtxKey{}).(RecordOwner)
	if !ok {
		return RecordOwner{}, false
	}
	owner.UserID = strings.TrimSpace(owner.UserID)
	owner.OrgID = strings.TrimSpace(owner.OrgID)
	owner.Source = strings.TrimSpace(owner.Source)
	return owner, owner.UserID != "" || owner.OrgID != "" || owner.Source != ""
}
