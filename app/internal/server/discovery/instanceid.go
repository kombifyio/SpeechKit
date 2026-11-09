package discovery

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/config"
)

// InstanceIDFile holds the server's stable instance id in the server state
// directory (config.ServerStateDir). The id is not a credential: it names
// this server on the LAN (TXT `id=`) and in an account's device registry.
const InstanceIDFile = "server-instance-id"

// AnnouncementFile is written by the announce-only mode with the LAN address
// it announces, so a server in a bridge network that shares the state
// directory can report its LAN URL without seeing the LAN itself.
const AnnouncementFile = "discovery-announcement.json"

const announcementSchema = "speechkit.discovery_announcement.v1"

var instanceIDPattern = regexp.MustCompile(`^dev_[A-Za-z0-9_-]{43}$`)

// ValidInstanceID reports whether id has the instance id form: "dev_" plus
// the unpadded base64url encoding of 32 random bytes.
func ValidInstanceID(id string) bool { return instanceIDPattern.MatchString(id) }

// NewInstanceID returns a fresh random instance id.
func NewInstanceID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("discovery: instance id: %w", err)
	}
	return "dev_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// LoadInstanceID reads the persisted instance id. A missing file returns
// fs.ErrNotExist; a malformed one is an error, never silently replaced.
func LoadInstanceID(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, InstanceIDFile)) // #nosec G304 -- fixed name in the configured state directory.
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if !ValidInstanceID(id) {
		return "", fmt.Errorf("discovery: %s holds no valid instance id; remove it to generate a new one",
			filepath.Join(dir, InstanceIDFile))
	}
	return id, nil
}

// EnsureInstanceID returns the persisted instance id and creates it once when
// it does not exist yet. Creation is a hard link of a complete temporary
// file, so two processes sharing the directory (server and announcer) agree
// on one id and a reader never sees a partial file.
func EnsureInstanceID(dir string) (string, error) {
	id, err := LoadInstanceID(dir)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return id, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("discovery: state directory: %w", err)
	}
	fresh, err := NewInstanceID()
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".instance-id-*")
	if err != nil {
		return "", fmt.Errorf("discovery: write instance id: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	_, werr := tmp.WriteString(fresh + "\n")
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", fmt.Errorf("discovery: write instance id: %w", werr)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil { // #nosec G302 -- the id is not secret; the announcer reads it.
		return "", fmt.Errorf("discovery: write instance id: %w", err)
	}
	if err := os.Link(tmpName, filepath.Join(dir, InstanceIDFile)); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("discovery: write instance id: %w", err)
	}
	return LoadInstanceID(dir)
}

// stableInstanceID is the id the announcement carries, or "" when the state
// directory cannot hold one (the announcement then simply omits `id=`).
func stableInstanceID(cfg *config.Config) string {
	dir := config.ServerStateDir(cfg)
	id, err := EnsureInstanceID(dir)
	if err != nil {
		slog.Warn("mDNS discovery: no instance id, announcing without id=", "dir", dir, "err", err)
		return ""
	}
	return id
}

// Announcement is the LAN address an announce-only process announced.
type Announcement struct {
	Schema    string `json:"schema"`
	URL       string `json:"url"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	UpdatedAt string `json:"updated_at"`
}

func writeAnnouncement(dir string, a Announcement) error {
	a.Schema = announcementSchema
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".announcement-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	if err := os.Chmod(tmpName, 0o644); err != nil { // #nosec G302 -- a LAN address, read by the server process.
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, AnnouncementFile))
}

// LoadAnnouncement reads the address an announce-only process last wrote to
// dir. ok is false when there is none or it is unusable.
func LoadAnnouncement(dir string) (Announcement, bool) {
	data, err := os.ReadFile(filepath.Join(dir, AnnouncementFile)) // #nosec G304 -- fixed name in the configured state directory.
	if err != nil {
		return Announcement{}, false
	}
	var a Announcement
	if json.Unmarshal(data, &a) != nil || a.Schema != announcementSchema || strings.TrimSpace(a.URL) == "" {
		return Announcement{}, false
	}
	return a, true
}
