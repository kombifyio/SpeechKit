package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsPackagingIncludesBundledStarterWhisperModel(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	files := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join(repoRoot, "scripts", "prepare-whisper-runtime.ps1"),
			want: []string{"ggml-small.bin", "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"},
		},
		{
			path: filepath.Join(repoRoot, "installer", "speechkit.nsi"),
			want: []string{
				`$INSTDIR\models`,
				`${STAGE_DIR}\models\*`,
				`${STAGE_DIR}\speechkit-wakeword.exe`,
				`${STAGE_DIR}\speechkit-openwakeword.exe`,
				`$INSTDIR\wakeword-kws`,
				`${STAGE_DIR}\wakeword-kws\*`,
			},
		},
		{
			path: filepath.Join(repoRoot, "installer", "wix", "SpeechKit.wxs"),
			want: []string{
				"StarterWhisperModel",
				`dist\windows\SpeechKit\models\ggml-small.bin`,
				"WakewordSidecarExe",
				`dist\windows\SpeechKit\speechkit-wakeword.exe`,
				"OpenWakewordSidecarExe",
				`dist\windows\SpeechKit\speechkit-openwakeword.exe`,
				"OpenWakewordMelspecModel",
				`dist\windows\SpeechKit\models\wakeword\melspectrogram.onnx`,
				"WakewordKwsEncoder",
				"WakewordKwsKeywords",
				"OnnxRuntimeDll",
				"SherpaOnnxCApiDll",
				"SherpaOnnxCxxApiDll",
			},
		},
		{
			path: filepath.Join(repoRoot, "installer", "wix", "build-msi.ps1"),
			want: []string{
				`models\ggml-small.bin`,
				"speechkit-wakeword.exe",
				"speechkit-openwakeword.exe",
				`models\wakeword\melspectrogram.onnx`,
				`wakeword-kws\keywords.txt`,
				"sherpa-onnx-c-api.dll",
			},
		},
	}

	for _, file := range files {
		t.Run(filepath.Base(file.path), func(t *testing.T) {
			data, err := os.ReadFile(file.path)
			if err != nil {
				t.Fatalf("read %s: %v", file.path, err)
			}
			body := string(data)
			for _, want := range file.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing %q", file.path, want)
				}
			}
		})
	}
}
