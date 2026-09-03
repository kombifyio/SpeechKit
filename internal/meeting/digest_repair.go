package meeting

import (
	"bytes"
	"encoding/json"
	"strings"
)

// RepairTruncatedJSON salvages a JSON answer a model stopped writing before
// the closing brackets — the usual result of an answer budget that was too
// small. It cuts the text back to the last complete value and closes every
// open array and object, so `{"topics":["a","b","c` becomes
// `{"topics":["a","b"]}`. The second result is false when nothing complete
// could be kept or the text is not a truncated JSON document at all.
func RepairTruncatedJSON(text string) (string, bool) {
	candidate := strings.TrimSpace(text)
	if fenced := fencedBlock(candidate); fenced != "" {
		candidate = fenced
	}
	start := strings.IndexAny(candidate, "{[")
	if start < 0 {
		return "", false
	}
	candidate = candidate[start:]
	if json.Valid([]byte(candidate)) {
		return "", false // nothing to repair
	}

	var stack []byte
	var stackAtComplete []byte
	lastComplete := -1
	inString, escaped := false, false
	// pendingString marks the end of a string that may turn out to be an
	// object key; it counts as a complete value only when a delimiter follows
	// — or when the text ends there and the string sat where a value belongs
	// (inside an array, or right after a colon).
	pendingString := -1
	pendingIsValue := false
	afterColon := false
	completeAt := func(end int) {
		lastComplete = end
		stackAtComplete = append(stackAtComplete[:0], stack...)
	}
	inLiteral := false
	for index := 0; index < len(candidate); index++ {
		c := candidate[index]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
				pendingString = index + 1
				pendingIsValue = afterColon || (len(stack) > 0 && stack[len(stack)-1] == '[')
				afterColon = false
			}
			continue
		}
		if inLiteral {
			if c == ',' || c == ']' || c == '}' || c == ' ' || c == '\n' || c == '\r' || c == '\t' {
				inLiteral = false
				completeAt(index)
			} else {
				continue
			}
		}
		switch c {
		case ' ', '\n', '\r', '\t':
			continue
		case ':':
			pendingString = -1 // the string before was a key
			afterColon = true
			continue
		case ',':
			afterColon = false
			if pendingString >= 0 {
				completeAt(pendingString)
				pendingString = -1
			}
		case '{', '[':
			afterColon = false
			stack = append(stack, c)
			pendingString = -1
		case '}', ']':
			afterColon = false
			if pendingString >= 0 {
				completeAt(pendingString)
				pendingString = -1
			}
			if len(stack) == 0 {
				return "", false
			}
			stack = stack[:len(stack)-1]
			if len(stack) > 0 {
				completeAt(index + 1)
			}
		case '"':
			inString = true
			pendingString = -1
		default:
			// number or literal (true/false/null)
			afterColon = false
			inLiteral = true
			pendingString = -1
		}
	}
	// The text ended right after a closed string: keep it when it sat where a
	// value belongs; a key without its colon is dropped with the rest.
	if pendingString >= 0 && pendingIsValue {
		completeAt(pendingString)
	}
	if len(stack) == 0 || lastComplete < 0 {
		return "", false
	}
	prefix := strings.TrimRight(candidate[:lastComplete], " \n\r\t,")
	var out strings.Builder
	out.WriteString(prefix)
	for index := len(stackAtComplete) - 1; index >= 0; index-- {
		if stackAtComplete[index] == '{' {
			out.WriteByte('}')
		} else {
			out.WriteByte(']')
		}
	}
	repaired := out.String()
	if !json.Valid([]byte(repaired)) {
		return "", false
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(repaired)); err != nil {
		return "", false
	}
	return compact.String(), true
}
