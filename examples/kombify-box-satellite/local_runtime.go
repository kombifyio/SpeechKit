//go:build windows && cgo

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	sttlocal "github.com/kombifyio/SpeechKit/pkg/speechkit/stt/local"
)

func speechKitLocalDir() string {
	if value := strings.TrimSpace(os.Getenv("SPEECHKIT_LOCAL_DIR")); value != "" {
		return value
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "SpeechKit")
	}
	return ""
}

func speechKitModelsDir() string {
	if value := strings.TrimSpace(os.Getenv("SPEECHKIT_MODELS_DIR")); value != "" {
		return value
	}
	if root := speechKitLocalDir(); root != "" {
		return filepath.Join(root, "models")
	}
	return ""
}

func defaultPiperVoiceDir() string {
	if value := strings.TrimSpace(os.Getenv("SPEECHKIT_PIPER_VOICE_DIR")); value != "" {
		return value
	}
	if root := speechKitLocalDir(); root != "" {
		return filepath.Join(root, "piper-voices")
	}
	return ""
}

func (c *Config) resolveWhisperModelPath() string {
	configured := []string{
		os.Getenv("SPEECHKIT_WHISPER_MODEL_PATH"),
		c.Local.ModelPath,
	}
	for _, candidate := range configured {
		if path := existingFile(candidate); path != "" {
			return path
		}
	}

	model := strings.TrimSpace(c.Local.Model)
	if model == "" {
		model = "ggml-small.bin"
	}
	if filepath.IsAbs(model) {
		return existingFile(model)
	}

	candidates := []string{
		filepath.Join("models", model),
		filepath.Join("examples", "kombify-box-satellite", "models", model),
	}
	if dir := speechKitModelsDir(); dir != "" {
		candidates = append([]string{filepath.Join(dir, model)}, candidates...)
	}
	if root := speechKitLocalDir(); root != "" {
		candidates = append([]string{filepath.Join(root, model)}, candidates...)
	}
	for _, candidate := range candidates {
		if path := existingFile(candidate); path != "" {
			return path
		}
	}
	return ""
}

func whisperModelHint(c *Config) string {
	model := "ggml-small.bin"
	if c != nil && strings.TrimSpace(c.Local.Model) != "" {
		model = strings.TrimSpace(c.Local.Model)
	}
	locations := []string{}
	if dir := speechKitModelsDir(); dir != "" {
		locations = append(locations, filepath.Join(dir, model))
	}
	locations = append(locations,
		filepath.Join("models", model),
		filepath.Join("examples", "kombify-box-satellite", "models", model),
	)
	return strings.Join(locations, " oder ")
}

func existingFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return filepath.Clean(path)
}

func piperUsable(binary, voiceDir string) bool {
	if strings.TrimSpace(voiceDir) == "" {
		return false
	}
	if entries, err := filepath.Glob(filepath.Join(voiceDir, "*.onnx")); err != nil || len(entries) == 0 {
		return false
	}
	return findPiperBinary(binary) != ""
}

func findPiperBinary(configured string) string {
	if path := existingFile(os.Getenv("SPEECHKIT_PIPER_BINARY")); path != "" {
		return path
	}
	if path := existingFile(configured); path != "" {
		return path
	}
	names := []string{}
	if strings.TrimSpace(configured) != "" {
		names = append(names, configured)
	}
	names = append(names, "piper.exe", "piper")
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if root := speechKitLocalDir(); root != "" {
		for _, candidate := range []string{
			filepath.Join(root, "piper.exe"),
			filepath.Join(root, "bin", "piper.exe"),
			filepath.Join(root, "piper", "piper.exe"),
			filepath.Join(root, "piper", "piper", "piper.exe"),
		} {
			if path := existingFile(candidate); path != "" {
				return path
			}
		}
	}
	return ""
}

func isLoopbackBaseURL(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		host = u.Host
		if h, _, err := net.SplitHostPort(u.Host); err == nil {
			host = h
		}
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func startLocalSTTIfNeeded(ctx context.Context, provider stt.STTProvider) (func(), error) {
	local, ok := provider.(*sttlocal.Provider)
	if !ok {
		return func() {}, nil
	}
	status := local.VerifyInstallation()
	if len(status.Problems) > 0 {
		return nil, fmt.Errorf("local STT nicht bereit: %s", strings.Join(status.Problems, "; "))
	}
	log.Printf("[local] starte whisper-server (%s)", status.ModelPath)
	if err := local.StartServer(ctx); err != nil {
		return nil, err
	}
	return local.StopServer, nil
}
