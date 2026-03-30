//go:build kombify

// When built with -tags kombify, this file imports the private kombify-speechkit module.
// That module's init() registers the kombify store backend and auth provider.
//
// To enable: go build -tags kombify ./cmd/speechkit/
// The private module must be available in the Go module cache.
package kombify

// TODO: Uncomment when the private module is published:
// import _ "github.com/kombifyio/kombify-speechkit"
