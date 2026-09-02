package live

import "errors"

// Sentinel errors shared by every LiveProvider implementation. Providers
// wrap them with their own prefix, so hosts test with errors.Is instead of
// matching provider-specific strings.
var (
	// ErrNotConnected is returned by session operations before Connect
	// succeeded or after Close.
	ErrNotConnected = errors.New("speechkit live: not connected")
	// ErrSessionNotReady is returned when the transport is up but the
	// provider has not yet acknowledged the session.
	ErrSessionNotReady = errors.New("speechkit live: session is not ready")
	// ErrMissingAPIKey is returned by Connect when the provider requires an
	// API key and none was configured.
	ErrMissingAPIKey = errors.New("speechkit live: APIKey is required")
	// ErrMissingEndpoint is returned by Connect when the provider requires an
	// explicit endpoint and none was configured.
	ErrMissingEndpoint = errors.New("speechkit live: Endpoint is required")
	// ErrNoResumableSession is returned by Resume when the provider has no
	// session id to resume.
	ErrNoResumableSession = errors.New("speechkit live: no resumable session id")
)
