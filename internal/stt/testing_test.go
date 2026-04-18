package stt

import "github.com/kombifyio/SpeechKit/internal/netsec"

// testValidation permits httptest.Server URLs (http://127.0.0.1:RAND) and
// loopback addresses that appear in unit tests. Production constructors
// keep the strict netsec defaults (public https only).
var testValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}
