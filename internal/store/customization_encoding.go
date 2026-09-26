package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func jsonList[T ~string](values []T) (string, error) {
	if values == nil {
		return "[]", nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(string(value)); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	data, err := json.Marshal(out)
	return string(data), err
}

func jsonObject(value map[string]any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	data, err := json.Marshal(value)
	return string(data), err
}

func unmarshalStringList(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(firstNonEmpty(raw, "[]")), &out); err != nil {
		return nil
	}
	return out
}

func unmarshalMap(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(firstNonEmpty(raw, "{}")), &out); err != nil {
		return nil
	}
	return out
}

func unmarshalModes(raw string) []speechcustomize.Mode {
	var values []string
	if err := json.Unmarshal([]byte(firstNonEmpty(raw, "[]")), &values); err != nil {
		return nil
	}
	modes := make([]speechcustomize.Mode, 0, len(values))
	for _, value := range values {
		if mode := speechcustomize.NormalizeMode(speechcustomize.Mode(value)); mode != "" {
			modes = append(modes, mode)
		}
	}
	return modes
}

func replacementModes(modes []speechcustomize.Mode) []string {
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		if normalized := speechcustomize.NormalizeMode(mode); normalized != "" {
			out = append(out, string(normalized))
		}
	}
	return out
}

func replacementHasMode(replacement speechcustomize.Replacement, mode speechcustomize.Mode) bool {
	if mode == "" {
		return true
	}
	for _, candidate := range replacement.Modes {
		if speechcustomize.NormalizeMode(candidate) == mode {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeCustomizationSource(source string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		return userDictionarySettingsSource
	}
	return source
}

func isMissingCustomizationTable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "customization_") && (strings.Contains(msg, "no such table") || strings.Contains(msg, "does not exist"))
}
