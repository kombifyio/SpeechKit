// Package testutil holds test helpers shared across SpeechKit's
// kernel packages — temp-dir builders, fake clocks, identity
// injectors. Production code must not import this package; the
// build will fail at link time if it does.
//
// Audit 2026-05-24 maintainability sweep.
package testutil
