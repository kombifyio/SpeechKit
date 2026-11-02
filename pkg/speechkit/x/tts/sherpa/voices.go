package sherpa

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bundled voice directory names. The installer lays both out under models\tts,
// sharing one espeak-ng-data tree.
const (
	VoiceDirGerman  = "vits-piper-de_DE-thorsten-medium"
	VoiceDirEnglish = "vits-piper-en_US-lessac-medium"
	EspeakDataDir   = "espeak-ng-data"
)

// Voice is one loaded Piper VITS model: the ONNX file, its tokens, and the
// espeak-ng data directory the phonemizer reads.
type Voice struct {
	ID       string
	Locale   string
	Model    string
	Tokens   string
	DataDir  string
	ModelDir string
}

// Voices is the pair a fresh install speaks with.
type Voices struct {
	German  Voice
	English Voice
	// DataDir is the shared espeak-ng data tree, when both voices use one.
	DataDir string
}

// ResolveVoices loads the German and English voices from root. root is the
// models\tts directory the installer ships: each voice in its own folder, and
// one espeak-ng-data directory next to them. A voice folder that still carries
// its own espeak-ng-data copy is accepted too, so a hand-unpacked archive works
// before the installer deduplicates it.
func ResolveVoices(root string) (Voices, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Voices{}, errors.New("sherpa tts: voice directory is required")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Voices{}, fmt.Errorf("sherpa tts: voice directory %s is not a directory", root)
	}
	shared := filepath.Join(root, EspeakDataDir)
	german, err := resolveVoice(filepath.Join(root, VoiceDirGerman), "de", shared)
	if err != nil {
		return Voices{}, err
	}
	english, err := resolveVoice(filepath.Join(root, VoiceDirEnglish), "en", shared)
	if err != nil {
		return Voices{}, err
	}
	out := Voices{German: german, English: english}
	if german.DataDir == shared && english.DataDir == shared {
		out.DataDir = shared
	}
	return out, nil
}

// VoiceForLocale picks the German voice for a de* locale and the English voice
// otherwise. An empty locale is English, matching the rest of the TTS surface.
func (v Voices) VoiceForLocale(locale string) Voice {
	if IsGermanLocale(locale) {
		return v.German
	}
	return v.English
}

// IsGermanLocale reports whether locale is German (de, de-DE, de_DE).
func IsGermanLocale(locale string) bool {
	return localeIsGerman(locale)
}

func resolveVoice(dir, locale, sharedData string) (Voice, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return Voice{}, fmt.Errorf("sherpa tts: voice directory %s is missing", dir)
	}
	model, err := findModel(dir)
	if err != nil {
		return Voice{}, err
	}
	tokens := filepath.Join(dir, "tokens.txt")
	if err := requireFile(tokens); err != nil {
		return Voice{}, fmt.Errorf("sherpa tts: %s: %w", dir, err)
	}
	dataDir := sharedData
	if err := requireDir(dataDir); err != nil {
		dataDir = filepath.Join(dir, EspeakDataDir)
		if err := requireDir(dataDir); err != nil {
			return Voice{}, fmt.Errorf("sherpa tts: %s has no espeak-ng-data (looked in %s and %s)", dir, sharedData, dataDir)
		}
	}
	return Voice{
		ID:       filepath.Base(dir),
		Locale:   locale,
		Model:    model,
		Tokens:   tokens,
		DataDir:  dataDir,
		ModelDir: dir,
	}, nil
}

func findModel(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.onnx"))
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("sherpa tts: %s has no .onnx model", dir)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("sherpa tts: %s has %d .onnx models; expected one", dir, len(matches))
	}
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("%s is missing", filepath.Base(path))
	}
	return nil
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func localeIsGerman(locale string) bool {
	locale = strings.ToLower(strings.TrimSpace(locale))
	locale = strings.ReplaceAll(locale, "_", "-")
	return locale == "de" || strings.HasPrefix(locale, "de-")
}
