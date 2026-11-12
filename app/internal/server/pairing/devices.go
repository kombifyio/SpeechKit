package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DeviceTokenPrefix marks a per-device LAN pairing credential
// (speechkit.pairing.v2). The server bearer never carries it.
const DeviceTokenPrefix = "skd_"

// DevicesFileVersion is the on-disk schema of the paired-device registry.
const DevicesFileVersion = "speechkit.paired_devices.v1"

// lastUsedFlushInterval bounds how often a busy device rewrites the registry
// file just to move its last_used timestamp.
const lastUsedFlushInterval = 10 * time.Minute

// Device is one paired LAN device. The registry keeps only the SHA-256 of
// its token; the token itself is shown once, to the device that polled it.
type Device struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	HardwareID  string     `json:"hardware_id,omitempty"`
	PairedFrom  string     `json:"paired_from,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	TokenSHA256 string     `json:"token_sha256,omitempty"`
}

// Public is the operator view: everything except the token hash.
func (d Device) Public() Device {
	d.TokenSHA256 = ""
	return d
}

type devicesFile struct {
	V       string   `json:"v"`
	Devices []Device `json:"devices"`
}

// DeviceRegistry holds the paired devices and persists them to one JSON
// file (mode 0600, atomic replace). Revocation removes the in-memory entry
// first, so it takes effect for the next request even if the file write
// fails.
type DeviceRegistry struct {
	mu        sync.Mutex
	path      string
	byID      map[string]*Device
	byHash    map[string]*Device
	lastFlush time.Time
}

var (
	ErrDeviceNotFound = errors.New("pairing: device not found")
	errDevicesVersion = errors.New("pairing: unsupported paired-devices file version")
)

// OpenDeviceRegistry loads path when it exists. An empty path keeps the
// registry in memory only (tokens die with the process).
func OpenDeviceRegistry(path string) (*DeviceRegistry, error) {
	reg := &DeviceRegistry{
		path:   strings.TrimSpace(path),
		byID:   map[string]*Device{},
		byHash: map[string]*Device{},
	}
	if reg.path == "" {
		return reg, nil
	}
	raw, err := os.ReadFile(reg.path) // #nosec G304 -- operator-controlled data directory.
	if errors.Is(err, os.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return nil, err
	}
	var file devicesFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("pairing: read %s: %w", reg.path, err)
	}
	if file.V != DevicesFileVersion {
		return nil, errDevicesVersion
	}
	for i := range file.Devices {
		d := file.Devices[i]
		if d.ID == "" || len(d.TokenSHA256) != sha256.Size*2 {
			continue
		}
		reg.byID[d.ID] = &d
		reg.byHash[d.TokenSHA256] = &d
	}
	return reg, nil
}

// HashDeviceToken is the stored form of a device token.
func HashDeviceToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Issue registers a device and returns it with its new token. A device with
// the same non-empty hardware id is replaced (its old token stops working):
// re-pairing a Box must not leave a second live credential behind.
func (r *DeviceRegistry) Issue(name, kind, hardwareID, source string, now time.Time) (Device, string, error) {
	secret, err := randomString(32)
	if err != nil {
		return Device{}, "", err
	}
	idPart, err := randomString(9)
	if err != nil {
		return Device{}, "", err
	}
	token := DeviceTokenPrefix + secret
	dev := &Device{
		ID:          "dev_" + idPart,
		Name:        name,
		Kind:        kind,
		HardwareID:  hardwareID,
		PairedFrom:  source,
		CreatedAt:   now.UTC().Truncate(time.Second),
		TokenSHA256: HashDeviceToken(token),
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	var replaced []*Device
	if hardwareID != "" {
		for _, existing := range r.byID {
			if existing.HardwareID == hardwareID {
				replaced = append(replaced, existing)
			}
		}
	}
	for _, old := range replaced {
		delete(r.byID, old.ID)
		delete(r.byHash, old.TokenSHA256)
	}
	r.byID[dev.ID] = dev
	r.byHash[dev.TokenSHA256] = dev
	if err := r.persistLocked(); err != nil {
		// Roll back: a token the registry cannot keep is never handed out.
		delete(r.byID, dev.ID)
		delete(r.byHash, dev.TokenSHA256)
		for _, old := range replaced {
			r.byID[old.ID] = old
			r.byHash[old.TokenSHA256] = old
		}
		return Device{}, "", err
	}
	return dev.Public(), token, nil
}

// Verify resolves a presented device token. Lookup is by SHA-256, so the
// comparison never touches the secret byte by byte.
func (r *DeviceRegistry) Verify(token string, now time.Time) (Device, bool) {
	token = strings.TrimSpace(token)
	if r == nil || !strings.HasPrefix(token, DeviceTokenPrefix) || len(token) < len(DeviceTokenPrefix)+32 {
		return Device{}, false
	}
	hash := HashDeviceToken(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	dev, ok := r.byHash[hash]
	if !ok {
		return Device{}, false
	}
	used := now.UTC().Truncate(time.Second)
	dev.LastUsedAt = &used
	if now.Sub(r.lastFlush) >= lastUsedFlushInterval {
		// Best effort: last_used is informational, auth already decided.
		if r.persistLocked() == nil {
			r.lastFlush = now
		}
	}
	return dev.Public(), true
}

// Revoke removes a device by id. The in-memory removal happens first, so the
// device's next request fails even when persisting returns an error.
func (r *DeviceRegistry) Revoke(id string) (Device, error) {
	id = strings.TrimSpace(id)
	r.mu.Lock()
	defer r.mu.Unlock()
	dev, ok := r.byID[id]
	if !ok {
		return Device{}, ErrDeviceNotFound
	}
	delete(r.byID, dev.ID)
	delete(r.byHash, dev.TokenSHA256)
	return dev.Public(), r.persistLocked()
}

// List returns the operator view of every paired device, oldest first.
func (r *DeviceRegistry) List() []Device {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Device, 0, len(r.byID))
	for _, d := range r.byID {
		out = append(out, d.Public())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (r *DeviceRegistry) persistLocked() error {
	if r.path == "" {
		return nil
	}
	file := devicesFile{V: DevicesFileVersion, Devices: make([]Device, 0, len(r.byID))}
	for _, d := range r.byID {
		file.Devices = append(file.Devices, *d)
	}
	sort.Slice(file.Devices, func(i, j int) bool { return file.Devices[i].ID < file.Devices[j].ID })
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.path), ".paired-devices-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, r.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(r.path))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
