package speechkit

// OutputTarget identifies where a transcript or Assist result is delivered.
//
// The framework never inspects a target: it travels unchanged from the
// [RecordingStartOptions] (or the Assist tool call) that produced the
// audio to the [TranscriptOutput] and [TranscriptInterceptor] that consume the
// result. Hosts therefore define their own concrete targets — a native window
// handle, an editor buffer, a chat channel — and implement this interface so
// that logs, audit events and interceptors can name the target family without
// type-asserting host internals.
//
// Recognised kinds are documented in docs/speechkit-framework-api.md; hosts may
// introduce further kinds, prefixed with their product name (for example
// "companion.chat").
type OutputTarget interface {
	// TargetKind names the target family. It must be stable and lower-case
	// (for example "window", "editor", "clipboard").
	TargetKind() string
}

// Well-known target kinds. Hosts are free to add their own.
const (
	// TargetKindWindow is a native OS window the text is injected into.
	TargetKindWindow = "window"
	// TargetKindEditor is an in-app editor or text buffer.
	TargetKindEditor = "editor"
	// TargetKindClipboard means "copy only, do not inject".
	TargetKindClipboard = "clipboard"
	// TargetKindNone is the Output's default destination (a nil target).
	TargetKindNone = ""
)

// TargetRef is a ready-made [OutputTarget] for hosts that address targets by a
// stable string ID instead of a native handle.
type TargetRef struct {
	Kind string
	ID   string
}

func (t TargetRef) TargetKind() string { return t.Kind }

// TargetKind reports the kind of an arbitrary target value handed through the
// `Target any` fields: the [OutputTarget] kind when the value implements it,
// [TargetKindNone] for nil, and "opaque" for legacy untyped values so that
// hosts can spot targets still awaiting migration.
func TargetKind(target any) string {
	switch typed := target.(type) {
	case nil:
		return TargetKindNone
	case OutputTarget:
		return typed.TargetKind()
	default:
		return "opaque"
	}
}
