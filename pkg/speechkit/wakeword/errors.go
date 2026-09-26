package wakeword

import "errors"

// ErrEngineUnavailable is returned by NewDetector when no keyword-spotting
// Engine is registered, or when DetectorConfig.Engine names one that is not.
// The root package ships no engine: import an engine package such as
// pkg/speechkit/wakeword/sherpa, which registers itself on import.
var ErrEngineUnavailable = errors.New("wakeword: no keyword-spotting engine registered (import an engine package such as pkg/speechkit/wakeword/sherpa)")

// ErrCgoRequired is returned by an engine constructor such as
// sherpa.NewDetector in a build without cgo. It exists in every build so hosts
// can write errors.Is(err, wakeword.ErrCgoRequired) regardless of how they
// compile.
var ErrCgoRequired = errors.New("wakeword: sherpa-onnx KWS requires cgo build (set CGO_ENABLED=1)")
