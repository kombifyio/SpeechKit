//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// applyVoicePrefDefaults fills the provider and persona a client omitted from
// the start frame with the user's edge-resolved voice preferences captured at
// session-mint time (middleware.VoicePrefsFromContext → ManagedSession).
// Explicit start-frame values always win. Preferences are best-effort per the
// voice-preferences contract: a preferred provider that is not configured on
// this server is skipped (the server default applies) instead of failing the
// session, and the returned personaFromPref flag lets the caller retry
// persona resolution without the preference when the preferred persona does
// not resolve.
func (a *Adapter) applyVoicePrefDefaults(start *StartFrame) (personaFromPref bool) {
	if a.Session == nil || start == nil {
		return false
	}
	prefs := a.Session.VoicePrefs
	if binding := a.Session.VoiceAgentBinding; binding.TargetAgentID != "" && a.Provider == nil {
		start.Provider = "kombify-agent"
	}
	// Provider default only matters when production provider selection runs
	// (a.Provider == nil); tests that pre-inject a provider keep their frame.
	if a.Provider == nil && strings.TrimSpace(start.Provider) == "" {
		if pref := normalizeProviderName(prefs.VAProvider); pref != "" {
			if _, ok := a.Providers[pref]; ok {
				start.Provider = pref
			} else {
				slog.Info("voiceagent: preferred provider not configured on this server; using default",
					"preferred_provider", pref,
					"session_id", a.Session.ID,
				)
			}
		}
	}
	if strings.TrimSpace(start.PersonaID) == "" {
		if pref := strings.TrimSpace(prefs.VAPersona); pref != "" {
			start.PersonaID = pref
			personaFromPref = true
		}
	}
	return personaFromPref
}

// resolvePersonaConfig resolves the persona/role config for the session.
// When the persona came from a user preference (not an explicit client
// value) and does not resolve, the adapter retries with the server-default
// persona instead of failing the session — preferences fall back, explicit
// requests still error.
func (a *Adapter) resolvePersonaConfig(start *StartFrame, personaFromPref bool) (LiveConfigFrame, error) {
	cfg, err := a.Persona.Resolve(*start)
	if err != nil && personaFromPref {
		slog.Warn("voiceagent: preferred persona did not resolve; falling back to default persona",
			"preferred_persona", start.PersonaID,
			"session_id", a.Session.ID,
			"err", err,
		)
		start.PersonaID = ""
		cfg, err = a.Persona.Resolve(*start)
	}
	return cfg, err
}

// selectProvider resolves the client-requested provider name (or the server
// default when empty) to a fresh provider instance. Returns the resolved name
// so the caller can normalise the StartFrame for downstream resolution.
func (a *Adapter) selectProvider(requested string) (LiveProviderAdapter, string, error) {
	name := normalizeProviderName(requested)
	if name == "" {
		name = normalizeProviderName(a.DefaultProvider)
	}
	if name == "" {
		return nil, "", errors.New("no voice agent provider requested and no server default configured")
	}
	factory, ok := a.Providers[name]
	if !ok || factory == nil {
		available := make([]string, 0, len(a.Providers))
		for k := range a.Providers {
			available = append(available, k)
		}
		sort.Strings(available)
		return nil, name, errors.New("voice agent provider " + strconv.Quote(name) +
			" is not available on this server; configured: " + strings.Join(available, ", "))
	}
	return factory.NewProvider(), name, nil
}

func normalizeProviderName(provider string) string {
	// Canonical alias table lives in internal/config so this adapter, the
	// core serving wiring, and catalog readiness cannot drift.
	return config.NormalizeVoiceAgentProviderName(provider)
}

func (a *Adapter) waitForStart(ctx context.Context) (StartFrame, error) {
	// Tight deadline so clients that never send `start` don't park a slot.
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	typ, data, err := a.Conn.Read(readCtx)
	if err != nil {
		return StartFrame{}, err
	}
	if typ != websocket.MessageText {
		return StartFrame{}, errors.New("first frame must be text 'start'")
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return StartFrame{}, err
	}
	if env.Type != MsgStart {
		return StartFrame{}, errors.New("first frame must be 'start'")
	}
	var start StartFrame
	if err := json.Unmarshal(data, &start); err != nil {
		return StartFrame{}, err
	}
	return start, nil
}
