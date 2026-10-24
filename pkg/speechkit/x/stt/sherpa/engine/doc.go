// Package engine is the native half of pkg/speechkit/x/stt/sherpa: the
// [Recognizer] that loads a sherpa-onnx nemo_transducer model and decodes
// audio with it.
//
// It links the sherpa-onnx runtime through cgo, so only a process meant to
// host it imports it: the speechkit-stt-sherpa sidecar. A desktop or server
// host talks to that sidecar over HTTP and imports the pure-Go parent package
// alone, which keeps a second ONNX runtime out of its process. Without cgo
// [NewRecognizer] returns [ErrCgoRequired].
//
// Stability: Experimental — may change in any release.
package engine

import "errors"

// ErrCgoRequired is returned by [NewRecognizer] in a build without cgo, where
// the sherpa-onnx native library is not linked.
var ErrCgoRequired = errors.New("sherpa: speech recognition requires a cgo build with the sherpa-onnx runtime")
