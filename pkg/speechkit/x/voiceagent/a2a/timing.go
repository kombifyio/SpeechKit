package a2a

import (
	"strconv"
	"strings"
)

// recordTiming keeps the phases of the latest turn: request to response
// headers, to the first answer text and to the end of the stream, plus the
// perimeter's Server-Timing durations ("edge;dur=3574, origin;dur=2157").
func (a *Agent) recordTiming(headerMs, firstDeltaMs, totalMs int64, serverTiming string, orchestrator map[string]int64) {
	timing := map[string]int64{"a2a_header_ms": headerMs, "a2a_total_ms": totalMs}
	if firstDeltaMs >= 0 {
		timing["a2a_first_delta_ms"] = firstDeltaMs
	}
	for _, entry := range strings.Split(serverTiming, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(entry), ";")
		for _, param := range strings.Split(params, ";") {
			if value, found := strings.CutPrefix(strings.TrimSpace(param), "dur="); found {
				if ms, err := strconv.ParseFloat(value, 64); err == nil && name != "" && len(name) <= 24 {
					timing["perimeter_"+strings.ToLower(name)+"_ms"] = int64(ms)
				}
			}
		}
	}
	for name, ms := range orchestrator {
		timing["orch_"+name] = ms
	}
	a.timingMu.Lock()
	a.lastTiming = timing
	a.timingMu.Unlock()
}

// LastTurnTiming returns the phase durations of the latest turn in
// milliseconds; it implements the cascaded provider's timing report.
func (a *Agent) LastTurnTiming() map[string]int64 {
	a.timingMu.Lock()
	defer a.timingMu.Unlock()
	out := make(map[string]int64, len(a.lastTiming))
	for name, ms := range a.lastTiming {
		out[name] = ms
	}
	return out
}

// captureRunTimings reads the agent's own per-hop spans (metadata "io.kombify.run_timings", numbers only).
func captureRunTimings(envelope any, into map[string]int64) {
	root, _ := envelope.(map[string]any)
	result, _ := root["result"].(map[string]any)
	metadata, _ := result["metadata"].(map[string]any)
	values, _ := metadata["io.kombify.run_timings"].(map[string]any)
	for key, raw := range values {
		if number, ok := raw.(float64); ok && len(key) > 0 && len(key) <= 32 && len(into) < 32 {
			into[key] = int64(number)
		}
	}
}
