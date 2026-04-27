//go:build linux

package core

import (
	"context"
	"os/signal"
	"syscall"
)

// NotifySignals wraps signal.NotifyContext for SIGINT + SIGTERM. Calling the
// returned stop func is idempotent and safe from defer.
func NotifySignals(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}
