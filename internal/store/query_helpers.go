package store

import (
	"fmt"
	"strings"
)

func sqlInPlaceholders(dialect string, start, count int) (string, error) {
	if count <= 0 {
		return "", fmt.Errorf("placeholder count must be positive")
	}
	if start <= 0 {
		return "", fmt.Errorf("placeholder start must be positive")
	}
	parts := make([]string, count)
	switch dialect {
	case "postgres":
		for i := range parts {
			parts[i] = fmt.Sprintf("$%d", start+i)
		}
	case "sqlite":
		for i := range parts {
			parts[i] = "?"
		}
	default:
		return "", fmt.Errorf("unsupported SQL dialect %q", dialect)
	}
	return strings.Join(parts, ", "), nil
}

func compactPositiveInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendInt64Args(args []any, values []int64) []any {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}
