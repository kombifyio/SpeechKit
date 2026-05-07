//go:build linux

package storageauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

func ContextWithRequestOwner(ctx context.Context, r *http.Request) context.Context {
	id := middleware.IdentityFromContext(r.Context())
	return store.WithRecordOwner(ctx, store.RecordOwner{
		UserID: id.UserID,
		OrgID:  id.OrgID,
		Source: id.Source,
	})
}

func ListOptsForIdentity(r *http.Request, limit int) store.ListOpts {
	id := middleware.IdentityFromContext(r.Context())
	opts := store.ListOpts{Limit: limit}
	if id.Role == "admin" {
		opts.IncludeAllOwners = true
		opts.IncludeOwnerless = true
		return opts
	}
	opts.OwnerUserID = strings.TrimSpace(id.UserID)
	opts.OwnerOrgID = strings.TrimSpace(id.OrgID)
	return opts
}

func CanReadOwned(r *http.Request, ownerUserID, ownerOrgID string) bool {
	id := middleware.IdentityFromContext(r.Context())
	if id.Role == "admin" {
		return true
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	ownerOrgID = strings.TrimSpace(ownerOrgID)
	if ownerUserID == "" && ownerOrgID == "" {
		return false
	}
	return ownerUserID == strings.TrimSpace(id.UserID) && ownerOrgID == strings.TrimSpace(id.OrgID)
}
