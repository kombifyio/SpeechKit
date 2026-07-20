//go:build linux

package core

import (
	"context"
	"log/slog"
	"net"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/wyoming"
)

// startWyoming launches the Wyoming voice-protocol adapter on its own TCP
// listener (Wyoming is raw TCP, not HTTP, so it cannot share the mux). It is
// non-blocking: the accept loop runs in a goroutine and is torn down when ctx
// is cancelled (srv.Serve closes the listener on ctx.Done). No-op when disabled.
//
// wyomingNeedsSTT / wyomingNeedsTTS (below) let bootstrap build the shared STT
// and TTS routers even when no HTTP mode/feature would otherwise require them.
func startWyoming(ctx context.Context, cfg *config.Config, app *App) {
	const componentID = "wyoming"
	wcfg := cfg.Server.Wyoming
	if !wcfg.Enabled {
		return
	}
	if app.STTRouter == nil && app.TTSRouter == nil {
		slog.Warn("wyoming: enabled but neither STT nor TTS router is available; not starting")
		app.Health.SetReadyWithOptions(componentID, StatusUnavailable,
			"no STT or TTS router available", ComponentOptions{Blocking: false, Kind: "feature"})
		return
	}

	addr := strings.TrimSpace(wcfg.Addr)
	if addr == "" {
		addr = ":10300"
	}
	serviceName := strings.TrimSpace(wcfg.ServiceName)
	if serviceName == "" {
		serviceName = "speechkit"
	}
	langs := wcfg.Languages
	if len(langs) == 0 {
		langs = []string{"en"}
	}

	cidrs, err := parseWyomingCIDRs(wcfg.AllowedClientCIDRs)
	if err != nil {
		slog.Warn("wyoming: invalid allowed_client_cidrs; connection restriction disabled", "err", err)
		cidrs = nil
	}

	opts := wyoming.Options{
		Info:         buildWyomingInfo(serviceName, langs, strings.TrimSpace(wcfg.Voice), app.STTRouter != nil, app.TTSRouter != nil),
		DefaultVoice: strings.TrimSpace(wcfg.Voice),
		DecodeLimits: audio.DecodeLimits{MaxDecodedAudioSeconds: cfg.Server.MaxDecodedAudioSeconds},
		AllowedCIDRs: cidrs,
	}
	// Guard the interface assignments so a typed-nil router pointer never lands
	// in the interface (which would then be non-nil).
	if app.STTRouter != nil {
		opts.STT = app.STTRouter
	}
	if app.TTSRouter != nil {
		opts.TTS = app.TTSRouter
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		slog.Warn("wyoming: listen failed", "addr", addr, "err", err)
		app.Health.SetReadyWithOptions(componentID, StatusUnavailable,
			"listen failed: "+err.Error(), ComponentOptions{Blocking: false, Kind: "feature"})
		return
	}

	srv := wyoming.NewServer(opts)
	go func() {
		if err := srv.Serve(ctx, ln); err != nil {
			slog.Warn("wyoming: serve ended with error", "err", err)
		}
	}()
	app.Health.SetReady(componentID, StatusOK, "listening on "+addr)
	slog.Info("wyoming voice backend listening",
		"addr", addr, "service", serviceName, "asr", app.STTRouter != nil, "tts", app.TTSRouter != nil)
}

// wyomingNeedsSTT / wyomingNeedsTTS report whether the Wyoming adapter requires
// the shared router even though no HTTP mode/feature does.
func wyomingNeedsSTT(cfg *config.Config) bool { return cfg.Server.Wyoming.Enabled }
func wyomingNeedsTTS(cfg *config.Config) bool { return cfg.Server.Wyoming.Enabled }

func buildWyomingInfo(serviceName string, langs []string, voice string, hasSTT, hasTTS bool) wyoming.Info {
	attr := wyoming.Attribution{Name: serviceName}
	var info wyoming.Info
	if hasSTT {
		info.ASR = []wyoming.AsrProgram{{
			Name:        serviceName,
			Attribution: attr,
			Installed:   true,
			Description: "SpeechKit STT router",
			Models: []wyoming.AsrModel{{
				Name:        serviceName,
				Attribution: attr,
				Installed:   true,
				Languages:   langs,
			}},
		}}
	}
	if hasTTS {
		v := voice
		if v == "" {
			v = serviceName
		}
		info.TTS = []wyoming.TtsProgram{{
			Name:        serviceName,
			Attribution: attr,
			Installed:   true,
			Description: "SpeechKit TTS router",
			Voices: []wyoming.TtsVoice{{
				Name:        v,
				Attribution: attr,
				Installed:   true,
				Languages:   langs,
			}},
		}}
	}
	return info
}

func parseWyomingCIDRs(raw []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, c := range raw {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
