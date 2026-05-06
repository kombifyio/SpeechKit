//go:build kombify

// This build-tag seam is intentionally inert.
//
// Do not add a private blank import here. The OSS default build must keep
// working without private module access, and downstream product integrations
// must remain separate from the SpeechKit framework checkout.
package kombify

// Blocked on private module publication:
// import _ "github.com/kombifyio/kombify-speechkit"
