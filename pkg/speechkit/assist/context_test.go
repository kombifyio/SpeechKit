package assist

import "testing"

func TestComposeContext(t *testing.T) {
	tests := []struct {
		name  string
		parts ContextParts
		want  string
	}{
		{
			name:  "empty",
			parts: ContextParts{},
			want:  "",
		},
		{
			name:  "free context only",
			parts: ContextParts{Context: "Speaker 1: hi"},
			want:  "Speaker 1: hi",
		},
		{
			name:  "app only",
			parts: ContextParts{ActiveApp: "Code"},
			want:  "Active application: Code",
		},
		{
			name:  "title only",
			parts: ContextParts{WindowTitle: "main.go — speechkit"},
			want:  "Active window: main.go — speechkit",
		},
		{
			name:  "app and title",
			parts: ContextParts{ActiveApp: "chrome", WindowTitle: "Inbox (2)"},
			want:  "Active application: chrome\nActive window: Inbox (2)",
		},
		{
			name:  "window block precedes free context",
			parts: ContextParts{ActiveApp: "Code", WindowTitle: "main.go", Context: "Speaker 1: hi"},
			want:  "Active application: Code\nActive window: main.go\nSpeaker 1: hi",
		},
		{
			name:  "whitespace-only fields are dropped",
			parts: ContextParts{ActiveApp: "   ", WindowTitle: "\t", Context: "  "},
			want:  "",
		},
		{
			name:  "vocabulary hint only",
			parts: ContextParts{VocabularyHint: "Prefer these terms: Kombify."},
			want:  "Prefer these terms: Kombify.",
		},
		{
			name:  "vocabulary hint appended last",
			parts: ContextParts{ActiveApp: "Code", Context: "Speaker 1: hi", VocabularyHint: "Prefer these terms: Kombify."},
			want:  "Active application: Code\nSpeaker 1: hi\nPrefer these terms: Kombify.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComposeContext(tt.parts); got != tt.want {
				t.Fatalf("ComposeContext() = %q, want %q", got, tt.want)
			}
		})
	}
}
