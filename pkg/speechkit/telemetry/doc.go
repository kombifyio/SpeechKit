// Package telemetry records named SpeechKit framework outcomes on the active
// OpenTelemetry span. It is the only place in the public SDK that imports
// OpenTelemetry, which keeps the root speechkit package free of that
// dependency (boundary rule 7).
//
// With no TracerProvider installed (the local-only default) [RecordOutcome]
// is a zero-cost no-op.
//
// Stability: Experimental — may change in any release.
package telemetry
