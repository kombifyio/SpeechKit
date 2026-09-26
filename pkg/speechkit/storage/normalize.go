package storage

import "strings"

// NormalizeIdentifier trims value and lowercases it; storage identifiers
// such as backend names are compared in this form.
func NormalizeIdentifier(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
