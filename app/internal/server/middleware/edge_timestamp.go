//go:build linux

package middleware

import (
	"strconv"
	"time"
)

// edgeHMACMaxSkew bounds how far an edge signing timestamp may deviate from
// server time. It is the freshness window of the signed edge envelope.
const edgeHMACMaxSkew = 5 * time.Minute

// edgeTimestampFresh reports whether ts (Unix seconds as a decimal string) is
// within edgeHMACMaxSkew of now. A malformed timestamp is rejected so a
// garbage value cannot bypass the freshness check.
func edgeTimestampFresh(ts string, now time.Time) bool {
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	delta := now.Sub(time.Unix(secs, 0))
	if delta < 0 {
		delta = -delta
	}
	return delta <= edgeHMACMaxSkew
}
