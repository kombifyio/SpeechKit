package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestStartDesktopWakeword_DisabledIsNoop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = false

	got := startDesktopWakeword(context.Background(), cfg, &appState{}, nil, nil)
	if got != nil {
		t.Fatalf("expected nil runtime when disabled, got %+v", got)
	}
}

func TestStartDesktopWakeword_MissingHotkeyManagerIsNoop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true

	state := &appState{}
	got := startDesktopWakeword(context.Background(), cfg, state, nil, nil)
	if got != nil {
		t.Fatalf("expected nil runtime when hotkey manager missing, got %+v", got)
	}
}

func TestResolveWakeword_MissingModelDirSurfacesError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true

	_, err := resolveWakeword(cfg)
	if err == nil {
		t.Fatal("expected error when wakeword-kws bundle directory is absent")
	}
}

func TestResolveWakeword_LiveKitBackendResolvesRealAssetsPath(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true
	cfg.Wakeword.Backend = config.WakewordBackendLiveKitOpenWakeWord

	_, err := resolveWakeword(cfg)
	if err == nil {
		t.Fatal("expected missing-asset error when LiveKit/openWakeWord bundle assets are absent")
	}
	if strings.Contains(err.Error(), "not wired") || strings.Contains(err.Error(), "planned") {
		t.Fatalf("error = %q, should report concrete missing assets instead of planned/unwired status", err.Error())
	}
	if !strings.Contains(err.Error(), "asset missing") {
		t.Fatalf("error = %q, want concrete missing asset status", err.Error())
	}
}

func TestResolveWakeword_STTPhraseBackendDoesNotRequireSidecarAssets(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.Enabled = true
	cfg.Wakeword.Backend = config.WakewordBackendSTTPhrase
	cfg.Wakeword.PhraseID = "hey_mira"

	got, err := resolveWakeword(cfg)
	if err != nil {
		t.Fatalf("resolveWakeword STT phrase backend: %v", err)
	}
	if got.Backend != config.WakewordBackendSTTPhrase {
		t.Fatalf("backend = %q, want %q", got.Backend, config.WakewordBackendSTTPhrase)
	}
	if got.DisplayPhrase != "Hey Mira" {
		t.Fatalf("DisplayPhrase = %q, want Hey Mira", got.DisplayPhrase)
	}
}

func TestWakewordConfigEqualsIncludesBackend(t *testing.T) {
	a := config.WakewordConfig{Backend: config.WakewordBackendSherpaKWS}
	b := config.WakewordConfig{Backend: config.WakewordBackendLiveKitOpenWakeWord}

	if wakewordConfigEquals(a, b) {
		t.Fatal("wakewordConfigEquals returned true for different wake-word backends")
	}
}

func TestWakewordConfigEqualsIncludesAutoEnd(t *testing.T) {
	a := config.WakewordConfig{
		Backend: config.WakewordBackendSherpaKWS,
		AutoEnd: config.WakewordAutoEndConfig{
			SilenceCutoffSec: 10,
			ExitPhrases:      []string{"danke"},
		},
	}
	b := a
	b.AutoEnd.ExitPhrases = []string{"stop"}

	if wakewordConfigEquals(a, b) {
		t.Fatal("wakewordConfigEquals returned true when auto-end exit phrases changed")
	}
}

// TestWakewordConfigEqualsIncludesDebugMode guards the invariant that flipping
// DebugMode forces a sidecar restart — the sidecar gets --debug at launch and
// won't change behaviour without a respawn.
func TestWakewordConfigEqualsIncludesDebugMode(t *testing.T) {
	a := config.WakewordConfig{Backend: config.WakewordBackendLiveKitOpenWakeWord, DebugMode: false}
	b := a
	b.DebugMode = true
	if wakewordConfigEquals(a, b) {
		t.Fatal("wakewordConfigEquals returned true after DebugMode flip — settings save will not restart the sidecar with --debug")
	}
}

// TestWakewordSidecarArgsIncludesDebugWhenEnabled checks the actual flag
// pipeline: when [wakeword] debug_mode = true, the sidecar invocation must
// carry --debug. Otherwise the sidecar runs in quiet mode.
func TestWakewordSidecarArgsIncludesDebugWhenEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.DebugMode = true
	cfg.Wakeword.PhraseID = "hey_kombify"
	cfg.Wakeword.Backend = config.WakewordBackendLiveKitOpenWakeWord
	resolved := resolvedWakewordAssets{
		Backend:            config.WakewordBackendLiveKitOpenWakeWord,
		DefaultMode:        "voice_agent",
		DisplayPhrase:      "Hey Kombify",
		ModelPath:          "fake-phrase.onnx",
		MelspecModelPath:   "fake-mel.onnx",
		EmbeddingModelPath: "fake-emb.onnx",
		OnnxRuntimePath:    "fake-onnx.dll",
	}
	args := wakewordSidecarArgs(cfg, resolved)
	found := false
	for _, a := range args {
		if a == "--debug" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --debug in sidecar args when DebugMode=true; got: %v", args)
	}

	cfg.Wakeword.DebugMode = false
	for _, a := range wakewordSidecarArgs(cfg, resolved) {
		if a == "--debug" {
			t.Fatalf("did NOT expect --debug when DebugMode=false; got it in args")
		}
	}
}

func TestWakewordSidecarArgsOmitsTrainingFlagsByDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.PhraseID = "hey_quby"
	cfg.Wakeword.Backend = config.WakewordBackendSherpaKWS
	// TrainingData zero-value → LocalCaptureEnabled is false.
	resolved := resolvedWakewordAssets{
		Backend:       config.WakewordBackendSherpaKWS,
		DefaultMode:   "voice_agent",
		DisplayPhrase: "Hey Quby",
		ModelDir:      t.TempDir(),
		KeywordsFile:  "keywords.txt",
	}
	for _, a := range wakewordSidecarArgs(cfg, resolved) {
		if a == "--training-enabled" || a == "--training-dir" {
			t.Fatalf("default config must not pass training flags; got %v", a)
		}
	}
}

func TestWakewordSidecarArgsIncludesTrainingFlagsWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.PhraseID = "hey_quby"
	cfg.Wakeword.Backend = config.WakewordBackendSherpaKWS
	cfg.Wakeword.TrainingData.LocalCaptureEnabled = true
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir
	cfg.Wakeword.TrainingData.PreRollMs = 2000
	cfg.Wakeword.TrainingData.PostRollMs = 750
	resolved := resolvedWakewordAssets{
		Backend:       config.WakewordBackendSherpaKWS,
		DefaultMode:   "voice_agent",
		DisplayPhrase: "Hey Quby",
		ModelDir:      dir,
		KeywordsFile:  "keywords.txt",
	}
	args := wakewordSidecarArgs(cfg, resolved)

	wantPairs := map[string]string{
		"--training-dir":          dir,
		"--training-pre-roll-ms":  "2000",
		"--training-post-roll-ms": "750",
	}
	gotEnabled := false
	for i, a := range args {
		if a == "--training-enabled" {
			gotEnabled = true
			continue
		}
		if want, ok := wantPairs[a]; ok {
			if i+1 >= len(args) {
				t.Fatalf("%s missing value", a)
			}
			if args[i+1] != want {
				t.Errorf("%s = %q, want %q", a, args[i+1], want)
			}
			delete(wantPairs, a)
		}
	}
	if !gotEnabled {
		t.Errorf("missing --training-enabled in args: %v", args)
	}
	for k := range wantPairs {
		t.Errorf("missing flag %s in args: %v", k, args)
	}
}

func TestEffectiveWakewordThresholdUsesConcreteDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.Wakeword.PhraseID = "hey_quby"

	if got := effectiveWakewordThreshold(cfg, config.WakewordBackendSherpaKWS); got != 0.25 {
		t.Fatalf("Sherpa threshold = %.2f, want sherpa-onnx upstream default 0.25", got)
	}
	if got := effectiveWakewordThreshold(cfg, config.WakewordBackendLiveKitOpenWakeWord); got != 0.5 {
		t.Fatalf("LiveKit/openWakeWord threshold = %.2f, want Wyoming/openWakeWord canonical default 0.5", got)
	}
	if got := effectiveWakewordThreshold(cfg, config.WakewordBackendSTTPhrase); got != 0 {
		t.Fatalf("STT phrase threshold = %.2f, want 0 (substring match, no acoustic probability)", got)
	}
	cfg.Wakeword.Threshold = 0.77
	if got := effectiveWakewordThreshold(cfg, config.WakewordBackendLiveKitOpenWakeWord); got != 0.77 {
		t.Fatalf("manual threshold override = %.2f, want user-pinned 0.77", got)
	}
}

func TestStageSelectedSherpaKeywordsFileFiltersByLabel(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "keywords.txt")
	content := strings.Join([]string{
		"▁HE Y ▁QU B Y :1.5 @hey_quby",
		"▁HE Y ▁C U B I :1.5 @hey_quby",
		"▁HE Y ▁COMP U TER :1.5 @hey_computer",
		"▁HE Y ▁JA R VI S :1.5 @hey_jarvis",
	}, "\n") + "\n"
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatalf("write source keywords: %v", err)
	}

	staged, cleanupFiles, err := stageSelectedSherpaKeywordsFile(source, "hey_quby")
	if err != nil {
		t.Fatalf("stageSelectedSherpaKeywordsFile: %v", err)
	}
	t.Cleanup(func() { cleanupPaths(cleanupFiles) })
	if staged == source {
		t.Fatal("expected filtered keywords to be staged to a temp file")
	}
	if len(cleanupFiles) != 1 || cleanupFiles[0] != staged {
		t.Fatalf("cleanupFiles = %#v, want staged file", cleanupFiles)
	}

	gotBytes, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("read staged keywords: %v", err)
	}
	got := string(gotBytes)
	if !strings.Contains(got, "@hey_quby") {
		t.Fatalf("staged keywords = %q, want selected label", got)
	}
	if strings.Contains(got, "@hey_computer") || strings.Contains(got, "@hey_jarvis") {
		t.Fatalf("staged keywords = %q, want only selected label", got)
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n") + 1; lines != 2 {
		t.Fatalf("staged line count = %d, want 2 Quby variants; content=%q", lines, got)
	}
}

func TestStageSelectedSherpaKeywordsFileWithoutLabelKeepsSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "keywords.txt")
	if err := os.WriteFile(source, []byte("▁A LE X A :2.0 @alexa\n"), 0o600); err != nil {
		t.Fatalf("write source keywords: %v", err)
	}

	staged, cleanupFiles, err := stageSelectedSherpaKeywordsFile(source, "")
	if err != nil {
		t.Fatalf("stageSelectedSherpaKeywordsFile: %v", err)
	}
	if staged != source {
		t.Fatalf("staged = %q, want original source %q", staged, source)
	}
	if len(cleanupFiles) != 0 {
		t.Fatalf("cleanupFiles = %#v, want none for unfiltered source", cleanupFiles)
	}
}

func TestWakePhraseTranscriptMatchesCatalogAliases(t *testing.T) {
	aliases := wakePhraseAliases(config.WakewordConfig{PhraseID: "hey_quby"}, "Hey Quby (Cubi / Kubi)")
	for _, transcript := range []string{
		"hey quby",
		"Hey Cubi, bitte starte",
		"okay hey kubi",
	} {
		if !wakePhraseTranscriptMatches(transcript, aliases) {
			t.Fatalf("transcript %q did not match aliases %#v", transcript, aliases)
		}
	}
	if wakePhraseTranscriptMatches("hey computer", aliases) {
		t.Fatal("unrelated phrase matched hey_quby aliases")
	}
}

// TestSidecarParentContextIsLongLived guards the wiring invariant that the
// sidecar's exec.CommandContext is rooted in a Background-equivalent context
// so callers' short-lived contexts (HTTP request, settings save) cannot kill
// the long-lived sidecar. See launchWakewordSidecar comment for bug history.
func TestSidecarParentContextIsLongLived(t *testing.T) {
	ctx := sidecarParentContext()
	if ctx == nil {
		t.Fatal("sidecarParentContext returned nil")
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Fatal("sidecar parent ctx must not carry a deadline")
	}
	select {
	case <-ctx.Done():
		t.Fatal("sidecar parent ctx is already Done — must be long-lived")
	default:
	}
}

// TestLaunchWakewordSidecar_DecouplesFromShortLivedCallerCtx is the behavioral
// regression test for the v0.35.6 "Wake-word sidecar exited (code 1)" bug.
// Until the fix, launchWakewordSidecar derived its exec.CommandContext from
// the caller's parentCtx — which, for restartDesktopWakeword(r.Context(), ...)
// callers in routes_feature_auth_app.go and routes_settings.go, was an HTTP
// request ctx that cancelled the instant the handler returned. exec then
// killed the sidecar via Windows TerminateProcess(handle, 1).
//
// The fix decouples the sidecar exec ctx from any caller ctx. This test
// proves it by passing an already-cancelled caller ctx into launch and
// asserting the ctx actually handed to exec.CommandContext is NOT cancelled.
func TestLaunchWakewordSidecar_DecouplesFromShortLivedCallerCtx(t *testing.T) {
	var (
		captured    context.Context
		doneAtCall  bool
		hadDeadline bool
	)
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		captured = ctx
		select {
		case <-ctx.Done():
			doneAtCall = true
		default:
		}
		_, hadDeadline = ctx.Deadline()
		// Return a Cmd pointing at a non-existent binary so cmd.Start() fails
		// fast and launchWakewordSidecar returns its early-error path. We
		// asserted on the ctx snapshot above before that error path could run
		// cancel() and pollute the captured ctx.
		return exec.Command("non-existent-wakeword-stub-binary-zzz")
	}

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	cancelCaller()

	state := &appState{}
	cfg := &config.Config{}
	cfg.Wakeword.Threshold = 0.25
	cfg.Wakeword.MinConsecutiveFrames = 1
	cfg.Wakeword.CooldownMs = 1500
	resolved := resolvedWakewordAssets{
		SidecarPath:   "fake-sidecar",
		ModelDir:      "fake-models",
		KeywordsFile:  "fake-keywords.txt",
		DefaultMode:   "voice_agent",
		DisplayPhrase: "Hey Quby",
	}

	_, _ = launchWakewordSidecar(callerCtx, state, nil, cfg, resolved)

	if captured == nil {
		t.Fatal("execCommandContext was never invoked")
	}
	if doneAtCall {
		t.Fatal("sidecar exec ctx was already Done at exec.CommandContext call time — regression: caller ctx cancellation is propagating to the sidecar (this is the v0.35.6 'Wake-word sidecar exited code 1' bug)")
	}
	if hadDeadline {
		t.Fatal("sidecar exec ctx unexpectedly carries a deadline — must be long-lived Background")
	}
}
