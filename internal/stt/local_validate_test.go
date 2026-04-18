package stt

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateModelPath(t *testing.T) {
	absOK := "/opt/speechkit/models/ggml-small.bin"
	if runtime.GOOS == "windows" {
		absOK = `C:\SpeechKit\models\ggml-small.bin`
	}

	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"empty", "", true},
		{"valid absolute", absOK, false},
		{"relative rejected", "models/ggml-small.bin", true},
		{"traversal rejected", "/opt/../etc/ggml-small.bin", true},
		{"non-ggml filename", filepath.Join(filepath.Dir(absOK), "whisper.pth"), true},
		{"shell metachar", filepath.Join(filepath.Dir(absOK), "ggml;rm -rf.bin"), true},
		{"spaces in name", filepath.Join(filepath.Dir(absOK), "ggml- small.bin"), true},
		{"valid turbo", filepath.Join(filepath.Dir(absOK), "ggml-large-v3-turbo.bin"), false},
		{"valid quant", filepath.Join(filepath.Dir(absOK), "ggml-medium.en-q5_0.bin"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModelPath(tc.path)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateModelPath(%q) = nil, want error", tc.path)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateModelPath(%q) = %v, want nil", tc.path, err)
			}
		})
	}
}
