// Package claimstore provides the durable at-most-once request ledger used by
// the local SpeechKit to Home Assistant bridge.
//
// A claim is committed before an outbound Home Assistant request may start.
// Existing non-terminal claims are deliberately never re-dispatched: after a
// crash the store cannot know whether Home Assistant applied the command, so
// returning an indeterminate outcome is safer than duplicating a side effect.
//
// The ledger stores request fingerprints and a small, allow-listed replay
// result. It never stores the command text, pairing credentials, Home
// Assistant credentials, raw Home Assistant payloads, or synthesized audio.
package claimstore
