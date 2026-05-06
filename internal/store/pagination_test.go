package store

import "testing"

func TestNormalizedListPaginationDefaultsAndClampsOffset(t *testing.T) {
	limit, offset := normalizedListPagination(ListOpts{Limit: 0, Offset: -10})

	if limit != defaultListLimit {
		t.Fatalf("limit = %d, want %d", limit, defaultListLimit)
	}
	if offset != 0 {
		t.Fatalf("offset = %d, want 0", offset)
	}
}

func TestNormalizedListPaginationPreservesExplicitValues(t *testing.T) {
	limit, offset := normalizedListPagination(ListOpts{Limit: 7, Offset: 3})

	if limit != 7 || offset != 3 {
		t.Fatalf("pagination = (%d, %d), want (7, 3)", limit, offset)
	}
}
