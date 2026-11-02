//go:build cgo

// speechkit-tts-sherpa serves on-device text-to-speech with the Piper VITS
// voices bundled in the installer (German Thorsten, English Lessac) through
// sherpa-onnx. The desktop's local TTS provider spawns it from
// sherpa-runtime/ next to the app and speaks HTTP to it, so the desktop
// process does not link a second ONNX runtime.
//
//	speechkit-tts-sherpa --voices <models/tts dir> [--host 127.0.0.1] [--port 8080] [--threads N]
//
// Both voices load before the port is bound, so a listener that answers
// /health is ready to speak. POST /v1/audio/speech with {"text","locale"}
// returns a WAV body.
//
// Build: requires cgo (the sherpa-onnx native library). It ships in the
// desktop bundle's sherpa-runtime/ directory next to the sherpa-onnx DLLs it
// shares with the wake-word and speech-to-text sidecars.
//
// Exit codes: 0 after SIGINT/SIGTERM, 2 on a runtime error, 3 on a bad
// configuration (voices missing or unusable).
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
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/tts/sherpa"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/tts/sherpa/engine"
)

// version is set at link time (-X main.version) by the desktop bundle.
var version = "dev"

type config struct {
	voices  string
	host    string
	port    int
	threads int
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
		logger.Error("speechkit-tts-sherpa exited with error", "error", err)
		var bad badConfigError
		if errors.As(err, &bad) {
			os.Exit(3)
		}
		os.Exit(2)
	}
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("speechkit-tts-sherpa", flag.ContinueOnError)
	var (
		cfg         config
		showVersion bool
	)
	fs.StringVar(&cfg.voices, "voices", "", "directory holding the bundled Piper VITS voices and espeak-ng-data (models/tts)")
	fs.StringVar(&cfg.host, "host", "127.0.0.1", "address to listen on")
	fs.IntVar(&cfg.port, "port", 0, "port to listen on; 0 picks an ephemeral port and prints it")
	fs.IntVar(&cfg.threads, "threads", defaultThreads(), "CPU threads per voice")
	fs.BoolVar(&showVersion, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if showVersion {
		fmt.Println("speechkit-tts-sherpa", version)
		os.Exit(0)
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if cfg.voices == "" {
		return config{}, errors.New("--voices is required")
	}
	if cfg.port < 0 || cfg.port > 65535 {
		return config{}, fmt.Errorf("--port %d is out of range", cfg.port)
	}
	return cfg, nil
}

func run(ctx context.Context, cfg config, logger *slog.Logger) error {
	if _, err := sherpa.ResolveVoices(cfg.voices); err != nil {
		return badConfigError{err}
	}
	loadStart := time.Now()
	speaker, err := engine.New(cfg.voices, cfg.threads)
	if err != nil {
		return badConfigError{err}
	}
	defer speaker.Close()
	logger.Info("voices loaded", "voices", cfg.voices, "threads", cfg.threads, "load_ms", time.Since(loadStart).Milliseconds())

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port)))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{
		Handler:           sherpa.NewHandler(speaker, sherpa.ServerOptions{Logger: logger}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// A long utterance on CPU can take several seconds; the write bound
		// only covers sending the WAV, which is small.
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}
	logger.Info("listening", "address", listener.Addr().String(), "version", version)

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

func defaultThreads() int {
	return min(max(runtime.NumCPU(), 1), 4)
}
