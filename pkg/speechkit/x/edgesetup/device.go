package edgesetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// InstallOverConsole writes an image into the device's inactive slot over the
// application console (`KBX ota-begin <bytes> <sha256>`, then the raw image
// after the `recv` line). The device verifies the image, switches the boot
// slot and restarts; the port disappears. progress gets 0..100.
func InstallOverConsole(ctx context.Context, l *Link, img Image, progress func(pct int)) error {
	sum := sha256.Sum256(img.Data)
	l.Drain()
	if err := l.Send(fmt.Sprintf("KBX ota-begin %d %s", len(img.Data), hex.EncodeToString(sum[:]))); err != nil {
		return err
	}
	// Nothing may be sent before `recv`: earlier bytes reach the line parser.
	err := l.Expect(ctx, 30*time.Second, func(line string) (bool, error) {
		switch {
		case strings.Contains(line, "kbx_ota: recv"):
			return true, nil
		case strings.Contains(line, "kbx_ota: err"), strings.Contains(line, "ota-begin rejected"),
			strings.Contains(line, "ota-begin: need"):
			return false, fmt.Errorf("edgesetup: device refused the install (%s)", lastField(line))
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	const block = 4096
	for off := 0; off < len(img.Data); off += block {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := off + block
		if end > len(img.Data) {
			end = len(img.Data)
		}
		if err := l.WriteRaw(img.Data[off:end]); err != nil {
			return fmt.Errorf("edgesetup: write image: %w", err)
		}
		if progress != nil {
			progress(end * 95 / len(img.Data))
		}
	}
	err = l.Expect(ctx, 90*time.Second, func(line string) (bool, error) {
		switch {
		case strings.Contains(line, "complete - rebooting"):
			return true, nil
		case strings.Contains(line, "kbx_ota: err"), strings.Contains(line, "sha256 mismatch"):
			return false, fmt.Errorf("edgesetup: device rejected the image (%s)", lastField(line))
		}
		return false, nil
	})
	if errors.Is(err, ErrClosed) {
		// The restart can close the port before the last line arrives; the
		// caller confirms the new version after the device is back.
		err = nil
	}
	if err == nil && progress != nil {
		progress(100)
	}
	return err
}

func lastField(line string) string {
	f := strings.Fields(line)
	if len(f) == 0 {
		return "no detail"
	}
	return f[len(f)-1]
}

// WiFiStatus is the device's Wi-Fi state.
type WiFiStatus struct {
	Connected bool   `json:"connected"`
	SSID      string `json:"ssid,omitempty"`
	IP        string `json:"ip,omitempty"`
}

var wifiConnected = regexp.MustCompile(`status: connected to (.+), IP (\S+)`)
var wifiConfigured = regexp.MustCompile(`status: configured for (.+), not connected`)

// ReadWiFi asks the device for its Wi-Fi state.
func ReadWiFi(ctx context.Context, l *Link) (WiFiStatus, error) {
	line, err := l.Ask(ctx, "KBX wifi?", "status:", 3*time.Second)
	if err != nil {
		return WiFiStatus{}, err
	}
	if m := wifiConnected.FindStringSubmatch(line); m != nil {
		return WiFiStatus{Connected: true, SSID: m[1], IP: m[2]}, nil
	}
	if m := wifiConfigured.FindStringSubmatch(line); m != nil {
		return WiFiStatus{SSID: m[1]}, nil
	}
	return WiFiStatus{}, nil
}

// SetWiFi hands Wi-Fi credentials to the device and waits until it joins.
// The password never appears in an error or event.
func SetWiFi(ctx context.Context, l *Link, ssid, password string, wait time.Duration) (WiFiStatus, error) {
	if ssid == "" || len(ssid) > 32 || len(password) > 63 || strings.ContainsAny(ssid+password, "|\r\n") {
		return WiFiStatus{}, errors.New("edgesetup: Wi-Fi name or password is not usable (1-32 character name, up to 63 character password, no | or line breaks)")
	}
	l.Drain()
	if err := l.Send("KBX wifi " + ssid + "|" + password); err != nil {
		return WiFiStatus{}, err
	}
	deadline := time.Now().Add(wait)
	for {
		select {
		case <-ctx.Done():
			return WiFiStatus{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		st, err := ReadWiFi(ctx, l)
		if err == nil && st.Connected && st.SSID == ssid {
			return st, nil
		}
		if time.Now().After(deadline) {
			if err == nil {
				return st, errors.New("edgesetup: the device did not join the Wi-Fi network; check the name and password")
			}
			return st, err
		}
	}
}

// Ticket is a single-use account link for one device.
type Ticket struct {
	// Value is the secret handed to the device; never log it.
	Value string
	// DeviceID is the account-scoped id the device will derive for itself.
	DeviceID string
}

// Linker issues a link ticket for an identified device (the caller's account
// service). It runs only after the person confirmed the link.
type Linker interface {
	Ticket(ctx context.Context, id Identity) (Ticket, error)
}

var enrollLinked = regexp.MustCompile(`enroll: linked device=(\S+)`)
var enrollFailed = regexp.MustCompile(`enroll: failed reason=(\S+) http=(\d+)`)

// LinkError is a refused link with the device's reason code.
type LinkError struct {
	Reason string
	HTTP   int
}

func (e *LinkError) Error() string {
	return fmt.Sprintf("edgesetup: the device could not link (%s)", e.Reason)
}

// LinkDevice hands a ticket to the device (`KBX cloud enroll <ticket>`) and
// waits for the device to redeem it and confirm the bound device id.
func LinkDevice(ctx context.Context, l *Link, t Ticket) (string, error) {
	if !strings.HasPrefix(t.Value, "kee_") || strings.ContainsAny(t.Value, " \t\r\n") {
		return "", errors.New("edgesetup: link ticket is malformed")
	}
	l.Drain()
	if err := l.Send("KBX cloud enroll " + t.Value); err != nil {
		return "", err
	}
	var linked string
	err := l.Expect(ctx, 90*time.Second, func(line string) (bool, error) {
		if m := enrollLinked.FindStringSubmatch(line); m != nil {
			linked = m[1]
			return true, nil
		}
		if m := enrollFailed.FindStringSubmatch(line); m != nil {
			status, _ := strconv.Atoi(m[2])
			return false, &LinkError{Reason: m[1], HTTP: status}
		}
		if strings.Contains(line, "kbx: unknown command") || strings.Contains(line, "refused in board mismatch") {
			return false, &LinkError{Reason: "unsupported"}
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	if t.DeviceID != "" && linked != t.DeviceID {
		return linked, &LinkError{Reason: "device_mismatch"}
	}
	return linked, nil
}
