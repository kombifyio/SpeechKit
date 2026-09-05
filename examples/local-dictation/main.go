// Local dictation through the public SDK, using a WAV file or explicit microphone capture.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/dictation"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/local"
)

type options struct {
	model     string
	wav       string
	language  string
	gpu       string
	recordFor time.Duration
	timeout   time.Duration
}

type recorder interface {
	speechkit.AudioRecorder
	Close() error
}

type managedProvider interface {
	stt.STTProvider
	StartServer(context.Context) error
	StopServer()
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output, progress io.Writer) error {
	opts, err := parseOptions(args, progress)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	var pcm []byte
	if opts.wav != "" {
		pcm, err = readWAV(opts.wav)
		if err != nil {
			return err
		}
	}

	// Select a separate loopback port; never attach to or stop an existing server.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("select a local whisper port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return fmt.Errorf("release the local whisper port: %w", err)
	}
	provider := local.New(port, opts.model, opts.gpu)
	installation := provider.VerifyInstallation()
	if !installation.BinaryFound || !installation.ModelFound {
		return fmt.Errorf("local whisper is not installed: %s; see examples/local-dictation/README.md",
			strings.Join(installation.Problems, "; "))
	}

	openRecorder := func() (recorder, error) {
		if opts.wav != "" {
			return &wavRecorder{pcm: pcm}, nil
		}
		session, err := capture.Open(capture.Config{InputSource: capture.InputSourceMicrophone})
		if err != nil {
			return nil, fmt.Errorf("open microphone (native capture needs Windows and cgo; use -wav otherwise): %w", err)
		}
		return session, nil
	}
	result, err := dictate(ctx, opts, provider, openRecorder, progress)
	if strings.TrimSpace(result.Transcript.Text) != "" {
		_, outputErr := fmt.Fprintln(output, result.Transcript.Text)
		return errors.Join(err, outputErr)
	}
	if err != nil {
		return err
	}
	return errors.New("the local model returned no text; check the input and language, then try again")
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("local-dictation", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.model, "model", "", "path to an installed ggml-*.bin whisper model (required)")
	flags.StringVar(&opts.wav, "wav", "", "16 kHz mono PCM16 WAV file; does not open the microphone")
	flags.DurationVar(&opts.recordFor, "record-for", 0, "explicit microphone recording duration, e.g. 5s (at most 1m)")
	flags.StringVar(&opts.language, "language", "auto", "language hint, e.g. de, en or auto")
	flags.StringVar(&opts.gpu, "gpu", "auto", "auto or cpu")
	flags.DurationVar(&opts.timeout, "timeout", 5*time.Minute, "transcription timeout, after model startup and capture")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 || strings.TrimSpace(opts.model) == "" {
		return opts, errors.New("provide -model and exactly one input: -wav FILE or -record-for 5s; use -h for help")
	}
	if opts.recordFor < 0 || opts.recordFor > time.Minute || (opts.wav == "") == (opts.recordFor == 0) {
		return opts, errors.New("choose exactly one input: -wav FILE or -record-for DURATION between 0 and 1m")
	}
	if opts.timeout <= 0 || (opts.gpu != "auto" && opts.gpu != "cpu") {
		return opts, errors.New("-timeout must be positive and -gpu must be auto or cpu")
	}
	model, err := filepath.Abs(opts.model)
	if err != nil {
		return opts, fmt.Errorf("resolve model path: %w", err)
	}
	if err := local.ValidateModelPath(model); err != nil {
		return opts, err
	}
	opts.model = model
	return opts, nil
}

func readWAV(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open WAV input: %w", err)
	}
	const maxBytes = 32 << 20
	wav, readErr := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, fmt.Errorf("read WAV input: %w", err)
	}
	if len(wav) > maxBytes {
		return nil, errors.New("WAV input exceeds this starter's 32 MiB limit")
	}
	pcm, rate, channels, ok := stt.PCM16FromWAV(wav)
	if !ok || rate != audio.SampleRate || channels != audio.Channels || len(pcm)%audio.BytesPerSample != 0 {
		return nil, errors.New("input must be a complete 16 kHz mono PCM16 WAV; convert it before running this starter")
	}
	if len(pcm) < speechkit.DefaultMinPCMBytes {
		return nil, fmt.Errorf("WAV input: %w", dictation.ErrAudioTooShort)
	}
	return pcm, nil
}

func dictate(ctx context.Context, opts options, provider managedProvider, open func() (recorder, error), progress io.Writer) (result speechkit.DictationRun, err error) {
	defer provider.StopServer()
	fmt.Fprintln(progress, "Starting local whisper; loading and warming the model can take several minutes.")
	// The process uses the host lifetime, not the shorter transcription deadline.
	if err := provider.StartServer(ctx); err != nil {
		return result, fmt.Errorf("start local whisper: %w", err)
	}
	input, err := open()
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := input.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close recording input: %w", closeErr))
		}
	}()
	service, err := dictation.NewService(dictation.Options{
		Recorder:    input,
		Transcriber: stt.AsTranscriber(provider),
		Language:    opts.language,
		Policy: speechkit.RuntimePolicy{
			EnabledModes:  []speechkit.Mode{speechkit.ModeDictation},
			FixedProfiles: map[speechkit.Mode]string{speechkit.ModeDictation: "stt.local.whispercpp"},
		},
	})
	if err != nil {
		return result, err
	}
	if err := service.Start(ctx); err != nil {
		return result, fmt.Errorf("start recording input: %w", err)
	}
	if opts.recordFor > 0 {
		fmt.Fprintf(progress, "Microphone active for %s; speak now. Ctrl+C discards the recording.\n", opts.recordFor)
		timer := time.NewTimer(opts.recordFor)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-timer.C:
		}
	}
	fmt.Fprintln(progress, "Transcribing locally; no cloud provider or history storage is configured.")
	transcribeCtx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()
	return service.Stop(transcribeCtx)
}

type wavRecorder struct {
	pcm []byte
}

func (*wavRecorder) Start() error               { return nil }
func (r *wavRecorder) Stop() ([]byte, error)    { return r.pcm, nil }
func (*wavRecorder) SetPCMHandler(func([]byte)) {}
func (r *wavRecorder) Close() error             { r.pcm = nil; return nil }
