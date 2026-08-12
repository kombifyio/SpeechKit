//go:build linux

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	deviceagentserver "github.com/kombifyio/SpeechKit/internal/server/deviceagent"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

const (
	boxMediaProviderProbeTimeout = 8 * time.Second
	boxMediaHealthProbeInterval  = 5 * time.Second
	// boxMediaMaxProbeFailures is how many CONSECUTIVE probe failures are
	// tolerated before the feature is declared dead.
	//
	// It exists because a probe failure is not evidence of a dead child. The
	// probe runs every 5s with an 8s budget against the same whisper process
	// that may be serving a Box turn for up to 60s, so model page-in, a loaded
	// CPU, or whisper occupying its worker threads on a long clip all look
	// exactly like a corpse. Escalating on the first miss meant one slow
	// second took down dictation, assist, voiceagent and the device-agent
	// bridge along with it.
	boxMediaMaxProbeFailures    = 3
	boxMediaMaxCertificateBytes = 1 << 20
	boxMediaHealthComponent     = "api.box_media"
)

type boxMediaLocalRuntime interface {
	stt.STTProvider
	StartServer(context.Context) error
	StopServer()
	IsReady() bool
}

type boxMediaSupervisedLocalRuntime interface {
	RuntimeDone() <-chan struct{}
	RuntimeError() error
}

type boxMediaListenerRuntime interface {
	Errors() <-chan error
	Addr() net.Addr
	Shutdown(context.Context) error
}

type boxMediaServerRuntime struct {
	listener         boxMediaListenerRuntime
	localSTT         boxMediaLocalRuntime
	localSupervisor  boxMediaSupervisedLocalRuntime
	ownsLocalSTT     bool
	health           *HealthRegistry
	blockSTT         bool
	healthInterval   time.Duration
	errors           chan error
	supervisorCancel context.CancelFunc
	supervisorDone   chan struct{}
	once             sync.Once
	shutdownErr      error
}

func (r *boxMediaServerRuntime) Errors() <-chan error {
	if r == nil {
		return nil
	}
	if r.errors != nil {
		return r.errors
	}
	if r.listener == nil {
		return nil
	}
	return r.listener.Errors()
}

func (r *boxMediaServerRuntime) Addr() net.Addr {
	if r == nil || r.listener == nil {
		return nil
	}
	return r.listener.Addr()
}

func (r *boxMediaServerRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		if r.supervisorCancel != nil {
			r.supervisorCancel()
		}
		if r.supervisorDone != nil {
			<-r.supervisorDone
		}
		if r.listener != nil {
			r.shutdownErr = r.listener.Shutdown(ctx)
		}
		if r.ownsLocalSTT && r.localSTT != nil {
			r.localSTT.StopServer()
		}
	})
	return r.shutdownErr
}

func (r *boxMediaServerRuntime) startSupervisor(parent context.Context) {
	if r == nil || r.listener == nil || r.localSTT == nil || r.localSupervisor == nil {
		return
	}
	// #nosec G118 -- cancel is stored on the runtime and invoked during
	// shutdown (see stop, r.supervisorCancel()); gosec cannot follow a cancel
	// func that outlives its function scope by design.
	ctx, cancel := context.WithCancel(parent)
	r.supervisorCancel = cancel
	r.supervisorDone = make(chan struct{})
	r.errors = make(chan error, 1)
	go func() {
		defer close(r.supervisorDone)
		defer close(r.errors)
		listenerErrors := r.listener.Errors()
		localDone := r.localSupervisor.RuntimeDone()
		interval := r.healthInterval
		if interval <= 0 {
			interval = boxMediaHealthProbeInterval
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		probeFailures := 0

		fail := func(detail string, err error, localFailed bool) {
			if r.health != nil {
				r.health.SetReadyWithOptions(boxMediaHealthComponent, StatusUnavailable, detail, ComponentOptions{
					Blocking: true,
					Kind:     "feature",
				})
				if localFailed {
					r.health.SetReadyWithOptions("stt.local", StatusUnavailable, detail, sttProviderOptions(namedProvider{
						name: "stt.local", provider: r.localSTT,
					}))
					if r.blockSTT {
						updateSTTAggregate(&App{Health: r.health})
					}
				}
			}
			r.errors <- err
		}

		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-listenerErrors:
				if ctx.Err() != nil {
					return
				}
				if !ok {
					err = errors.New("box media TLS listener stopped unexpectedly")
				} else if err == nil {
					err = errors.New("box media TLS listener failed without an error")
				}
				fail("dedicated TLS listener failed", err, false)
				return
			case <-localDone:
				if ctx.Err() != nil {
					return
				}
				err := r.localSupervisor.RuntimeError()
				if err == nil {
					err = errors.New("box media host-local STT runtime stopped unexpectedly")
				}
				fail("host-local STT runtime stopped", err, true)
				return
			case <-ticker.C:
				// A single miss is tolerated and reported as degraded; only a
				// run of them is treated as death. The listener and
				// runtime-stopped branches above stay immediately fatal —
				// those are proof, not inference.
				probeErr := error(nil)
				detail := ""
				if !r.localSTT.IsReady() {
					probeErr = errors.New("box media host-local STT readiness was lost")
					detail = "host-local STT runtime is not ready"
				} else {
					probeCtx, cancelProbe := context.WithTimeout(ctx, boxMediaProviderProbeTimeout)
					err := r.localSTT.Health(probeCtx)
					cancelProbe()
					if err != nil {
						probeErr = fmt.Errorf("probe Box media host-local STT: %w", err)
						detail = "host-local STT health probe failed"
					}
				}

				if probeErr == nil {
					if probeFailures > 0 {
						// Recovered. Without this the component would stay
						// degraded for the life of the process even though the
						// child is answering again.
						probeFailures = 0
						if r.health != nil {
							r.health.SetReadyWithOptions(boxMediaHealthComponent, StatusOK,
								"host-local STT recovered", ComponentOptions{Blocking: true, Kind: "feature"})
						}
					}
					continue
				}

				probeFailures++
				if probeFailures < boxMediaMaxProbeFailures {
					if r.health != nil {
						r.health.SetReadyWithOptions(boxMediaHealthComponent, StatusDegraded,
							fmt.Sprintf("%s (%d/%d)", detail, probeFailures, boxMediaMaxProbeFailures),
							ComponentOptions{Blocking: true, Kind: "feature"})
					}
					continue
				}
				fail(fmt.Sprintf("%s (%d consecutive)", detail, probeFailures), probeErr, true)
				return
			}
		}
	}()
}

// wireBoxMediaListener starts the dedicated Box listener only after its exact
// local STT and local-only TTS dependencies are proven ready. It never mounts
// the handler on app.Mux and never routes Box audio through the general STT
// router, cloud providers, Gateway, federation, or MCP.
func wireBoxMediaListener(ctx context.Context, cfg *config.Config, app *App) (*boxMediaServerRuntime, error) {
	if cfg == nil || app == nil || app.Health == nil {
		return nil, errors.New("box media listener requires initialized server state")
	}
	media := cfg.Server.DeviceAgent.BoxMedia
	if !media.Enabled {
		app.Health.SetReadyWithOptions(boxMediaHealthComponent, StatusDisabled, "configured off", ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		return nil, nil
	}
	if err := config.ValidateServerProductionAuth(cfg); err != nil {
		return nil, fmt.Errorf("validate Box media server configuration: %w", err)
	}
	if app.DeviceAgentBridge == nil || !app.DeviceAgentBridgeMounted {
		return nil, errors.New("box media listener requires the initialized local device-agent bridge")
	}
	if app.STTRouter == nil {
		return nil, errors.New("box media listener requires the host-local STT router")
	}
	local, ok := app.STTRouter.Local().(boxMediaLocalRuntime)
	if !ok || local == nil || !strings.EqualFold(strings.TrimSpace(local.Name()), "local") {
		return nil, errors.New("box media listener requires the concrete host-local STT runtime")
	}
	localSupervisor, ok := local.(boxMediaSupervisedLocalRuntime)
	if !ok || localSupervisor == nil {
		return nil, errors.New("box media listener requires a supervised host-local STT runtime")
	}

	// The shared STT router may already own a ready local runtime for another
	// server mode. Box shutdown must stop only a startup attempt made here.
	ownsLocalSTT := false
	cleanupOwnedLocal := true
	defer func() {
		if cleanupOwnedLocal && ownsLocalSTT {
			local.StopServer()
		}
	}()
	if !local.IsReady() {
		// LocalProvider owns a whisper-server subprocess through the supplied
		// context. Use the process-lifetime server context: cancelling a narrow
		// startup context after readiness would terminate the ready subprocess.
		ownsLocalSTT = true
		if err := local.StartServer(ctx); err != nil {
			return nil, fmt.Errorf("start Box media host-local STT: %w", err)
		}
	}
	if !local.IsReady() {
		return nil, errors.New("box media host-local STT returned from startup without readiness")
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, boxMediaProviderProbeTimeout)
	localHealthErr := local.Health(probeCtx)
	cancelProbe()
	if localHealthErr != nil {
		return nil, fmt.Errorf("probe Box media host-local STT: %w", localHealthErr)
	}
	localHealthProvider := namedProvider{name: "stt.local", provider: local}
	app.Health.SetReadyWithOptions("stt.local", StatusOK, "ready", sttProviderOptions(localHealthProvider))
	if app.ModeEnabled(ModeDictation) {
		updateSTTAggregate(app)
	}
	localDone := localSupervisor.RuntimeDone()
	if localDone == nil {
		return nil, errors.New("box media host-local STT runtime did not expose process supervision")
	}
	select {
	case <-localDone:
		if runtimeErr := localSupervisor.RuntimeError(); runtimeErr != nil {
			return nil, fmt.Errorf("box media host-local STT exited before listener startup: %w", runtimeErr)
		}
		return nil, errors.New("box media host-local STT stopped before listener startup")
	default:
	}

	if app.TTSRouter == nil || !app.TTSEnabled {
		return nil, errors.New("box media listener requires the local-only TTS router")
	}
	ttsProbeCtx, cancelTTSProbe := context.WithTimeout(ctx, boxMediaProviderProbeTimeout)
	ttsHealth := app.TTSRouter.ReadyHealthCheck(ttsProbeCtx)
	cancelTTSProbe()
	if len(ttsHealth) == 0 {
		return nil, errors.New("box media listener requires at least one eligible local TTS provider")
	}
	ttsReady := false
	for _, healthErr := range ttsHealth {
		if healthErr == nil {
			ttsReady = true
			break
		}
	}
	if !ttsReady {
		return nil, errors.New("box media listener requires one ready local TTS provider")
	}

	mediaToken := strings.TrimSpace(config.ResolveSecret(media.TokenEnv))
	handler, err := deviceagentserver.NewBoxMediaHandler(deviceagentserver.BoxMediaHandlerOptions{
		Bridge:            app.DeviceAgentBridge,
		LocalSTT:          &boxMediaLocalTranscriber{runtime: local},
		MediaPairingToken: mediaToken,
		Rule: deviceagentserver.BoxMediaRuleOptions{
			DeviceID: media.DeviceID, PairingID: media.PairingID, RoomID: media.RoomID,
			Transcript: media.Transcript, CommandID: media.CommandID, Locale: media.Locale,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build Box media handler: %w", err)
	}
	tlsServer, err := deviceagentserver.NewBoxMediaTLSServer(deviceagentserver.BoxMediaTLSConfig{
		ListenAddr: media.ListenAddr, CertificateFile: media.CertificateFile, PrivateKeyFile: media.PrivateKeyFile,
	}, handler)
	if err != nil {
		return nil, fmt.Errorf("build Box media TLS listener: %w", err)
	}
	if err := verifyBoxMediaPinnedCA(media, tlsServer.CertificateChainDER()); err != nil {
		return nil, err
	}
	listener, err := tlsServer.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("start Box media TLS listener: %w", err)
	}
	runtime := &boxMediaServerRuntime{
		listener: listener, localSTT: local, localSupervisor: localSupervisor,
		ownsLocalSTT: ownsLocalSTT, health: app.Health, blockSTT: app.ModeEnabled(ModeDictation),
	}
	app.BoxMediaRuntime = runtime
	cleanupOwnedLocal = false
	app.Health.SetReadyWithOptions(boxMediaHealthComponent, StatusOK, "dedicated local TLS listener ready", ComponentOptions{
		Blocking: true,
		Kind:     "feature",
	})
	runtime.startSupervisor(ctx)
	slog.Info("local Box media listener enabled",
		"path", deviceagentserver.BoxMediaTurnPath,
		"addr", runtime.Addr().String(),
		"device_id", strings.TrimSpace(media.DeviceID),
		"pairing_id", strings.TrimSpace(media.PairingID),
		"command_id", strings.TrimSpace(media.CommandID),
		"pinned_ca_sha256", strings.TrimSpace(media.PinnedCASHA256))
	return runtime, nil
}

type boxMediaLocalTranscriber struct {
	runtime boxMediaLocalRuntime
}

func (t *boxMediaLocalTranscriber) LocalOnly() bool {
	return t != nil && t.runtime != nil && t.runtime.IsReady() && strings.EqualFold(strings.TrimSpace(t.runtime.Name()), "local")
}

func (t *boxMediaLocalTranscriber) TranscribeLocal(ctx context.Context, wav []byte, locale string) (*deviceagentserver.BoxMediaTranscript, error) {
	if !t.LocalOnly() {
		return nil, errors.New("box media host-local STT is not ready")
	}
	result, err := t.runtime.Transcribe(ctx, wav, stt.TranscribeOpts{Language: strings.TrimSpace(locale)})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("box media host-local STT returned no result")
	}
	provider := strings.TrimSpace(result.Provider)
	if provider == "" {
		provider = strings.TrimSpace(t.runtime.Name())
	}
	if !strings.EqualFold(provider, strings.TrimSpace(t.runtime.Name())) {
		return nil, errors.New("box media host-local STT returned a foreign provider identity")
	}
	return &deviceagentserver.BoxMediaTranscript{Text: result.Text, Provider: provider}, nil
}

func verifyBoxMediaPinnedCA(media config.ServerDeviceAgentBoxMediaConfig, serverCertificateDER [][]byte) error {
	rootCertificates, err := readBoxMediaCertificates(media.PinnedCAFile)
	if err != nil {
		return fmt.Errorf("verify Box-pinned local CA: %w", err)
	}
	if len(rootCertificates) != 1 {
		return errors.New("verify Box-pinned local CA: pinned_ca_file must contain exactly one CA certificate")
	}
	root := rootCertificates[0]
	now := time.Now()
	if !root.IsCA || !root.BasicConstraintsValid || root.KeyUsage&x509.KeyUsageCertSign == 0 {
		return errors.New("verify Box-pinned local CA: certificate is not an explicit signing CA")
	}
	if now.Before(root.NotBefore) || !now.Before(root.NotAfter) {
		return errors.New("verify Box-pinned local CA: certificate is not currently valid")
	}
	digest := sha256.Sum256(root.Raw)
	actualDigest := hex.EncodeToString(digest[:])
	expectedDigest := strings.TrimSpace(media.PinnedCASHA256)
	if len(actualDigest) != len(expectedDigest) || subtle.ConstantTimeCompare([]byte(actualDigest), []byte(expectedDigest)) != 1 {
		return errors.New("verify Box-pinned local CA: pinned_ca_sha256 does not match the CA DER certificate")
	}

	if len(serverCertificateDER) == 0 {
		return errors.New("verify Box media certificate chain: loaded TLS certificate chain is empty")
	}
	serverCertificates := make([]*x509.Certificate, 0, len(serverCertificateDER))
	for _, certificateDER := range serverCertificateDER {
		certificate, parseErr := x509.ParseCertificate(certificateDER)
		if parseErr != nil {
			return fmt.Errorf("verify Box media certificate chain: parse loaded TLS certificate: %w", parseErr)
		}
		serverCertificates = append(serverCertificates, certificate)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	intermediates := x509.NewCertPool()
	for _, certificate := range serverCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(media.ListenAddr))
	if err != nil {
		return fmt.Errorf("verify Box media certificate chain: parse listen address: %w", err)
	}
	if _, err := serverCertificates[0].Verify(x509.VerifyOptions{
		DNSName: strings.TrimSpace(host), Roots: roots, Intermediates: intermediates,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, CurrentTime: now,
	}); err != nil {
		return fmt.Errorf("verify Box media certificate chain against pinned CA: %w", err)
	}
	return nil
}

func readBoxMediaCertificates(path string) ([]*x509.Certificate, error) {
	file, err := os.Open(strings.TrimSpace(path)) // #nosec G304 -- production validation requires a canonical absolute operator-owned certificate path.
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // read-only certificate close errors are not actionable
	raw, err := io.ReadAll(io.LimitReader(file, boxMediaMaxCertificateBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > boxMediaMaxCertificateBytes {
		return nil, errors.New("certificate PEM must be non-empty and at most 1 MiB")
	}
	certificates := make([]*x509.Certificate, 0, 2)
	for len(bytes.TrimSpace(raw)) > 0 {
		block, remaining := pem.Decode(raw)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, errors.New("certificate file must contain only PEM CERTIFICATE blocks")
		}
		certificate, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return nil, parseErr
		}
		certificates = append(certificates, certificate)
		raw = remaining
	}
	return certificates, nil
}
