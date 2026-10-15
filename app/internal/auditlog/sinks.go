package auditlog

// builtinSinks returns the Event Log and OTLP sinks that are open right now,
// as Forwarders for forward-required delivery.
func builtinSinks() []Forwarder {
	var sinks []Forwarder
	if s := eventLogSink(); s != nil {
		sinks = append(sinks, s)
	}
	if s := otlpSink(); s != nil {
		sinks = append(sinks, s)
	}
	return sinks
}

// isBuiltinSink reports whether s is the Event Log or OTLP sink, which the
// best-effort path reaches through emitToEventLog and emitToOTLP instead.
func isBuiltinSink(s Forwarder) bool {
	switch s.(type) {
	case eventLogForwarder, otlpForwarder:
		return true
	default:
		return false
	}
}
