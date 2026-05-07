package agentkit

import (
	"context"
	"errors"
	"testing"
)

func TestRegistryDefinitionsAreSortedAndRejectDuplicates(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&FuncTool{ToolName: "zeta", ToolDescription: "last"}); err != nil {
		t.Fatalf("register zeta: %v", err)
	}
	if err := registry.Register(&FuncTool{ToolName: "alpha", ToolDescription: "first"}); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := registry.Register(&FuncTool{ToolName: "alpha"}); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateTool", err)
	}

	defs := registry.Definitions()
	if len(defs) != 2 {
		t.Fatalf("definitions = %d, want 2", len(defs))
	}
	if defs[0].Name != "alpha" || defs[1].Name != "zeta" {
		t.Fatalf("definitions not sorted by name: %#v", defs)
	}
}

func TestInMemoryListsKeysByPrefixInStableOrder(t *testing.T) {
	memory := NewInMemory()
	ctx := context.Background()

	for _, item := range []struct {
		key   string
		value string
	}{
		{"session-b/item", "ignored"},
		{"session-a/two", "2"},
		{"session-a/one", "1"},
	} {
		if err := memory.Set(ctx, item.key, item.value); err != nil {
			t.Fatalf("set %s: %v", item.key, err)
		}
	}

	keys, err := memory.List(ctx, "session-a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got, want := len(keys), 2; got != want {
		t.Fatalf("keys = %d, want %d: %#v", got, want, keys)
	}
	if keys[0] != "session-a/one" || keys[1] != "session-a/two" {
		t.Fatalf("keys not sorted by prefix: %#v", keys)
	}
}
