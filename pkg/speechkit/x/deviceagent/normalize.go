package deviceagent

import "strings"

func normalizeDevice(d DeviceDescriptor) DeviceDescriptor {
	d.AgentID = firstNonEmpty(d.AgentID, "speechkit-device-agent")
	d.DeviceID = firstNonEmpty(d.DeviceID, "speechkit-device-agent-001")
	d.DisplayName = firstNonEmpty(d.DisplayName, d.DeviceID)
	d.RoomID = firstNonEmpty(d.RoomID, "default")
	d.CaptureDevice.Kind = firstNonEmpty(d.CaptureDevice.Kind, "microphone")
	d.OutputDevice.Kind = firstNonEmpty(d.OutputDevice.Kind, "speaker")
	d.Wakeword.Status = firstNonEmpty(d.Wakeword.Status, CapabilityUnverified)
	return d
}

func normalizeHealth(h Health) Health {
	h.Status = firstNonEmpty(h.Status, CapabilityUnverified)
	return h
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
