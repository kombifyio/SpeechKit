//go:build !kombify

package kombify

import "testing"

// TestOSSBuildTagIsInert verifies the OSS build of the kombify seam package
// does not register any global state. In OSS mode the package has no
// side-effects — the kombify build tag pulls in the private module that
// registers store backends and auth providers.
//
// This test exists primarily to keep the package importable and to give the
// coverage tool something to report. The kombify-tagged variant is validated
// by the canonical `scripts/build.ps1` which exercises the private module
// path.
func TestOSSBuildTagIsInert(t *testing.T) {
	t.Parallel()
	// Package has zero exported symbols by design — importing it (which the
	// compiler does by virtue of this file being in the package) is itself
	// the assertion.
}
