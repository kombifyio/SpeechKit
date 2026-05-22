package tts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewPiper_RequiresVoiceDir(t *testing.T) {
	if _, err := NewPiper(PiperOpts{}); err == nil {
		t.Fatal("expected error when VoiceDir is empty")
	}
}

func TestNewPiper_DefaultBinaryAndTimeout(t *testing.T) {
	p, err := NewPiper(PiperOpts{VoiceDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewPiper: %v", err)
	}
	if p.binary != "piper" {
		t.Errorf("default binary = %q, want piper", p.binary)
	}
	if p.timeout.Seconds() != 30 {
		t.Errorf("default timeout = %v, want 30s", p.timeout)
	}
}

func TestPiper_ResolveVoice(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewPiper(PiperOpts{
		VoiceDir: dir,
		DefaultVoices: map[string]string{
			"en": "en_US-amy-medium.onnx",
			"de": "de_DE-thorsten-medium.onnx",
			"fr": "fr_FR-siwis-medium.onnx",
		},
	})

	cases := []struct {
		name             string
		requestedVoice   string
		requestedLocale  string
		wantBasenameLike string
	}{
		{"explicit voice with suffix", "voice-en.onnx", "", "voice-en.onnx"},
		{"explicit voice no suffix", "voice-en", "", "voice-en.onnx"},
		{"locale en-US falls back to en default", "", "en-US", "en_US-amy-medium.onnx"},
		{"locale de", "", "de", "de_DE-thorsten-medium.onnx"},
		{"locale fr_FR underscore form", "", "fr_FR", "fr_FR-siwis-medium.onnx"},
		{"unknown locale falls back to en", "", "ja", "en_US-amy-medium.onnx"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, err := p.resolveVoice(c.requestedVoice, c.requestedLocale)
			if err != nil {
				t.Fatalf("resolveVoice: %v", err)
			}
			if !endsWith(path, c.wantBasenameLike) {
				t.Errorf("resolveVoice(%q, %q) = %q, want it to end with %q", c.requestedVoice, c.requestedLocale, path, c.wantBasenameLike)
			}
		})
	}
}

func TestPiper_NoDefaultVoiceForLocale(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewPiper(PiperOpts{
		VoiceDir:      dir,
		DefaultVoices: map[string]string{}, // explicitly empty
	})
	// Override the default-prefilled map to truly have no voice.
	p.defaults = map[string]string{}
	_, err := p.resolveVoice("", "")
	if err == nil {
		t.Fatal("expected error when no default voice + no explicit voice")
	}
}

func TestPiper_NameIsStable(t *testing.T) {
	p, _ := NewPiper(PiperOpts{VoiceDir: t.TempDir()})
	if p.Name() != "piper" {
		t.Errorf("Name = %q, want piper", p.Name())
	}
}

func TestNormaliseLocaleShort(t *testing.T) {
	cases := map[string]string{
		"":        "en",
		"  ":      "en",
		"en":      "en",
		"de":      "de",
		"de-DE":   "de",
		"EN_us":   "en",
		"DE_DE":   "de",
		"fr_FR":   "fr",
		" en-US ": "en",
	}
	for in, want := range cases {
		if got := normaliseLocaleShort(in); got != want {
			t.Errorf("normaliseLocaleShort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadWAVSampleRate(t *testing.T) {
	// Build a fake 44-byte WAV header with sample rate 22050.
	hdr := make([]byte, 44)
	// Bytes 24-27 = sample rate LE.
	hdr[24] = 0x22
	hdr[25] = 0x56
	hdr[26] = 0x00
	hdr[27] = 0x00
	if got := readWAVSampleRate(hdr); got != 22050 {
		t.Errorf("readWAVSampleRate = %d, want 22050", got)
	}

	// Short buffer falls back to 16000.
	if got := readWAVSampleRate([]byte{0x01, 0x02}); got != 16000 {
		t.Errorf("short buf: got %d, want 16000", got)
	}
}

func TestEstimateWAVDuration(t *testing.T) {
	// 44 bytes header + 32000 bytes data at 16 kHz mono S16:
	//   32000 / (2 * 16000) = 1.0 second.
	audio := make([]byte, 44+32000)
	got := estimateWAVDuration(audio, 16000)
	if got.Seconds() != 1.0 {
		t.Errorf("duration = %v, want 1.0 s", got)
	}

	// Below header → zero.
	if got := estimateWAVDuration([]byte{1, 2, 3}, 16000); got != 0 {
		t.Errorf("undersized got %v, want 0", got)
	}
}

func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

func TestParsePiperVoiceFilename(t *testing.T) {
	cases := []struct {
		name   string
		want   PiperVoiceInfo
		wantOK bool
	}{
		{
			name:   "en_US-amy-medium.onnx",
			want:   PiperVoiceInfo{Filename: "en_US-amy-medium.onnx", Locale: "en", Region: "US", Name: "amy", Quality: "medium"},
			wantOK: true,
		},
		{
			name:   "de_DE-thorsten-medium.onnx",
			want:   PiperVoiceInfo{Filename: "de_DE-thorsten-medium.onnx", Locale: "de", Region: "DE", Name: "thorsten", Quality: "medium"},
			wantOK: true,
		},
		{
			name:   "fr_FR-siwis-low.onnx",
			want:   PiperVoiceInfo{Filename: "fr_FR-siwis-low.onnx", Locale: "fr", Region: "FR", Name: "siwis", Quality: "low"},
			wantOK: true,
		},
		{
			name:   "voice-en.onnx",
			want:   PiperVoiceInfo{Filename: "voice-en.onnx", Name: "en"},
			wantOK: true,
		},
		{
			name:   "custom.onnx",
			want:   PiperVoiceInfo{Filename: "custom.onnx"},
			wantOK: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parsePiperVoiceFilename(c.name)
			if got.Filename != c.want.Filename {
				t.Errorf("Filename = %q, want %q", got.Filename, c.want.Filename)
			}
			if got.Locale != c.want.Locale {
				t.Errorf("Locale = %q, want %q", got.Locale, c.want.Locale)
			}
			if got.Region != c.want.Region {
				t.Errorf("Region = %q, want %q", got.Region, c.want.Region)
			}
			if got.Name != c.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, c.want.Name)
			}
			if got.Quality != c.want.Quality {
				t.Errorf("Quality = %q, want %q", got.Quality, c.want.Quality)
			}
		})
	}
}

func TestListPiperVoices_EmptyDir(t *testing.T) {
	got, err := ListPiperVoices("")
	if err != nil {
		t.Fatalf("ListPiperVoices(\"\"): %v", err)
	}
	if got != nil {
		t.Errorf("empty dir = %v, want nil", got)
	}

	got, err = ListPiperVoices("/does/not/exist/anywhere")
	if err != nil {
		t.Fatalf("missing dir error: %v", err)
	}
	if got != nil {
		t.Errorf("missing dir = %v, want nil (graceful)", got)
	}
}

func TestListPiperVoices_FiltersAndSorts(t *testing.T) {
	dir := t.TempDir()
	// Files: 2 onnx (1 well-formed, 1 custom), 1 non-onnx, 1 dir.
	mustWrite(t, filepath.Join(dir, "en_US-amy-medium.onnx"), []byte("fake-model-bytes"))
	mustWrite(t, filepath.Join(dir, "de_DE-thorsten-medium.onnx"), []byte("fake"))
	mustWrite(t, filepath.Join(dir, "voice-en.onnx"), []byte("fake"))
	mustWrite(t, filepath.Join(dir, "README.txt"), []byte("ignored"))
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got, err := ListPiperVoices(dir)
	if err != nil {
		t.Fatalf("ListPiperVoices: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d voices, want 3 (%v)", len(got), got)
	}
	wantOrder := []string{"de_DE-thorsten-medium.onnx", "en_US-amy-medium.onnx", "voice-en.onnx"}
	for i, want := range wantOrder {
		if got[i].Filename != want {
			t.Errorf("idx %d: filename = %q, want %q", i, got[i].Filename, want)
		}
	}
	if got[0].Locale != "de" || got[0].Region != "DE" {
		t.Errorf("de voice: locale/region = %q/%q, want de/DE", got[0].Locale, got[0].Region)
	}
	if got[1].Locale != "en" || got[1].Region != "US" {
		t.Errorf("en voice: locale/region = %q/%q, want en/US", got[1].Locale, got[1].Region)
	}
	if got[1].SizeKB < 0 {
		t.Errorf("size_kb negative: %d", got[1].SizeKB)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile %q: %v", path, err)
	}
}
