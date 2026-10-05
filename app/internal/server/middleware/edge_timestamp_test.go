//go:build linux

package middleware

import (
	"strconv"
	"testing"
	"time"
)

func TestEdgeTimestampFresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name string
		ts   string
		want bool
	}{
		{"exact now", "1700000000", true},
		{"within window past", strconv.FormatInt(now.Add(-4*time.Minute).Unix(), 10), true},
		{"within window future", strconv.FormatInt(now.Add(4*time.Minute).Unix(), 10), true},
		{"too old", strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10), false},
		{"too far future", strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10), false},
		{"malformed", "abc", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := edgeTimestampFresh(tt.ts, now); got != tt.want {
				t.Fatalf("edgeTimestampFresh(%q) = %v, want %v", tt.ts, got, tt.want)
			}
		})
	}
}
