// Package homegraph consumes kombify.home_graph.v1 for voice answer routing.
// The graph remains Cloud-owned; this package never changes it or its leader.
package homegraph

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
)

const MaxDocumentBytes = 2 << 20

var idPattern = regexp.MustCompile(`^(spc|hdv|rom)_[a-f0-9]{24}$`)

// Document is the voice projection of the shared home graph wire document.
// Floors, names and other fields have no bearing on an answer's authority.
type Document struct {
	Schema   string `json:"schema"`
	SpaceID  string `json:"space_id"`
	Revision int64  `json:"revision"`
	Rooms    []struct {
		ID string `json:"id"`
	} `json:"rooms"`
	Devices     []Device `json:"devices"`
	Assignments []struct {
		Kind           string `json:"kind"`
		DeviceID       string `json:"device_id"`
		TargetDeviceID string `json:"target_device_id"`
	} `json:"assignments"`
}

type Device struct {
	ID       string   `json:"id"`
	Class    string   `json:"class"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles"`
	RoomID   *string  `json:"room_id"`
	Identity struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	} `json:"identity"`
	ExternalRefs []struct {
		System string `json:"system"`
		Ref    string `json:"ref"`
	} `json:"external_refs"`
}

// Route has one physical destination, never a broadcast or group expansion.
type Route struct {
	DeviceID   string
	HADeviceID string
	Reason     string
	Local      bool
	Announce   bool
}

func Read(r io.Reader, spaceID string) (*Document, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxDocumentBytes+1))
	if err != nil || len(raw) > MaxDocumentBytes {
		return nil, errors.New("home_audio_graph_unreadable")
	}
	var d Document
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errors.New("home_audio_graph_invalid")
	}
	if d.Schema != "kombify.home_graph.v1" || d.SpaceID != spaceID || !validID(spaceID, "spc") || d.Revision < 0 || len(d.Devices) > 1024 || len(d.Rooms) > 256 || len(d.Assignments) > 4096 {
		return nil, errors.New("home_audio_graph_invalid")
	}
	rooms := map[string]bool{}
	for _, room := range d.Rooms {
		if !validID(room.ID, "rom") || rooms[room.ID] {
			return nil, errors.New("home_audio_graph_invalid")
		}
		rooms[room.ID] = true
	}
	devices := map[string]bool{}
	identities := map[string]bool{}
	for _, device := range d.Devices {
		identity := device.Identity.Kind + ":" + device.Identity.Ref
		if !validID(device.ID, "hdv") || devices[device.ID] || identities[identity] || device.Identity.Ref == "" || (device.Status != "active" && device.Status != "revoked") || (device.RoomID != nil && !rooms[*device.RoomID]) {
			return nil, errors.New("home_audio_graph_invalid")
		}
		devices[device.ID], identities[identity] = true, true
	}
	answers := map[string]bool{}
	for _, assignment := range d.Assignments {
		if assignment.Kind != "answers_on" {
			continue
		}
		if !devices[assignment.DeviceID] || !devices[assignment.TargetDeviceID] || answers[assignment.DeviceID] {
			return nil, errors.New("home_audio_graph_invalid")
		}
		answers[assignment.DeviceID] = true
	}
	return &d, nil
}

// Resolve binds input to a trusted registry identity supplied by the paired
// host, never to a device id, room or destination supplied in spoken text.
func (d *Document) Resolve(identityKind, identityRef string) (Route, error) {
	var source *Device
	for i := range d.Devices {
		device := &d.Devices[i]
		if device.Identity.Kind == identityKind && device.Identity.Ref == identityRef {
			source = device
			break
		}
	}
	// External microphones (including Sonos) cannot become kombify input.
	if source == nil || source.Status != "active" || identityKind == "external" || source.Class == "speaker" || !slices.Contains(source.Roles, "audio_in") {
		return Route{}, errors.New("home_audio_input_denied")
	}
	for _, assignment := range d.Assignments {
		if assignment.Kind == "answers_on" && assignment.DeviceID == source.ID {
			for _, device := range d.Devices {
				if device.ID == assignment.TargetDeviceID {
					return destination(device, source.ID, "answers_on")
				}
			}
		}
	}
	var candidates []Device
	if source.RoomID != nil {
		for _, device := range d.Devices {
			if device.ID != source.ID && device.Status == "active" && device.RoomID != nil && *device.RoomID == *source.RoomID && slices.Contains(device.Roles, "announce") {
				candidates = append(candidates, device)
			}
		}
	}
	// A stable choice avoids changing speakers when the document order changes.
	slices.SortFunc(candidates, func(a, b Device) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	if len(candidates) != 0 {
		return destination(candidates[0], source.ID, "room_announce")
	}
	if slices.Contains(source.Roles, "audio_out") {
		return Route{DeviceID: source.ID, Reason: "self", Local: true}, nil
	}
	return Route{}, errors.New("home_audio_no_output")
}

func destination(device Device, sourceID, reason string) (Route, error) {
	if device.Status != "active" || (!slices.Contains(device.Roles, "audio_out") && !slices.Contains(device.Roles, "announce")) {
		return Route{}, errors.New("home_audio_output_denied")
	}
	route := Route{DeviceID: device.ID, Reason: reason, Local: device.ID == sourceID, Announce: slices.Contains(device.Roles, "announce")}
	if route.Local {
		return route, nil
	}
	for _, ref := range device.ExternalRefs {
		if ref.System == "home_assistant" {
			if route.HADeviceID != "" {
				return Route{}, errors.New("home_audio_output_ambiguous")
			}
			route.HADeviceID = ref.Ref
		}
	}
	if route.HADeviceID == "" {
		return Route{}, errors.New("home_audio_output_unavailable")
	}
	return route, nil
}

func validID(id, prefix string) bool {
	return len(id) == 28 && id[:3] == prefix && idPattern.MatchString(id)
}
