package storage

import "strings"

func NormalizeIdentifier(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
