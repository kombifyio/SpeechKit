package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
)

func resolveLiveKitOpenWakeWord(cfg *config.Config, backend string) (resolvedWakewordAssets, error) {
	var out resolvedWakewordAssets
	out.Backend = backend
	exe, err := os.Executable()
	if err != nil {
		return out, fmt.Errorf("resolve executable path: %w", err)
	}
	exeDir := filepath.Dir(exe)
	paths := liveKitOpenWakeWordAssetPaths(cfg)
	out.SidecarPath = filepath.Join(exeDir, defaultOpenWakewordSidecarBinary)
	out.OnnxRuntimePath = filepath.Join(exeDir, defaultOpenWakewordRuntimeDLLName)
	out.MelspecModelPath = paths["melspec"]
	out.EmbeddingModelPath = paths["embedding"]
	out.ModelPath = paths["phrase"]

	for label, p := range map[string]string{
		"sidecar":     out.SidecarPath,
		"onnxruntime": out.OnnxRuntimePath,
		"melspec":     out.MelspecModelPath,
		"embedding":   out.EmbeddingModelPath,
		"phrase":      out.ModelPath,
	} {
		if !pathIsRegularFileOrDir(p) {
			return resolvedWakewordAssets{}, fmt.Errorf("wake-word %s asset missing at %s (rebuild the SpeechKit bundle via scripts/build.ps1 or download the wake-word model artifacts)", label, p)
		}
	}

	out.DefaultMode = config.NormalizeWakewordDefaultMode(cfg.Wakeword.DefaultMode)
	out.AutoEnd = cfg.Wakeword.AutoEnd
	out.DisplayPhrase = resolvedWakewordDisplayPhrase(cfg)
	return out, nil
}

func resolveSTTPhraseWakeword(cfg *config.Config, backend string) (resolvedWakewordAssets, error) {
	return resolvedWakewordAssets{
		Backend:       backend,
		DefaultMode:   config.NormalizeWakewordDefaultMode(cfg.Wakeword.DefaultMode),
		DisplayPhrase: resolvedWakewordDisplayPhrase(cfg),
		AutoEnd:       cfg.Wakeword.AutoEnd,
	}, nil
}

func resolvedWakewordDisplayPhrase(cfg *config.Config) string {
	if cfg == nil {
		return "Wake phrase"
	}
	display := strings.TrimSpace(cfg.Wakeword.Phrase)
	if id := strings.TrimSpace(cfg.Wakeword.PhraseID); id != "" {
		if entry := wakeword.LookupPhrase(id); entry != nil && display == "" {
			display = entry.DisplayName
		}
	}
	if display == "" {
		return "Wake phrase"
	}
	return display
}

// effectiveWakewordThreshold resolves the runtime detector threshold for
// the active backend. Returns the user-set Threshold when in (0, 1], else
// the per-backend default.
//
// openWakeWord uses the upstream-canonical 0.5 baseline (Wyoming/openWakeWord
// docs). The earlier per-phrase hand-tuning (hey_mira=0.08, hey_computer=0.10,
// hey_quby=0.22) was field-calibrated when the prior Go port mis-scaled
// scores and is left as the baseline only when explicitly opted in by the
// user via [wakeword] threshold = ... — set 0 to use the canonical default.
//
// Sherpa-onnx KWS uses 0.25 as documented by the upstream Python example;
// per-keyword sensitivity is tuned via the `:boost` suffix in keywords.txt.
//
// STT phrase matching returns 0 because the detector decides on transcript
// substring match, not acoustic probability.
func effectiveWakewordThreshold(cfg *config.Config, backend string) float32 {
	if cfg != nil && cfg.Wakeword.Threshold > 0 && cfg.Wakeword.Threshold <= 1 {
		return float32(cfg.Wakeword.Threshold)
	}
	switch config.NormalizeWakewordBackend(backend) {
	case config.WakewordBackendSherpaKWS:
		return 0.25
	case config.WakewordBackendSTTPhrase:
		return 0
	case config.WakewordBackendLiveKitOpenWakeWord:
		return 0.5
	default:
		return 0.5
	}
}
