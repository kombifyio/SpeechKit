package edgesetup

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Board ids as the release manifests name them.
const (
	BoardRound185 = "waveshare-esp32s3-touch-lcd-1.85"
	BoardRLCD42   = "waveshare-esp32s3-rlcd-4.2"
	BoardC6AMOLED = "waveshare-esp32c6-touch-amoled-1.8"
)

// Match states of the running image against the probed hardware.
const (
	MatchYes     = "yes"
	MatchNo      = "no"
	MatchUnknown = "unknown"
)

// Identity is what the device reports about its hardware and running image.
type Identity struct {
	Chip string `json:"chip,omitempty"`
	// MAC is the factory MAC (lowercase, colon separated); it identifies the unit.
	MAC string `json:"mac,omitempty"`
	// Board is the probed hardware board, empty when no probe was conclusive.
	Board string `json:"board,omitempty"`
	// ImageBoard is the board the running image was built for.
	ImageBoard string `json:"image_board,omitempty"`
	Match      string `json:"match"`
	// Via names the evidence for Board: i2c, nvs or image (firmware probe),
	// i2c-signature (host check of the image's own bus) or rom (ROM loader).
	Via             string `json:"via,omitempty"`
	Project         string `json:"project,omitempty"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	// ROMLoader is set when only the ROM loader answered: no application runs.
	ROMLoader bool `json:"rom_loader,omitempty"`
}

// Known reports whether the hardware board is established.
func (id Identity) Known() bool { return id.Board != "" }

var keyValue = regexp.MustCompile(`([a-z_]+)=(\S+)`)

func fields(line, after string) map[string]string {
	i := strings.Index(line, after)
	if i < 0 {
		return nil
	}
	out := map[string]string{}
	for _, m := range keyValue.FindAllStringSubmatch(line[i+len(after):], -1) {
		out[m[1]] = m[2]
	}
	return out
}

// ParseHardwareReport parses the `kbx_hw: hw: chip=.. mac=.. board=.. image=..
// match=.. via=..` line of `KBX hw?`.
func ParseHardwareReport(line string) (Identity, bool) {
	f := fields(line, "hw:")
	if f == nil || f["chip"] == "" || f["mac"] == "" {
		return Identity{}, false
	}
	id := Identity{Chip: f["chip"], MAC: strings.ToLower(f["mac"]), ImageBoard: f["image"], Match: f["match"], Via: f["via"]}
	if b := f["board"]; b != "" && b != "unknown" {
		id.Board = b
	}
	if id.Match != MatchYes && id.Match != MatchNo {
		id.Match = MatchUnknown
	}
	return id, true
}

// ParseVersionReport parses the `kbx_ota: version: project=.. version=..` line of `KBX version?`.
func ParseVersionReport(line string) (project, version string, ok bool) {
	f := fields(line, "version:")
	if f == nil || f["version"] == "" {
		return "", "", false
	}
	return f["project"], f["version"], true
}

// i2cSignature confirms a board when its image scans its own system bus. A
// board on another board's image finds almost nothing on those pins.
var i2cSignature = map[string]struct {
	pins     string
	required []string
}{
	BoardRound185: {pins: "SDA11/SCL10", required: []string{"0x20", "0x15"}},
	BoardRLCD42:   {pins: "SDA13/SCL14", required: []string{"0x70"}},
}

var scanLine = regexp.MustCompile(`scan i2c0/system (SDA\d+/SCL\d+): (\d+) device\(s\)((?: 0x[0-9A-Fa-f]{2})*)`)

// confirmBySignature reports whether an i2cscan line proves imageBoard.
func confirmBySignature(imageBoard, line string) bool {
	sig, ok := i2cSignature[imageBoard]
	m := scanLine.FindStringSubmatch(line)
	if !ok || m == nil || m[1] != sig.pins {
		return false
	}
	found := strings.Fields(strings.ToUpper(strings.ReplaceAll(m[3], "0x", "0X")))
	has := func(addr string) bool {
		for _, f := range found {
			if strings.EqualFold(f, addr) {
				return true
			}
		}
		return false
	}
	anchor := false
	for _, r := range sig.required {
		anchor = anchor || has(r)
	}
	return anchor && len(found) >= 2
}

var checkinBoard = regexp.MustCompile(`"board":"([a-z0-9][a-z0-9.-]{2,63})"`)

// Identify asks an application console for its hardware report. Firmware
// without `KBX hw?` falls back to the image's own board, confirmed only by
// the I2C signature of that board; the MAC then stays unknown.
func Identify(ctx context.Context, l *Link) (Identity, error) {
	var id Identity
	if line, err := l.Ask(ctx, "KBX version?", "version:", 3*time.Second); err == nil {
		id.Project, id.FirmwareVersion, _ = ParseVersionReport(line)
	} else if errors.Is(err, ErrClosed) {
		return id, err
	}
	l.Drain()
	if err := l.Send("KBX hw?"); err != nil {
		return id, err
	}
	var hwLine string
	olderC6 := false
	err := l.Expect(ctx, 3*time.Second, func(line string) (bool, error) {
		switch {
		case strings.Contains(line, "hw: chip="):
			hwLine = line
			return true, nil
		case strings.Contains(line, "kbx: unknown command"):
			// Only the C6 console answers an unknown command; an older C6 image.
			olderC6 = true
			return true, nil
		}
		return false, nil
	})
	if errors.Is(err, ErrClosed) {
		return id, err
	}
	if hw, ok := ParseHardwareReport(hwLine); ok {
		hw.Project, hw.FirmwareVersion = id.Project, id.FirmwareVersion
		return hw, nil
	}
	if olderC6 {
		id.Chip, id.ImageBoard, id.Board, id.Match, id.Via = "esp32c6", BoardC6AMOLED, BoardC6AMOLED, MatchYes, "image"
		return id, nil
	}

	// Older image: read the board it was built for from its check-in body.
	id.Match = MatchUnknown
	var body strings.Builder
	l.Drain()
	if err := l.Send("KBX sync body"); err != nil {
		return id, err
	}
	_ = l.Expect(ctx, 3*time.Second, func(line string) (bool, error) {
		if i := strings.Index(line, "checkin["); i >= 0 {
			if j := strings.Index(line[i:], "] "); j >= 0 {
				body.WriteString(line[i+j+2:])
			}
		}
		return checkinBoard.MatchString(body.String()), nil
	})
	if m := checkinBoard.FindStringSubmatch(body.String()); m != nil {
		id.ImageBoard = m[1]
	}
	if strings.Contains(id.Project, "c6") || id.ImageBoard == BoardC6AMOLED {
		id.Chip, id.ImageBoard, id.Board, id.Match, id.Via = "esp32c6", BoardC6AMOLED, BoardC6AMOLED, MatchYes, "image"
		return id, nil
	}
	if _, ok := i2cSignature[id.ImageBoard]; ok {
		id.Chip = "esp32s3"
		if line, err := l.Ask(ctx, "KBX i2cscan", "scan i2c0/system", 5*time.Second); err == nil && confirmBySignature(id.ImageBoard, line) {
			id.Board, id.Match, id.Via = id.ImageBoard, MatchYes, "i2c-signature"
		}
	}
	return id, nil
}
