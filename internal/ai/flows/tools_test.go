package flows

import (
	"context"
	"errors"
	"testing"

	"github.com/firebase/genkit/go/genkit"
)

func TestDefineClipboardReadTool(t *testing.T) {
	g := genkit.Init(context.Background())
	tool := DefineClipboardReadTool(g, func() (string, error) {
		return "clipboard content", nil
	})
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
	if tool.Name() != "readClipboard" {
		t.Errorf("name = %q", tool.Name())
	}
}

func TestDefineClipboardWriteTool(t *testing.T) {
	var written string
	g := genkit.Init(context.Background())
	tool := DefineClipboardWriteTool(g, func(text string) error {
		written = text
		return nil
	})
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
	if tool.Name() != "writeClipboard" {
		t.Errorf("name = %q", tool.Name())
	}
	_ = written
}

func TestDefineClipboardReadTool_Error(t *testing.T) {
	g := genkit.Init(context.Background())
	tool := DefineClipboardReadTool(g, func() (string, error) {
		return "", errors.New("clipboard locked")
	})
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
}

func TestDefineSummarizeTool(t *testing.T) {
	g := genkit.Init(context.Background())
	summarizeFlow := DefineSummarizeFlow(g, nil)
	tool := DefineSummarizeTool(g, summarizeFlow)
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
	if tool.Name() != "summarize" {
		t.Errorf("name = %q", tool.Name())
	}
}
