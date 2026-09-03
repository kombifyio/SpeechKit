package meeting

import (
	"errors"
	"testing"
)

func TestNormalizeDigestJSONAcceptsTheShapesLocalModelsProduce(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain object", `{"topics":["a"]}`, `{"topics":["a"]}`},
		{"pretty printed", "{\n  \"topics\": [\n    \"a\"\n  ]\n}", `{"topics":["a"]}`},
		{"fenced with language tag", "```json\n{\"topics\": [\"a\"]}\n```", `{"topics":["a"]}`},
		{"fenced one line", "```json {\"topics\": [\"a\"]}```", `{"topics":["a"]}`},
		{"fenced without tag", "```\n{\"topics\":[\"a\"]}\n```", `{"topics":["a"]}`},
		{"leading sentence", "Here is the digest:\n{\"topics\":[\"a\"]}\nHope this helps.", `{"topics":["a"]}`},
		{"array", "[1, 2]", `[1,2]`},
	}
	for _, tc := range cases {
		got, err := NormalizeDigestJSON(tc.in)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeDigestJSONRejectsEmptyAndNonJSONAnswers(t *testing.T) {
	for _, in := range []string{"", "   \n", "The meeting covered budgets.", "```json\n```", "{\"topics\": [\"unterminated\"", "```json\nnot json\n```"} {
		if _, err := NormalizeDigestJSON(in); !errors.Is(err, ErrDigestNotJSON) {
			t.Errorf("%q: err = %v, want ErrDigestNotJSON", in, err)
		}
	}
}
