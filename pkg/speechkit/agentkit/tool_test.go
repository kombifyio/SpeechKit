package agentkit

import (
	"context"
	"errors"
	"testing"
)

func TestFuncTool_AccessorsAndInvoke(t *testing.T) {
	called := false
	tool := &FuncTool{
		ToolName:        "echo",
		ToolDescription: "Echo back input.",
		ToolSchema: Schema{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
		Fn: func(_ context.Context, args map[string]any) (map[string]any, error) {
			called = true
			return map[string]any{"text": args["text"]}, nil
		},
	}

	if got := tool.Name(); got != "echo" {
		t.Errorf("Name() = %q, want %q", got, "echo")
	}
	if got := tool.Description(); got != "Echo back input." {
		t.Errorf("Description() = %q", got)
	}
	if tool.InputSchema()["type"] != "object" {
		t.Errorf("InputSchema missing type=object")
	}

	out, err := tool.Invoke(context.Background(), map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("Invoke returned err: %v", err)
	}
	if !called {
		t.Fatalf("Fn was not invoked")
	}
	if out["text"] != "hi" {
		t.Errorf("Invoke result = %v, want text=hi", out)
	}
}

func TestFuncTool_NilFn(t *testing.T) {
	tool := &FuncTool{ToolName: "broken"}
	if _, err := tool.Invoke(context.Background(), nil); err == nil {
		t.Fatalf("Invoke with nil Fn should return error")
	}
}

func TestFuncTool_FnError(t *testing.T) {
	want := errors.New("boom")
	tool := &FuncTool{
		ToolName: "errs",
		Fn: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, want
		},
	}
	_, err := tool.Invoke(context.Background(), nil)
	if !errors.Is(err, want) {
		t.Fatalf("Invoke err = %v, want %v", err, want)
	}
}
