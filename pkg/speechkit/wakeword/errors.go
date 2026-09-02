package wakeword

import "errors"

// ErrCgoRequired is returned by NewDetector and NewPipeline in a build
// without cgo. It exists in every build so hosts can write
// errors.Is(err, wakeword.ErrCgoRequired) regardless of how they compile.
var ErrCgoRequired = errors.New("wakeword: sherpa-onnx KWS requires cgo build (set CGO_ENABLED=1)")
