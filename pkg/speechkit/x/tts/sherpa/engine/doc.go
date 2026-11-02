// Package engine is the native half of pkg/speechkit/x/tts/sherpa: the
// [Engine] that loads the bundled Piper VITS voices and speaks with them
// through sherpa-onnx.
//
// It links the sherpa-onnx runtime through cgo, so only a process meant to
// host it imports it: the speechkit-tts-sherpa sidecar. A desktop or server
// host talks to that sidecar over HTTP and imports the pure-Go parent package
// alone, which keeps a second ONNX runtime out of its process. Without cgo
// [New] returns [ErrCgoRequired].
//
// Stability: Experimental — may change in any release.
package engine

import "errors"

// ErrCgoRequired is returned by [New] in a build without cgo, where the
// sherpa-onnx native library is not linked.
var ErrCgoRequired = errors.New("sherpa tts: speech synthesis requires a cgo build with the sherpa-onnx runtime")
