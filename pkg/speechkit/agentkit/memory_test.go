package agentkit

import (
	"context"
	"sort"
	"sync"
	"testing"
)

func TestInMemory_GetSetDelete(t *testing.T) {
	ctx := context.Background()
	m := NewInMemory()

	if _, ok, _ := m.Get(ctx, "missing"); ok {
		t.Fatalf("missing key should not be ok")
	}

	if err := m.Set(ctx, "k", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, ok, err := m.Get(ctx, "k")
	if err != nil || !ok || v != "v" {
		t.Fatalf("Get(k) = %q, %v, %v", v, ok, err)
	}

	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := m.Get(ctx, "k"); ok {
		t.Fatalf("after Delete, Get should not find key")
	}
}

func TestInMemory_ListPrefix(t *testing.T) {
	ctx := context.Background()
	m := NewInMemory()
	for _, k := range []string{"sess1/a", "sess1/b", "sess2/a"} {
		_ = m.Set(ctx, k, "x")
	}
	keys, err := m.List(ctx, "sess1/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(keys)
	want := []string{"sess1/a", "sess1/b"}
	if len(keys) != len(want) {
		t.Fatalf("List len = %d, want %d (%v)", len(keys), len(want), keys)
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("List[%d] = %q, want %q", i, k, want[i])
		}
	}

	all, _ := m.List(ctx, "")
	if len(all) != 3 {
		t.Errorf("List(\"\") len = %d, want 3", len(all))
	}
}

func TestInMemory_Concurrent(t *testing.T) {
	ctx := context.Background()
	m := NewInMemory()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			key := "k"
			_ = m.Set(ctx, key, "v")
			_, _, _ = m.Get(ctx, key)
		})
	}
	wg.Wait()
	if _, ok, _ := m.Get(ctx, "k"); !ok {
		t.Fatalf("expected key after concurrent writers")
	}
}
