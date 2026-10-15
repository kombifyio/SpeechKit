package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Capture channel names of [meeting] capture_channels and the
// Meeting\CaptureChannels policy. They are the SDK's CaptureChannel* values.
const (
	MeetingChannelMic    = speechkit.CaptureChannelMicrophone
	MeetingChannelSystem = speechkit.CaptureChannelSystem
)

// ErrMeetingCaptureChannels wraps every rejected capture channel list.
var ErrMeetingCaptureChannels = errors.New("invalid meeting capture channels")

// ParseMeetingCaptureChannels trims, lower-cases and de-duplicates a channel
// list (order kept). An unknown name or an empty list is an error: an empty
// list is never read as "no capture" or as the default, because the default
// is expressed by leaving the setting out.
func ParseMeetingCaptureChannels(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.ToLower(strings.TrimSpace(value))
		switch name {
		case MeetingChannelMic, MeetingChannelSystem:
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		default:
			return nil, fmt.Errorf("%w: unknown channel %q (expected %s or %s)", ErrMeetingCaptureChannels, value, MeetingChannelMic, MeetingChannelSystem)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: name at least one of %s, %s (omit the setting for both)", ErrMeetingCaptureChannels, MeetingChannelMic, MeetingChannelSystem)
	}
	return out, nil
}

// NormalizeMeetingCaptureChannels canonicalizes [meeting] capture_channels and
// rejects unknown names and an explicit empty list. An absent setting stays
// absent. Load and Save both call it.
func NormalizeMeetingCaptureChannels(cfg *Config) error {
	if cfg == nil || cfg.Meeting.CaptureChannels == nil {
		return nil
	}
	channels, err := ParseMeetingCaptureChannels(cfg.Meeting.CaptureChannels)
	if err != nil {
		return fmt.Errorf("[meeting] capture_channels: %w", err)
	}
	cfg.Meeting.CaptureChannels = channels
	return nil
}

// ResolvedCaptureChannels returns the channels a meeting records: the
// configured list, or the reference behaviour (microphone plus system
// loopback) when none is configured.
func (m MeetingConfig) ResolvedCaptureChannels() []string {
	if channels, err := ParseMeetingCaptureChannels(m.CaptureChannels); err == nil {
		return channels
	}
	return []string{MeetingChannelMic, MeetingChannelSystem}
}

// meetingCaptureChannelsKey is the Meeting\CaptureChannels list key: a plain
// list pin (HKCU may only remove entries) that fails closed to the
// microphone alone, because an empty or unreadable pin must never fall back
// to the two-channel default the pin exists to narrow.
func meetingCaptureChannelsKey() policyKey {
	return policyKey{
		name:  `Meeting\CaptureChannels`,
		field: "meeting.capture_channels",
		ops: listPolicy{
			slot:       func(p *PolicyValues) *[]string { return &p.MeetingCaptureChannels },
			target:     func(c *Config) *[]string { return &c.Meeting.CaptureChannels },
			validate:   ParseMeetingCaptureChannels,
			failClosed: []string{MeetingChannelMic},
		},
	}
}
