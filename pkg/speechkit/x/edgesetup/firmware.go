package edgesetup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// ReleaseSource tells where the release manifest and image of a board live.
// The caller owns the locations; the package only fetches and verifies.
type ReleaseSource struct {
	// ManifestURL returns the latest release manifest of a board, or "" when the
	// board has no published release.
	ManifestURL func(board string) string
	// ImageURL returns the image location a verified manifest names.
	ImageURL func(m Manifest) string
	// Project is the application project name every image must carry.
	Project string
	// HTTP fetches manifests and images (redirects to the release host allowed).
	HTTP *http.Client
}

// Manifest is a published firmware release of one board.
type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Board         string `json:"board"`
	Version       string `json:"version"`
	SourceSHA     string `json:"source_sha"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	ELFSHA256     string `json:"elf_sha256"`
}

// Image is a downloaded, verified application image.
type Image struct {
	Manifest Manifest
	Data     []byte
}

// MaxImageBytes bounds an application slot (6 MiB on the S3 boards).
const MaxImageBytes = 6 << 20

var (
	hex64     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hex40     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	semverish = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

// ErrNoRelease reports that a board has no published firmware.
var ErrNoRelease = errors.New("edgesetup: no published firmware for this board")

// FetchManifest reads and checks the latest manifest of board.
func (s ReleaseSource) FetchManifest(ctx context.Context, board string) (Manifest, error) {
	var m Manifest
	url := ""
	if s.ManifestURL != nil {
		url = s.ManifestURL(board)
	}
	if url == "" {
		return m, ErrNoRelease
	}
	raw, err := s.get(ctx, url, 64<<10)
	if err != nil {
		return m, fmt.Errorf("edgesetup: firmware manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("edgesetup: firmware manifest is not valid JSON: %w", err)
	}
	if m.SchemaVersion != 1 || m.Board != board || !semverish.MatchString(m.Version) ||
		!hex40.MatchString(m.SourceSHA) || !hex64.MatchString(m.SHA256) || !hex64.MatchString(m.ELFSHA256) ||
		m.Size < 1024 || m.Size > MaxImageBytes {
		return m, fmt.Errorf("edgesetup: firmware manifest for %s is not acceptable", board)
	}
	return m, nil
}

// Download fetches the image a manifest names and verifies size, SHA-256,
// project name, version and ELF identity, the checks the device repeats.
func (s ReleaseSource) Download(ctx context.Context, m Manifest) (Image, error) {
	if s.ImageURL == nil {
		return Image{}, ErrNoRelease
	}
	data, err := s.get(ctx, s.ImageURL(m), MaxImageBytes+1)
	if err != nil {
		return Image{}, fmt.Errorf("edgesetup: firmware image: %w", err)
	}
	if err := VerifyImage(data, m, s.Project); err != nil {
		return Image{}, err
	}
	return Image{Manifest: m, Data: data}, nil
}

// VerifyImage checks an application image against its manifest. The image
// descriptor follows the 24-byte image header and the first 8-byte segment
// header: version at 48, project name at 80, ELF SHA-256 at 176.
func VerifyImage(data []byte, m Manifest, project string) error {
	if int64(len(data)) != m.Size {
		return fmt.Errorf("edgesetup: image size %d does not match the manifest (%d)", len(data), m.Size)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		return errors.New("edgesetup: image SHA-256 does not match the manifest")
	}
	if len(data) < 208 || data[0] != 0xE9 || !bytes.Equal(data[32:36], []byte{0x32, 0x54, 0xCD, 0xAB}) {
		return errors.New("edgesetup: not an ESP application image")
	}
	cstr := func(b []byte) string {
		if i := bytes.IndexByte(b, 0); i >= 0 {
			return string(b[:i])
		}
		return string(b)
	}
	if v := cstr(data[48:80]); v != m.Version {
		return fmt.Errorf("edgesetup: image version %q does not match the manifest", v)
	}
	if project != "" && cstr(data[80:112]) != project {
		return errors.New("edgesetup: image belongs to another project")
	}
	if hex.EncodeToString(data[176:208]) != m.ELFSHA256 {
		return errors.New("edgesetup: image identity does not match the manifest")
	}
	return nil
}

func (s ReleaseSource) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	if s.HTTP == nil {
		return nil, errors.New("no HTTP client")
	}
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("release locations must use HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= limit {
		return nil, errors.New("response too large")
	}
	return data, nil
}
