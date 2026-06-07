//go:build linux

package storageauth

import (
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

func TestCanReadOwned(t *testing.T) {
	tests := []struct {
		name        string
		id          middleware.Identity
		ownerUserID string
		ownerOrgID  string
		want        bool
	}{
		{
			name: "admin reads anything",
			id:   middleware.Identity{UserID: "u1", OrgID: "o1", Role: "admin"},
			// even a record owned by someone else
			ownerUserID: "other", ownerOrgID: "otherorg",
			want: true,
		},
		{
			name:        "admin reads ownerless",
			id:          middleware.Identity{UserID: "u1", OrgID: "o1", Role: "admin"},
			ownerUserID: "", ownerOrgID: "",
			want: true,
		},
		{
			name:        "owner reads own record",
			id:          middleware.Identity{UserID: "u1", OrgID: "o1"},
			ownerUserID: "u1", ownerOrgID: "o1",
			want: true,
		},
		{
			name:        "non-owner cannot read other user record",
			id:          middleware.Identity{UserID: "u1", OrgID: "o1"},
			ownerUserID: "u2", ownerOrgID: "o1",
			want: false,
		},
		{
			name:        "non-owner cannot read other org record",
			id:          middleware.Identity{UserID: "u1", OrgID: "o1"},
			ownerUserID: "u1", ownerOrgID: "o2",
			want: false,
		},
		{
			name:        "non-admin cannot read ownerless record",
			id:          middleware.Identity{UserID: "u1", OrgID: "o1"},
			ownerUserID: "", ownerOrgID: "",
			want: false,
		},
		{
			name:        "whitespace owner is trimmed before compare",
			id:          middleware.Identity{UserID: "u1", OrgID: "o1"},
			ownerUserID: "  u1  ", ownerOrgID: " o1 ",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/v1/resource", nil)
			r = r.WithContext(middleware.InjectIdentityForTest(r.Context(), tt.id))
			if got := CanReadOwned(r, tt.ownerUserID, tt.ownerOrgID); got != tt.want {
				t.Fatalf("CanReadOwned(owner=%q/%q, id=%+v) = %v, want %v",
					tt.ownerUserID, tt.ownerOrgID, tt.id, got, tt.want)
			}
		})
	}
}

func TestListOptsForIdentity(t *testing.T) {
	t.Run("admin gets unrestricted opts", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/v1/resource", nil)
		r = r.WithContext(middleware.InjectIdentityForTest(r.Context(),
			middleware.Identity{UserID: "admin", OrgID: "o1", Role: "admin"}))

		opts := ListOptsForIdentity(r, 25)
		if !opts.IncludeAllOwners || !opts.IncludeOwnerless {
			t.Fatalf("admin must see all owners + ownerless, got %+v", opts)
		}
		if opts.OwnerUserID != "" || opts.OwnerOrgID != "" {
			t.Fatalf("admin opts must not be scoped to an owner, got %+v", opts)
		}
		if opts.Limit != 25 {
			t.Fatalf("limit not propagated: got %d", opts.Limit)
		}
	})

	t.Run("non-admin is scoped to own owner", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/v1/resource", nil)
		r = r.WithContext(middleware.InjectIdentityForTest(r.Context(),
			middleware.Identity{UserID: "u1", OrgID: "o1"}))

		opts := ListOptsForIdentity(r, 10)
		if opts.IncludeAllOwners || opts.IncludeOwnerless {
			t.Fatalf("non-admin must not see other owners, got %+v", opts)
		}
		if opts.OwnerUserID != "u1" || opts.OwnerOrgID != "o1" {
			t.Fatalf("non-admin opts must be scoped to caller, got %+v", opts)
		}
	})
}

func TestContextWithRequestOwner(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/resource", nil)
	r = r.WithContext(middleware.InjectIdentityForTest(r.Context(),
		middleware.Identity{UserID: "u1", OrgID: "o1", Source: "bearer"}))

	ctx := ContextWithRequestOwner(r.Context(), r)
	owner, ok := store.RecordOwnerFromContext(ctx)
	if !ok {
		t.Fatal("expected record owner to be present in context")
	}
	if owner.UserID != "u1" || owner.OrgID != "o1" || owner.Source != "bearer" {
		t.Fatalf("owner mismatch: got %+v", owner)
	}
}
