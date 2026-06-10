package assist

import (
	"context"
	"strings"
	"testing"
)

func TestComposeAssistContext(t *testing.T) {
	tests := []struct {
		name string
		opts ProcessOpts
		want string
	}{
		{
			name: "empty",
			opts: ProcessOpts{},
			want: "",
		},
		{
			name: "free context only",
			opts: ProcessOpts{Context: "Speaker 1: hi"},
			want: "Speaker 1: hi",
		},
		{
			name: "app only",
			opts: ProcessOpts{ActiveApp: "Code"},
			want: "Active application: Code",
		},
		{
			name: "title only",
			opts: ProcessOpts{WindowTitle: "main.go — speechkit"},
			want: "Active window: main.go — speechkit",
		},
		{
			name: "app and title",
			opts: ProcessOpts{ActiveApp: "chrome", WindowTitle: "Inbox (2)"},
			want: "Active application: chrome\nActive window: Inbox (2)",
		},
		{
			name: "window block precedes free context",
			opts: ProcessOpts{ActiveApp: "Code", WindowTitle: "main.go", Context: "Speaker 1: hi"},
			want: "Active application: Code\nActive window: main.go\nSpeaker 1: hi",
		},
		{
			name: "whitespace-only fields are dropped",
			opts: ProcessOpts{ActiveApp: "   ", WindowTitle: "\t", Context: "  "},
			want: "",
		},
		{
			name: "vocabulary hint only",
			opts: ProcessOpts{VocabularyHint: "Prefer these terms: Kombify."},
			want: "Prefer these terms: Kombify.",
		},
		{
			name: "vocabulary hint appended last",
			opts: ProcessOpts{ActiveApp: "Code", Context: "Speaker 1: hi", VocabularyHint: "Prefer these terms: Kombify."},
			want: "Active application: Code\nSpeaker 1: hi\nPrefer these terms: Kombify.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composeAssistContext(tt.opts); got != tt.want {
				t.Fatalf("composeAssistContext() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestProcessFoldsWindowContextIntoToolCall verifies the structured
// active-window fields reach the executor as a composed Context block,
// with the window block ahead of any free-form context.
func TestProcessFoldsWindowContextIntoToolCall(t *testing.T) {
	executor := &mockToolExecutor{
		result: ToolResult{Text: "ok", Action: "execute", Locale: "en"},
	}
	pipeline := NewPipeline(nil, executor, nil, false)

	_, err := pipeline.Process(context.Background(), "copy last", ProcessOpts{
		Locale:      "en",
		ActiveApp:   "Code",
		WindowTitle: "main.go",
		Context:     "Speaker 1: hi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	got := executor.call.Context
	want := "Active application: Code\nActive window: main.go\nSpeaker 1: hi"
	if got != want {
		t.Fatalf("ToolCall.Context = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "Active application: Code") {
		t.Fatalf("window block should lead the context, got %q", got)
	}
}
