package cloudlink

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const credentialSchema = "speechkit.cloud_link.credential.v1"

// credential is the persisted link. RefreshToken is the only secret; the
// file is written owner-only and replaced atomically on every rotation.
type credential struct {
	Schema         string    `json:"schema"`
	DeviceID       string    `json:"device_id"`
	Issuer         string    `json:"issuer"`
	ClientID       string    `json:"client_id"`
	Audience       string    `json:"audience"`
	RefreshToken   string    `json:"refresh_token"`
	LinkedAt       time.Time `json:"linked_at"`
	RelinkRequired bool      `json:"relink_required,omitempty"`
}

func credentialPath(dir string) string { return filepath.Join(dir, CredentialFile) }

// loadCredential returns (nil, nil) when no link exists.
func loadCredential(dir string) (*credential, error) {
	data, err := os.ReadFile(credentialPath(dir)) // #nosec G304 -- fixed name in the configured state directory.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read the link credential: %w", err)
	}
	var c credential
	if err := json.Unmarshal(data, &c); err != nil || c.Schema != credentialSchema || c.RefreshToken == "" {
		return nil, fmt.Errorf("%s is not a valid link credential; unlink to remove it", credentialPath(dir))
	}
	return &c, nil
}

func saveCredential(dir string, c *credential) error {
	c.Schema = credentialSchema
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".cloud-link-*")
	if err != nil {
		return fmt.Errorf("write the link credential: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	// os.CreateTemp already creates 0600; the explicit mode guards a umask
	// or platform that widens it.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write the link credential: %w", err)
	}
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("write the link credential: %w", werr)
	}
	if err := os.Rename(tmpName, credentialPath(dir)); err != nil {
		return fmt.Errorf("write the link credential: %w", err)
	}
	return nil
}

func deleteCredential(dir string) error {
	if err := os.Remove(credentialPath(dir)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the link credential: %w", err)
	}
	return nil
}
