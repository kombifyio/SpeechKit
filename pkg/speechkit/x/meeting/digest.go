package meeting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrDigestNotJSON is returned when a model answer carries no JSON object or
// array at all — empty output, or prose without a JSON body.
var ErrDigestNotJSON = errors.New("meeting digest is not JSON")

// MergeDigests combines the digests of one segment's parts into a single
// digest: list fields (chronology, topics, decisions, …) are concatenated in
// part order, any other field keeps the value of the first part that set it.
// Every input must be a JSON object.
func MergeDigests(digests []string) (string, error) {
	merged := map[string]any{}
	for index, digest := range digests {
		var fields map[string]any
		if err := json.Unmarshal([]byte(digest), &fields); err != nil {
			return "", fmt.Errorf("merge digests: part %d: %w", index+1, err)
		}
		for key, value := range fields {
			existing, seen := merged[key]
			if !seen {
				merged[key] = value
				continue
			}
			existingList, existingIsList := existing.([]any)
			list, isList := value.([]any)
			if existingIsList && isList {
				merged[key] = append(existingList, list...)
			}
		}
	}
	if len(merged) == 0 {
		return "", ErrDigestNotJSON
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("merge digests: %w", err)
	}
	return string(out), nil
}

// NormalizeDigestJSON extracts the JSON document from a model answer and
// returns it compacted. Local models routinely wrap the requested JSON in a
// fenced code block or prefix it with a sentence; storing that verbatim as a
// "ready" digest fed fenced text into every rollup and write-up downstream
// (96 of 99 stored digests were unparsable on 2026-09-03). Empty or
// non-JSON answers return ErrDigestNotJSON so the batch is retried instead of
// being marked ready with nothing in it.
func NormalizeDigestJSON(text string) (string, error) {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return "", ErrDigestNotJSON
	}
	if fenced := fencedBlock(candidate); fenced != "" {
		candidate = fenced
	}
	start := strings.IndexAny(candidate, "{[")
	if start < 0 {
		return "", ErrDigestNotJSON
	}
	candidate = candidate[start:]
	if end := strings.LastIndexAny(candidate, "}]"); end >= 0 && end < len(candidate)-1 {
		candidate = candidate[:end+1]
	}
	if !json.Valid([]byte(candidate)) {
		return "", ErrDigestNotJSON
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(candidate)); err != nil {
		return "", ErrDigestNotJSON
	}
	return compact.String(), nil
}

// fencedBlock returns the body of the first ``` fenced block, or "" when the
// text has no complete fence.
func fencedBlock(text string) string {
	start := strings.Index(text, "```")
	if start < 0 {
		return ""
	}
	rest := text[start+3:]
	if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
		rest = rest[newline+1:]
	} else if space := strings.IndexByte(rest, ' '); space >= 0 {
		// ```json {...}``` on one line: drop the language tag.
		rest = rest[space+1:]
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}
