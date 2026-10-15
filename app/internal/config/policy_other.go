//go:build !windows && !darwin

package config

import (
	"context"
	"time"
)

// ReadPolicyValues returns a zero-value overlay on platforms without a
// machine-policy source (Linux). The Server-Target (Linux container) uses
// this stub so neither the registry nor the managed-preferences reader is
// compiled into the Linux binary.
func ReadPolicyValues() PolicyValues {
	return PolicyValues{Origin: "non-windows"}
}

// watchPolicyChanges does nothing: there is no policy source to watch.
func watchPolicyChanges(context.Context, time.Duration, func()) {}
