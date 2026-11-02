package edgesetup

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Step names of a setup run.
const (
	StepInspect = "inspect"
	StepInstall = "install"
	StepWiFi    = "wifi"
	StepLink    = "link"
)

// Event is one outcome line of a run, safe to show and log.
type Event struct {
	At      time.Time `json:"at"`
	Step    string    `json:"step"`
	Level   string    `json:"level"` // info | warn | error
	Message string    `json:"message"`
}

// Plan is what the run proposes after inspection; each action needs the
// person's confirmation.
type Plan struct {
	// Install is set when the device needs another image: none for its board,
	// an image built for another board, or an older version.
	Install       bool      `json:"install"`
	InstallReason string    `json:"install_reason,omitempty"`
	Release       *Manifest `json:"release,omitempty"`
	// NeedsBoardChoice is set when the hardware board could not be established.
	NeedsBoardChoice bool `json:"needs_board_choice,omitempty"`
	// CanLink is false when the device cannot take a link ticket yet.
	CanLink bool `json:"can_link"`
}

// Status is a snapshot of a run.
type Status struct {
	Port     Port        `json:"port"`
	Identity Identity    `json:"identity"`
	Plan     Plan        `json:"plan"`
	WiFi     *WiFiStatus `json:"wifi,omitempty"`
	DeviceID string      `json:"device_id,omitempty"`
	Busy     string      `json:"busy,omitempty"`
	Progress int         `json:"progress"`
	Done     []string    `json:"done"`
	Events   []Event     `json:"events"`
}

// Setup is one device setup run. Its methods block; run them off the UI.
type Setup struct {
	Releases ReleaseSource
	Linker   Linker

	mu     sync.Mutex
	status Status
	image  *Image
}

// NewSetup starts a run for a port the person chose.
func NewSetup(p Port, releases ReleaseSource, linker Linker) *Setup {
	return &Setup{Releases: releases, Linker: linker, status: Status{Port: p, Done: []string{}, Events: []Event{}}}
}

// Snapshot returns the current status.
func (s *Setup) Snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.status
	out.Done = append([]string(nil), s.status.Done...)
	out.Events = append([]Event(nil), s.status.Events...)
	return out
}

func (s *Setup) event(step, level, format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Events = append(s.status.Events, Event{At: time.Now().UTC(), Step: step, Level: level, Message: fmt.Sprintf(format, args...)})
	if len(s.status.Events) > 200 {
		s.status.Events = s.status.Events[len(s.status.Events)-200:]
	}
}

func (s *Setup) begin(step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Busy != "" {
		return fmt.Errorf("edgesetup: %s is still running", s.status.Busy)
	}
	s.status.Busy, s.status.Progress = step, 0
	return nil
}

func (s *Setup) end(step string, err error) error {
	s.mu.Lock()
	s.status.Busy = ""
	if err == nil {
		s.status.Done = append(s.status.Done, step)
	}
	s.mu.Unlock()
	if err != nil {
		s.event(step, "error", "%s", err.Error())
	}
	return err
}

// Inspect identifies the device and proposes a plan.
func (s *Setup) Inspect(ctx context.Context) (Status, error) {
	if err := s.begin(StepInspect); err != nil {
		return s.Snapshot(), err
	}
	id, err := s.identify(ctx, s.Snapshot().Port)
	if err == nil {
		s.mu.Lock()
		s.status.Identity = id
		s.mu.Unlock()
		s.describe(id)
		err = s.plan(ctx, id)
	}
	return s.Snapshot(), s.end(StepInspect, err)
}

// ChooseBoard sets the hardware board when the device could not prove it.
func (s *Setup) ChooseBoard(ctx context.Context, board string) (Status, error) {
	if board != BoardRound185 && board != BoardRLCD42 {
		return s.Snapshot(), errors.New("edgesetup: unknown board")
	}
	s.mu.Lock()
	s.status.Identity.Board, s.status.Identity.Via = board, "person"
	if s.status.Identity.ImageBoard != "" && s.status.Identity.ImageBoard != board {
		s.status.Identity.Match = MatchNo
	}
	id := s.status.Identity
	s.mu.Unlock()
	s.event(StepInspect, "info", "Board chosen: %s", board)
	return s.Snapshot(), s.plan(ctx, id)
}

func (s *Setup) identify(ctx context.Context, p Port) (Identity, error) {
	l, err := OpenLink(p)
	if err == nil {
		id, idErr := Identify(ctx, l)
		_ = l.Close()
		if idErr == nil && (id.MAC != "" || id.ImageBoard != "" || id.Board != "") {
			return id, nil
		}
		err = idErr
	}
	if p.Mode == ModeSerialJTAG {
		// No application answered on the built-in port: try the ROM loader.
		if id, romErr := IdentifyROM(ctx, p); romErr == nil {
			return id, nil
		}
	}
	if err == nil {
		err = errors.New("edgesetup: the device did not identify itself")
	}
	return Identity{}, err
}

func (s *Setup) describe(id Identity) {
	switch {
	case id.ROMLoader:
		s.event(StepInspect, "warn", "Only the chip's ROM loader answers (%s, %s): no application is running.", id.Chip, id.MAC)
	case id.Match == MatchNo:
		s.event(StepInspect, "warn", "Hardware %s runs an image built for %s.", id.Board, id.ImageBoard)
	case id.Board != "":
		s.event(StepInspect, "info", "Hardware %s (%s), firmware %s, identified by %s.", id.Board, id.Chip, orDash(id.FirmwareVersion), id.Via)
	default:
		s.event(StepInspect, "warn", "Could not prove the board; the running image is for %s.", orDash(id.ImageBoard))
	}
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func (s *Setup) plan(ctx context.Context, id Identity) error {
	p := Plan{CanLink: id.MAC != "" && !id.ROMLoader && id.Match == MatchYes}
	if id.Board == "" {
		p.NeedsBoardChoice = !id.ROMLoader
		s.setPlan(p)
		return nil
	}
	m, err := s.Releases.FetchManifest(ctx, id.Board)
	switch {
	case errors.Is(err, ErrNoRelease):
		s.event(StepInspect, "info", "No published firmware for %s; keeping the installed one.", id.Board)
	case err != nil:
		s.setPlan(p)
		return err
	default:
		p.Release = &m
		switch {
		case id.ROMLoader:
			p.InstallReason = "no_application"
		case id.Match == MatchNo || (id.ImageBoard != "" && id.ImageBoard != id.Board):
			p.Install, p.InstallReason = true, "wrong_board_image"
		case id.FirmwareVersion == "" || compareVersions(id.FirmwareVersion, m.Version) < 0:
			p.Install, p.InstallReason = true, "update"
		}
		if p.Install {
			// The new image proves the board itself; link after it runs.
			p.CanLink = false
		}
	}
	s.setPlan(p)
	return nil
}

func (s *Setup) setPlan(p Plan) {
	s.mu.Lock()
	s.status.Plan = p
	s.mu.Unlock()
}

// Install downloads, verifies and installs the planned image, then finds the
// device again by its MAC and confirms the new version.
func (s *Setup) Install(ctx context.Context) (Status, error) {
	if err := s.begin(StepInstall); err != nil {
		return s.Snapshot(), err
	}
	st := s.Snapshot()
	err := s.install(ctx, st)
	return s.Snapshot(), s.end(StepInstall, err)
}

func (s *Setup) install(ctx context.Context, st Status) error {
	if st.Plan.Release == nil || !st.Plan.Install {
		return errors.New("edgesetup: nothing to install")
	}
	if st.Identity.ROMLoader || st.Port.Mode != ModeApp {
		return errors.New("edgesetup: this device can only take an image while its application runs")
	}
	if s.image == nil || s.image.Manifest.SHA256 != st.Plan.Release.SHA256 {
		s.event(StepInstall, "info", "Downloading firmware %s for %s.", st.Plan.Release.Version, st.Plan.Release.Board)
		img, err := s.Releases.Download(ctx, *st.Plan.Release)
		if err != nil {
			return err
		}
		s.image = &img
	}
	l, err := OpenLink(st.Port)
	if err != nil {
		return err
	}
	s.event(StepInstall, "info", "Installing firmware %s (%d bytes).", s.image.Manifest.Version, len(s.image.Data))
	err = InstallOverConsole(ctx, l, *s.image, func(pct int) {
		s.mu.Lock()
		s.status.Progress = pct
		s.mu.Unlock()
	})
	_ = l.Close()
	if err != nil {
		return err
	}
	s.event(StepInstall, "info", "Image accepted; waiting for the device to restart.")
	port, id, err := s.reattach(ctx, st.Identity.MAC, 90*time.Second)
	if err != nil {
		return err
	}
	if id.FirmwareVersion != s.image.Manifest.Version {
		return fmt.Errorf("edgesetup: the device restarted on %s instead of %s (it may have rolled back)", orDash(id.FirmwareVersion), s.image.Manifest.Version)
	}
	s.mu.Lock()
	s.status.Port, s.status.Identity = port, id
	s.mu.Unlock()
	s.event(StepInstall, "info", "Firmware %s runs; hardware %s, match=%s.", id.FirmwareVersion, orDash(id.Board), id.Match)
	return s.plan(ctx, id)
}

// reattach waits for the application console to come back and identifies it.
// With a known MAC only that unit is accepted; port names never count.
func (s *Setup) reattach(ctx context.Context, mac string, wait time.Duration) (Port, Identity, error) {
	deadline := time.Now().Add(wait)
	time.Sleep(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return Port{}, Identity{}, err
		}
		ports, _ := ScanPorts()
		for _, p := range ports {
			if p.Mode != ModeApp {
				continue
			}
			l, err := OpenLink(p)
			if err != nil {
				continue
			}
			time.Sleep(500 * time.Millisecond)
			id, err := Identify(ctx, l)
			_ = l.Close()
			if err == nil && (mac == "" || id.MAC == mac) {
				return p, id, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return Port{}, Identity{}, errors.New("edgesetup: the device did not come back after the install; unplug it and plug it in again")
}

// ConfigureWiFi hands Wi-Fi credentials to the device.
func (s *Setup) ConfigureWiFi(ctx context.Context, ssid, password string) (Status, error) {
	if err := s.begin(StepWiFi); err != nil {
		return s.Snapshot(), err
	}
	st := s.Snapshot()
	l, err := OpenLink(st.Port)
	if err == nil {
		var w WiFiStatus
		w, err = SetWiFi(ctx, l, ssid, password, 45*time.Second)
		_ = l.Close()
		if err == nil {
			s.mu.Lock()
			s.status.WiFi = &w
			s.mu.Unlock()
			s.event(StepWiFi, "info", "Joined %s (IP %s).", w.SSID, w.IP)
		}
	}
	return s.Snapshot(), s.end(StepWiFi, err)
}

// ReadWiFiState refreshes the device's Wi-Fi state without changing it.
func (s *Setup) ReadWiFiState(ctx context.Context) (Status, error) {
	st := s.Snapshot()
	l, err := OpenLink(st.Port)
	if err != nil {
		return st, err
	}
	defer l.Close()
	w, err := ReadWiFi(ctx, l)
	if err == nil {
		s.mu.Lock()
		s.status.WiFi = &w
		s.mu.Unlock()
	}
	return s.Snapshot(), err
}

// Link asks the Linker for a ticket and hands it to the device.
func (s *Setup) Link(ctx context.Context) (Status, error) {
	if err := s.begin(StepLink); err != nil {
		return s.Snapshot(), err
	}
	st := s.Snapshot()
	err := func() error {
		if !st.Plan.CanLink {
			return errors.New("edgesetup: the device cannot be linked yet; install its firmware first")
		}
		if s.Linker == nil {
			return errors.New("edgesetup: linking is not available in this app")
		}
		ticket, err := s.Linker.Ticket(ctx, st.Identity)
		if err != nil {
			return err
		}
		l, err := OpenLink(st.Port)
		if err != nil {
			return err
		}
		defer l.Close()
		id, err := LinkDevice(ctx, l, ticket)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.status.DeviceID = id
		s.mu.Unlock()
		s.event(StepLink, "info", "Linked as %s.", id)
		return nil
	}()
	return s.Snapshot(), s.end(StepLink, err)
}

// compareVersions compares dotted numeric versions; a suffix after the third
// number (a local build) sorts below the release.
func compareVersions(a, b string) int {
	pa, sa := splitVersion(a)
	pb, sb := splitVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case sa == sb:
		return 0
	case sa != "" && sb == "":
		return -1
	case sa == "" && sb != "":
		return 1
	}
	return 0
}

func splitVersion(v string) ([3]int, string) {
	var out [3]int
	if len(v) > 0 && v[0] == 'v' {
		v = v[1:]
	}
	i, part := 0, 0
	for ; i < len(v) && part < 3; i++ {
		c := v[i]
		switch {
		case c >= '0' && c <= '9':
			out[part] = out[part]*10 + int(c-'0')
		case c == '.':
			part++
		default:
			return out, v[i:]
		}
	}
	if i < len(v) {
		return out, v[i:]
	}
	return out, ""
}
