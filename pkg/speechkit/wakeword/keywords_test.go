package wakeword

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testTokens = "testdata/tokens.txt"

func TestEncodeKeywords(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single word with boost+label", "KOMBIFY :2.0 @kombify", "▁KOM B IF Y :2.0 @kombify"},
		{"two words with label", "HEY KOMBIFY @hey_kombify", "▁HE Y ▁KOM B IF Y @hey_kombify"},
		{"lowercase is uppercased", "hey kombify @x", "▁HE Y ▁KOM B IF Y @x"},
		{"already encoded passes through", "▁KOM B IF Y :2.0 @kombify", "▁KOM B IF Y :2.0 @kombify"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EncodeKeywords(testTokens, []string{tc.in})
			if err != nil {
				t.Fatalf("EncodeKeywords(%q) error: %v", tc.in, err)
			}
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("EncodeKeywords(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEncodeKeywordsUnencodable(t *testing.T) {
	if _, err := EncodeKeywords(testTokens, []string{"ZEBRA @z"}); err == nil {
		t.Fatal("expected error for phrase with out-of-vocabulary characters, got nil")
	}
}

func TestValidateKeywordsFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"tokenized ok", "▁COM B IF Y :2.0 @kombify\n", false},
		{"comment then tokenized", "# a comment\n\n▁COM B IF Y @kombify\n", false},
		{"raw text rejected", "kombify\n", true},
		{"comment only rejected", "# only comments\n\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKeywordsFile(write(tc.name+".txt", tc.content))
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.name == "raw text rejected" && err != nil && !strings.Contains(err.Error(), "raw text") {
				t.Fatalf("error should mention raw text, got: %v", err)
			}
		})
	}
}
