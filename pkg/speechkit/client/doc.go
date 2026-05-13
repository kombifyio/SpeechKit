// Package client provides a typed HTTP client for talking to a remote
// SpeechKit Server (the `cmd/speechkit-server` Linux container or any
// compatible deployment).
//
// Use it from any Go program — desktop, server, or test harness — that
// wants to consume Dictation, Assist, or Voice Agent endpoints over the
// network without embedding the kernel.
//
// # Auth
//
// The client supports the same bearer-token flow as the server: pass the
// token via [Options.Token] (or the equivalent env var documented for
// the server you are calling). For local trusted setups, the server may
// be configured without a token; the client tolerates that mode.
//
// # Timeouts and retries
//
// Per-request timeouts are configured via [Options.Timeout]. The client
// does not retry by default — host apps own the retry policy because the
// right behavior differs between idempotent reads (safe to retry) and
// non-idempotent writes (which may need a higher-level dedupe key).
package client
