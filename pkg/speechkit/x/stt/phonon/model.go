package phonon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa"
)

// File names of Fermion's unpacked archive.
const (
	ContainerFile      = "model.fermion"
	ConfigFile         = "config.json"
	PackedManifestFile = "packed_manifest.json"
)

// englishOnlyModelDir is the catalog directory name of Phonon-2.5 English.
const englishOnlyModelDir = "phonon-2.5-en"

// ModelLanguages returns the languages the model in dir transcribes, matched
// on the directory's base name as the model catalog installs it: Phonon-2.5
// English gets [sherpa.EnglishOnlyLanguages], every other directory
// [sherpa.ParakeetLanguages].
func ModelLanguages(dir string) []string {
	if strings.EqualFold(filepath.Base(filepath.Clean(dir)), englishOnlyModelDir) {
		return sherpa.EnglishOnlyLanguages
	}
	return sherpa.ParakeetLanguages
}

// ModelFiles are the three files of an unpacked Phonon-2.5 archive.
type ModelFiles struct {
	Dir       string
	Container string
	Config    string
	Manifest  string
}

// ResolveModelDir validates dir as an unpacked Phonon-2.5 archive and returns
// its files. dir must be an absolute, canonical path.
func ResolveModelDir(dir string) (ModelFiles, error) {
	if strings.TrimSpace(dir) == "" {
		return ModelFiles{}, errors.New("phonon: model directory is empty")
	}
	clean := filepath.Clean(dir)
	if clean != dir && filepath.ToSlash(clean) != filepath.ToSlash(dir) {
		return ModelFiles{}, fmt.Errorf("phonon: model directory must be in canonical form (got %q, want %q)", dir, clean)
	}
	if !filepath.IsAbs(clean) {
		return ModelFiles{}, fmt.Errorf("phonon: model directory must be absolute: %s", clean)
	}
	info, err := os.Stat(clean)
	if err != nil {
		return ModelFiles{}, fmt.Errorf("phonon: model directory: %w", err)
	}
	if !info.IsDir() {
		return ModelFiles{}, fmt.Errorf("phonon: model path is not a directory: %s", clean)
	}
	files := ModelFiles{Dir: clean}
	for _, part := range []struct {
		name string
		dst  *string
	}{
		{ContainerFile, &files.Container},
		{ConfigFile, &files.Config},
		{PackedManifestFile, &files.Manifest},
	} {
		path := filepath.Join(clean, part.name)
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return ModelFiles{}, fmt.Errorf("phonon: model directory %s has no %s", clean, part.name)
		}
		*part.dst = path
	}
	return files, nil
}

// IsModelDir reports whether path is a Phonon model directory: one that
// holds Fermion's container. Hosts use it to pick the engine; a directory
// with the container but a missing file is still a Phonon model, so
// [ResolveModelDir] reports what is missing instead of a sherpa-onnx error.
func IsModelDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, ContainerFile))
	return err == nil && info.Mode().IsRegular()
}

// PackedManifest is the part of packed_manifest.json the loader uses.
type PackedManifest struct {
	Format          string `json:"format"`
	Container       string `json:"container"`
	ContainerSHA256 string `json:"container_sha256"`
	BaseModel       string `json:"base_model"`
	License         string `json:"license"`
}

// ReadPackedManifest reads and validates packed_manifest.json at path.
func ReadPackedManifest(path string) (PackedManifest, error) {
	var m PackedManifest
	raw, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("phonon: read packed manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("phonon: packed manifest: %w", err)
	}
	if m.Format != ContainerFormat {
		return m, fmt.Errorf("phonon: packed manifest format %q, want %q", m.Format, ContainerFormat)
	}
	if m.Container != ContainerFile {
		return m, fmt.Errorf("phonon: packed manifest container %q, want %q", m.Container, ContainerFile)
	}
	if len(m.ContainerSHA256) != 64 {
		return m, errors.New("phonon: packed manifest has no container_sha256")
	}
	return m, nil
}

// Load builds the weight image of the model in dir. It first checks the
// container against the SHA-256 in packed_manifest.json and refuses a
// mismatch, so a truncated or modified download never reaches the engine.
func Load(dir string) (*Image, error) {
	return LoadInto(dir, nil)
}

// LoadInto is [Load] with the weight image in memory from allocate; a nil
// allocate uses the Go heap.
func LoadInto(dir string, allocate Allocator) (*Image, error) {
	files, err := ResolveModelDir(dir)
	if err != nil {
		return nil, err
	}
	pm, err := ReadPackedManifest(files.Manifest)
	if err != nil {
		return nil, err
	}
	m := meta{baseModel: pm.BaseModel, license: pm.License, wantSHA: strings.ToLower(pm.ContainerSHA256)}
	if m.baseModel == "" {
		m.baseModel = DefaultBaseModel
	}
	if m.license == "" {
		m.license = DefaultLicense
	}
	return buildImage(files.Container, files.Config, m, allocate)
}
