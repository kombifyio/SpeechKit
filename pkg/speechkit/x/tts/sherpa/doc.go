// Package sherpa is the pure-Go half of the on-device text-to-speech engine
// that runs Piper VITS voices through sherpa-onnx.
//
// The native half lives in the engine subpackage and links the sherpa-onnx
// runtime through cgo, so only the speechkit-tts-sherpa sidecar imports it. A
// desktop or server host talks to that sidecar over HTTP and imports this
// package alone, which keeps a second ONNX runtime out of its process. The
// HTTP contract is the sidecar's own: POST /v1/audio/speech with JSON text and
// locale, answering a WAV body.
//
// Stability: Experimental — may change in any release.
package sherpa
