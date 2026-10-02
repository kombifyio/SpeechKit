package store

import (
	"strings"
	"testing"
)

func TestNormalizedLanguageFilterHelpers(t *testing.T) {
	clauses, args := appendNormalizedLanguageFilter(nil, nil, "de-DE")
	if len(clauses) != 1 || len(args) != 1 {
		t.Fatalf("filter clauses=%v args=%v, want one clause and one arg", clauses, args)
	}
	if !strings.Contains(clauses[0], "?") {
		t.Fatalf("filter clause must use a `?` placeholder (rebound per dialect): %s", clauses[0])
	}
	clauses, args = appendNormalizedLanguageFilter(clauses, args, " ")
	if len(clauses) != 1 || len(args) != 1 {
		t.Fatalf("empty filter should not append: clauses=%v args=%v", clauses, args)
	}
}
