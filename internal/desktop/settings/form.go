package settings

import (
	"net/http"
	"strconv"
	"strings"
)

func BoolValue(req *http.Request, key string, fallback bool) bool {
	raw := TrimmedValue(req, key)
	if raw == "" {
		return fallback
	}
	return raw == "1"
}

func IntValue(req *http.Request, key string, fallback int) int {
	raw := TrimmedValue(req, key)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func NonNegativeIntValue(req *http.Request, key string, fallback int) int {
	parsed := IntValue(req, key, fallback)
	if parsed < 0 {
		return fallback
	}
	return parsed
}

func TrimmedValue(req *http.Request, key string) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.FormValue(key))
}

func Includes(req *http.Request, key string) bool {
	if req == nil || req.PostForm == nil {
		return false
	}
	_, ok := req.PostForm[key]
	return ok
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ValueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func NormalizeMultiline(input string) string {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.TrimSpace(normalized)
}
