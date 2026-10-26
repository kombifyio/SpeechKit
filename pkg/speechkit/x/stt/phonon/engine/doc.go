// Package engine is the native half of pkg/speechkit/x/stt/phonon: the
// [Recognizer] that runs Fermion Research's five-value Phonon models on the
// CPU without expanding their weights.
//
// It links phonon-2-c, a pure-C inference engine for five-value Parakeet TDT
// models (MIT, https://github.com/eschmidbauer/phonon-2-c, vendored here with
// SpeechKit's portability patches; see LICENSE.phonon-2-c and UPSTREAM.md),
// through cgo. The encoder's linear layers stay in the container's five-value
// form in memory, so a loaded model needs less than half the memory of an
// int8 ONNX export. Only a process meant to host it imports it: the local STT
// sidecar. Without cgo, or on an architecture other than amd64 and arm64,
// [NewRecognizer] returns [ErrCgoRequired].
//
// The amd64 build targets x86-64-v3 (AVX2, FMA, BMI, F16C, MOVBE, LZCNT),
// the arm64 build the dot-product extension; [NewRecognizer] returns
// [ErrUnsupportedCPU] on a processor without them instead of crashing on the
// first illegal instruction.
//
// Stability: Experimental — may change in any release.
package engine

import "errors"

// ErrCgoRequired is returned by [NewRecognizer] in a build without cgo or on
// an architecture the vendored engine is not built for.
var ErrCgoRequired = errors.New("phonon: speech recognition requires a cgo build on amd64 or arm64")

// ErrUnsupportedCPU is returned by [NewRecognizer] when the processor lacks
// the instructions the engine was compiled for (x86-64-v3 on amd64, the
// dot-product extension on arm64).
var ErrUnsupportedCPU = errors.New("phonon: this processor lacks the instructions the five-value engine needs (x86-64-v3 with AVX2 on x86-64, dot product on ARM64)")
