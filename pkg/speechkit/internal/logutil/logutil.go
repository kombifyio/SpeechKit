// Package logutil resolves optional per-instance loggers for SpeechKit
// library types. It is the single place under pkg/ allowed to touch the
// process-global slog default.
package logutil

import "log/slog"

// Resolve returns l, or the process default logger when l is nil. Call it at
// log time so a later slog.SetDefault is honoured by instances that were not
// given a logger.
func Resolve(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.Default() //nolint:forbidigo // default fallback
}
