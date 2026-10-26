package local

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa"
)

// runtimeEngine is the local STT server a model needs. Both speak
// whisper-server's HTTP contract, so lifecycle, identity proof, the keyed
// proxy and readiness are shared; only the binary, its arguments and the
// request fields differ.
type runtimeEngine int

const (
	// engineWhisper is whisper.cpp's whisper-server with a ggml-*.bin file.
	engineWhisper runtimeEngine = iota
	// engineSherpa is speechkit-stt-sherpa with a model directory: a
	// sherpa-onnx Parakeet export or a Phonon five-value model, which the
	// sidecar runs with its own engine.
	engineSherpa
)

// ParakeetProfileID is the catalog profile of the Parakeet models the
// provider runs through speechkit-stt-sherpa.
const ParakeetProfileID = "stt.local.parakeet"

// sherpaRuntimeDir is the bundle subdirectory the sidecar ships in, next to
// the sherpa-onnx libraries it shares with the wake-word sidecar.
const sherpaRuntimeDir = "sherpa-runtime"

// engineForModel picks the engine from the model path: a directory is a
// sherpa-onnx model, anything else a whisper.cpp ggml file. A path that does
// not exist counts as a Parakeet directory unless it names a ".bin" file, so
// a moved or deleted Parakeet model is reported as such and not as a
// malformed whisper file.
func engineForModel(modelPath string) runtimeEngine {
	info, err := os.Stat(modelPath)
	switch {
	case err == nil && info.IsDir():
		return engineSherpa
	case err != nil && strings.TrimSpace(modelPath) != "" && !strings.EqualFold(filepath.Ext(modelPath), ".bin"):
		return engineSherpa
	default:
		return engineWhisper
	}
}

// serverName names the engine's server in errors and logs.
func (e runtimeEngine) serverName() string {
	if e == engineSherpa {
		return "speechkit-stt-sherpa"
	}
	return "whisper-server"
}

// profileID is the catalog profile requests of the engine resolve against.
func (e runtimeEngine) profileID() string {
	if e == engineSherpa {
		return ParakeetProfileID
	}
	return "stt.local.whispercpp"
}

// IsSherpaModel reports whether modelPath is a Parakeet model directory the
// provider runs through speechkit-stt-sherpa rather than whisper-server.
func IsSherpaModel(modelPath string) bool {
	return engineForModel(modelPath) == engineSherpa
}

// FindSherpaBinary returns the speechkit-stt-sherpa sidecar the provider
// would start for a Parakeet model, for callers that report runtime readiness
// without starting it.
func FindSherpaBinary() (string, error) {
	return findSherpaBinary()
}

// findBinary returns the server executable of engine.
func findBinary(engine runtimeEngine) (string, error) {
	if engine == engineSherpa {
		return findSherpaBinary()
	}
	return findWhisperBinary()
}

// findSherpaBinary looks for speechkit-stt-sherpa next to the executable, in
// its sherpa-runtime subdirectory, and in the platform's managed install
// locations, in that order. PATH is consulted only under the same developer
// opt-in as whisper-server (SPEECHKIT_ALLOW_WHISPER_PATH=1).
func findSherpaBinary() (string, error) {
	name := "speechkit-stt-sherpa"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var dirs []string
	if exe, _ := os.Executable(); exe != "" {
		dirs = append(dirs, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), sherpaRuntimeDir))
	}
	for _, dir := range platformWhisperSearchDirs() {
		dirs = append(dirs, dir, filepath.Join(dir, sherpaRuntimeDir))
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() { // #nosec G703 -- bundle and managed install dirs, not user input.
			return path, nil
		}
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPEECHKIT_ALLOW_WHISPER_PATH")), "1") {
		if path, err := exec.LookPath(name); err == nil {
			logutil.Resolve(nil).Warn("using speechkit-stt-sherpa from PATH due to SPEECHKIT_ALLOW_WHISPER_PATH=1", "path", path)
			return path, nil
		}
	}
	return "", fmt.Errorf("speechkit-stt-sherpa binary not found in bundle or managed install location")
}

// resolveSidecarModelDir validates a model directory speechkit-stt-sherpa
// runs and returns it: a Phonon five-value model (Fermion's container) or a
// sherpa-onnx export.
func resolveSidecarModelDir(modelPath string) (string, error) {
	if phonon.IsModelDir(modelPath) {
		files, err := phonon.ResolveModelDir(modelPath)
		return files.Dir, err
	}
	files, err := sherpa.ResolveModelDir(modelPath)
	return files.Dir, err
}

// sidecarModelLanguages is the language list of the model directory, which
// the sidecar enforces.
func sidecarModelLanguages(modelPath string) []string {
	if phonon.IsModelDir(modelPath) {
		return phonon.ModelLanguages(modelPath)
	}
	return sherpa.ModelLanguages(modelPath)
}

// validateModel checks the model of engine before a start.
func validateModel(engine runtimeEngine, modelPath string) error {
	if engine == engineSherpa {
		_, err := resolveSidecarModelDir(modelPath)
		return err
	}
	if err := ValidateModelPath(modelPath); err != nil {
		return fmt.Errorf("validate whisper model path: %w", err)
	}
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("model not found: %s", modelPath)
	}
	if err := verifyReadableModelFile(modelPath); err != nil {
		return fmt.Errorf("model not readable: %s: %w", modelPath, err)
	}
	return nil
}

// serverArgs returns the command line of engine's server listening on port,
// and the working directory the child runs in ("" means the binary's own).
func serverArgs(engine runtimeEngine, modelPath string, port, threads int, identity *serverIdentity, gpu string) (args []string, dir string) {
	if engine == engineSherpa {
		// The sidecar is a Go program: it receives argv as UTF-16 on Windows
		// and converts paths for the native runtime itself, so a non-ASCII
		// model or identity path needs no short-name workaround here.
		args = []string{
			"--model", modelPath,
			"--host", "127.0.0.1",
			"--port", fmt.Sprint(port),
			"--threads", fmt.Sprint(threads),
			"--inference-path", "/v1/audio/transcriptions",
			// English-only models refuse a pinned non-English language.
			"--languages", strings.Join(sidecarModelLanguages(modelPath), ","),
		}
		if identity != nil {
			args = append(args, "--public", identity.dir)
		}
		return args, ""
	}
	modelArg, workDir := whisperModelArgument(modelPath)
	args = []string{
		"--model", modelArg,
		"--host", "127.0.0.1",
		"--port", fmt.Sprint(port),
		"--threads", fmt.Sprint(threads),
		"--inference-path", "/v1/audio/transcriptions",
	}
	if identity != nil {
		args = append(args, "--public", identity.publicArg)
	}
	// whisper.cpp uses GPU by default; only pass --no-gpu when explicitly disabled.
	// "auto" and "" mean let whisper.cpp decide (default behavior).
	if gpu == "cpu" {
		args = append(args, "--no-gpu")
	}
	return args, workDir
}
