package sherpa

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// TokensFile is the vocabulary file every model directory carries.
const TokensFile = "tokens.txt"

// minEncoderBytes rejects a truncated download: the smallest supported
// encoder (Parakeet TDT 0.6B int8) is about 650 MB.
const minEncoderBytes = 100_000_000

// ParakeetLanguages are the 25 languages Parakeet TDT 0.6B v3 and its
// derivatives transcribe, as ISO 639-1 codes. The model detects which one is
// spoken; it cannot be pinned to one.
var ParakeetLanguages = []string{
	"bg", "cs", "da", "de", "el", "en", "es", "et", "fi", "fr", "hr", "hu", "it",
	"lt", "lv", "mt", "nl", "pl", "pt", "ro", "ru", "sk", "sl", "sv", "uk",
}

// ModelFiles are the four files of a nemo_transducer model directory.
type ModelFiles struct {
	Dir     string
	Encoder string
	Decoder string
	Joiner  string
	Tokens  string
}

// ResolveModelDir validates dir as a nemo_transducer model directory and
// returns its files. dir must be an absolute, canonical path; the int8 file
// names are preferred over the float ones when both exist.
func ResolveModelDir(dir string) (ModelFiles, error) {
	if strings.TrimSpace(dir) == "" {
		return ModelFiles{}, errors.New("sherpa: model directory is empty")
	}
	clean := filepath.Clean(dir)
	if clean != dir && filepath.ToSlash(clean) != filepath.ToSlash(dir) {
		return ModelFiles{}, fmt.Errorf("sherpa: model directory must be in canonical form (got %q, want %q)", dir, clean)
	}
	if !filepath.IsAbs(clean) {
		return ModelFiles{}, fmt.Errorf("sherpa: model directory must be absolute: %s", clean)
	}
	info, err := os.Stat(clean)
	if err != nil {
		return ModelFiles{}, fmt.Errorf("sherpa: model directory: %w", err)
	}
	if !info.IsDir() {
		return ModelFiles{}, fmt.Errorf("sherpa: model path is not a directory: %s", clean)
	}
	files := ModelFiles{Dir: clean}
	for _, part := range []struct {
		name string
		dst  *string
	}{
		{"encoder", &files.Encoder},
		{"decoder", &files.Decoder},
		{"joiner", &files.Joiner},
	} {
		path, err := firstRegularFile(clean, part.name+".int8.onnx", part.name+".onnx")
		if err != nil {
			return ModelFiles{}, err
		}
		*part.dst = path
	}
	tokens, err := firstRegularFile(clean, TokensFile)
	if err != nil {
		return ModelFiles{}, err
	}
	files.Tokens = tokens
	if info, err := os.Stat(files.Encoder); err == nil && info.Size() < minEncoderBytes {
		return ModelFiles{}, fmt.Errorf("sherpa: encoder %s is %d bytes; the download is truncated or not a supported model", files.Encoder, info.Size())
	}
	return files, nil
}

// IsModelDir reports whether path is a directory ResolveModelDir accepts.
// Hosts use it to tell a sherpa model from a whisper.cpp ggml file.
func IsModelDir(path string) bool {
	_, err := ResolveModelDir(path)
	return err == nil
}

func firstRegularFile(dir string, names ...string) (string, error) {
	for _, name := range names {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("sherpa: model directory %s has no %s", dir, strings.Join(names, " or "))
}

// SupportsLanguage reports whether a request language can be served by a
// model transcribing languages. An empty, "auto" or "multi" request always
// can; a regional tag is matched on its primary subtag. An empty languages
// list accepts everything.
func SupportsLanguage(languages []string, requested string) bool {
	code := NormalizeLanguage(requested)
	if code == "" || len(languages) == 0 {
		return true
	}
	return slices.Contains(languages, code)
}

// NormalizeLanguage reduces a request language to the primary subtag
// ("de-AT" → "de") and maps the auto-detect spellings to "".
func NormalizeLanguage(requested string) string {
	code := strings.ToLower(strings.TrimSpace(requested))
	if code == "" || code == "auto" || code == "multi" {
		return ""
	}
	code = strings.ReplaceAll(code, "_", "-")
	if primary, _, found := strings.Cut(code, "-"); found {
		code = primary
	}
	return code
}
