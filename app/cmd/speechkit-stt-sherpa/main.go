//go:build cgo

// speechkit-stt-sherpa serves on-device speech-to-text with a sherpa-onnx
// nemo_transducer model (NVIDIA Parakeet TDT 0.6B v3, Moondream Parakeet
// Ultra) over the HTTP contract of whisper.cpp's whisper-server, so every
// SpeechKit host that drives whisper-server drives it unchanged:
//
//   - the desktop's built-in local provider (pkg/speechkit/x/stt/local)
//     spawns it from sherpa-runtime/ next to the app when the selected model
//     is a sherpa model directory;
//   - speechkit-server reaches it as a self-hosted [vps] endpoint;
//   - the Box media runtime and the Box satellite use it through the same
//     local provider.
//
// Command line (the whisper-server subset SpeechKit uses, plus model
// options):
//
//	speechkit-stt-sherpa --model <dir> [--host 127.0.0.1] [--port 8080]
//	    [--threads N] [--inference-path /inference] [--public <dir>]
//	    [--languages bg,cs,...|any] [--max-in-flight 8] [--no-gpu]
//
// The model loads before the port is bound, as in whisper-server, so a
// listener that answers /health is ready to transcribe. With a Phonon model
// it also serves the stream routes (/stream, see sherpa.StreamTranscriber),
// which the local provider uses for live dictation.
//
// Build: requires cgo (the sherpa-onnx native library). It ships in the
// desktop bundle's sherpa-runtime/ directory next to the sherpa-onnx DLLs it
// shares with the wake-word sidecar.
//
// Exit codes: 0 after SIGINT/SIGTERM, 2 on a runtime error, 3 on a bad
// configuration (model missing or unusable).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon"
	phononengine "github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon/engine"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa/engine"
)

// version is set at link time (-X main.version) by the desktop bundle and
// the server image.
var version = "dev"

type config struct {
	model         string
	host          string
	port          int
	threads       int
	inferencePath string
	publicDir     string
	languages     []string
	maxInFlight   int
}

type badConfigError struct{ error }

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("speechkit-stt-sherpa exited with error", "error", err)
		var bad badConfigError
		if errors.As(err, &bad) {
			os.Exit(3)
		}
		os.Exit(2)
	}
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("speechkit-stt-sherpa", flag.ContinueOnError)
	var (
		cfg         config
		languages   string
		showVersion bool
	)
	fs.StringVar(&cfg.model, "model", "", "model directory: a sherpa-onnx nemo_transducer export (encoder, decoder, joiner, tokens.txt) or a Phonon five-value model (model.fermion, config.json, packed_manifest.json)")
	fs.StringVar(&cfg.model, "m", "", "shorthand for --model")
	fs.StringVar(&cfg.host, "host", "127.0.0.1", "address to listen on")
	fs.IntVar(&cfg.port, "port", 8080, "port to listen on")
	fs.IntVar(&cfg.threads, "threads", defaultThreads(), "CPU threads for inference")
	fs.IntVar(&cfg.threads, "t", defaultThreads(), "shorthand for --threads")
	fs.StringVar(&cfg.inferencePath, "inference-path", "/inference", "transcription route")
	fs.StringVar(&cfg.publicDir, "public", "", "directory whose files are served at the root")
	fs.StringVar(&languages, "languages", "",
		`comma-separated ISO 639-1 codes the model transcribes; a request pinning another language is refused ("any" accepts all; empty uses the model's own list, English for the Bark 3.0 and Phonon-2.5 English directories and Parakeet's 25 languages otherwise)`)
	fs.IntVar(&cfg.maxInFlight, "max-in-flight", 8, "transcription requests held at once; more get 503")
	fs.Bool("no-gpu", false, "accepted for whisper-server compatibility; inference always runs on the CPU")
	fs.BoolVar(&showVersion, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if showVersion {
		fmt.Println("speechkit-stt-sherpa", version)
		os.Exit(0)
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if cfg.model == "" {
		return config{}, errors.New("--model is required")
	}
	if cfg.port < 0 || cfg.port > 65535 {
		return config{}, fmt.Errorf("--port %d is out of range", cfg.port)
	}
	if strings.TrimSpace(languages) == "" {
		languages = strings.Join(modelLanguages(cfg.model), ",")
	}
	if !strings.EqualFold(strings.TrimSpace(languages), "any") {
		for _, code := range strings.Split(languages, ",") {
			if code = sherpa.NormalizeLanguage(code); code != "" {
				cfg.languages = append(cfg.languages, code)
			}
		}
	}
	return cfg, nil
}

func run(ctx context.Context, cfg config, logger *slog.Logger) error {
	five := phonon.IsModelDir(cfg.model)
	if five {
		if _, err := phonon.ResolveModelDir(cfg.model); err != nil {
			return badConfigError{err}
		}
	} else if _, err := sherpa.ResolveModelDir(cfg.model); err != nil {
		return badConfigError{err}
	}
	if cfg.publicDir != "" {
		if info, err := os.Stat(cfg.publicDir); err != nil || !info.IsDir() {
			return badConfigError{fmt.Errorf("--public %s is not a directory", cfg.publicDir)}
		}
	}

	loadStart := time.Now()
	recognizer, engineName, err := openRecognizer(cfg, five)
	if err != nil {
		return badConfigError{err}
	}
	defer recognizer.Close()
	modelName := filepath.Base(cfg.model)
	logger.Info("model loaded", "model", modelName, "engine", engineName, "threads", cfg.threads, "load_ms", time.Since(loadStart).Milliseconds())

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port)))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{
		Handler: sherpa.NewHandler(recognizer, sherpa.ServerOptions{
			InferencePath: cfg.inferencePath,
			PublicDir:     cfg.publicDir,
			Languages:     cfg.languages,
			ModelName:     modelName,
			MaxInFlight:   cfg.maxInFlight,
			Logger:        logger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		// Bounds a slow upload; decoding happens after the body is read, so
		// no write timeout cuts a long transcription short.
		ReadTimeout: 5 * time.Minute,
		IdleTimeout: 2 * time.Minute,
	}
	logger.Info("listening", "address", listener.Addr().String(), "inference_path", cfg.inferencePath, "version", version)

	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	select {
	case err := <-errs:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// recognizer is what the sidecar serves and closes; both engines implement it.
type recognizer interface {
	sherpa.Transcriber
	Close()
}

// openRecognizer loads the model with the engine its directory needs: the
// five-value engine for a Phonon model, sherpa-onnx otherwise.
func openRecognizer(cfg config, five bool) (recognizer, string, error) {
	if five {
		r, err := phononengine.NewRecognizer(phonon.Config{ModelDir: cfg.model, Threads: cfg.threads})
		if err != nil {
			return nil, "", err
		}
		return r, "phonon-2-c", nil
	}
	r, err := engine.NewRecognizer(sherpa.Config{ModelDir: cfg.model, Threads: cfg.threads})
	if err != nil {
		return nil, "", err
	}
	return r, "sherpa-onnx", nil
}

// modelLanguages is the language list of the model in dir.
func modelLanguages(dir string) []string {
	if phonon.IsModelDir(dir) {
		return phonon.ModelLanguages(dir)
	}
	return sherpa.ModelLanguages(dir)
}

func defaultThreads() int {
	return min(max(runtime.NumCPU(), 1), 8)
}
