//go:build linux

package core

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/toolbridge"
	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// toolBridgeRouter adapts the platform-neutral toolbridge.Bridge to the
// linux-only vsserver.SessionToolRouter interface. It remembers each
// session's resolved locale/persona from Definitions so Execute can populate
// the invoke request's session block; entries are purged lazily.
type toolBridgeRouter struct {
	bridge *toolbridge.Bridge

	mu       sync.Mutex
	sessions map[string]toolBridgeSessionMeta
}

type toolBridgeSessionMeta struct {
	locale    string
	personaID string
	touched   time.Time
}

const toolBridgeSessionMetaTTL = 2 * time.Hour

func (r *toolBridgeRouter) Definitions(ctx context.Context, session *vsserver.ManagedSession, cfg vsserver.LiveConfigFrame) []vsserver.ToolDefinitionFrame {
	if session == nil || strings.TrimSpace(session.BridgeCredential) == "" {
		// Fail-closed: no forwarded credential means no tools for this
		// session (e.g. direct bearer callers, self-hosted without an edge).
		return nil
	}
	r.rememberSession(session.ID, cfg)
	defs, err := r.bridge.Definitions(ctx, r.bridgeSession(session))
	if err != nil {
		slog.Warn("voiceagent: tool bridge manifest fetch failed; session continues tool-less", "err", err) // #nosec G706 -- structured attribute, no credential in the error path.
		return nil
	}
	out := make([]vsserver.ToolDefinitionFrame, 0, len(defs))
	for _, def := range defs {
		out = append(out, vsserver.ToolDefinitionFrame{
			Name:                 def.Name,
			Description:          def.Description,
			ParametersJSONSchema: def.Parameters,
			// Bridge tools are always blocking: Gemini 3.1 flash live rejects
			// non_blocking function declarations.
			Behavior:  string(live.ToolBehaviorBlocking),
			TimeoutMs: def.TimeoutMs,
		})
	}
	return out
}

func (r *toolBridgeRouter) Execute(ctx context.Context, session *vsserver.ManagedSession, call vsserver.ToolCall) (map[string]any, bool) {
	if session == nil || strings.TrimSpace(session.BridgeCredential) == "" {
		return nil, false
	}
	return r.bridge.Execute(ctx, r.bridgeSession(session), call.Name, call.Args)
}

func (r *toolBridgeRouter) bridgeSession(session *vsserver.ManagedSession) toolbridge.Session {
	meta := r.sessionMeta(session.ID)
	return toolbridge.Session{
		ID:         session.ID,
		Locale:     meta.locale,
		PersonaID:  meta.personaID,
		Credential: session.BridgeCredential,
	}
}

func (r *toolBridgeRouter) rememberSession(sessionID string, cfg vsserver.LiveConfigFrame) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, meta := range r.sessions {
		if now.Sub(meta.touched) > toolBridgeSessionMetaTTL {
			delete(r.sessions, id)
		}
	}
	r.sessions[sessionID] = toolBridgeSessionMeta{
		locale:    cfg.Locale,
		personaID: cfg.PersonaID,
		touched:   now,
	}
}

func (r *toolBridgeRouter) sessionMeta(sessionID string) toolBridgeSessionMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	meta := r.sessions[sessionID]
	meta.touched = time.Now()
	r.sessions[sessionID] = meta
	return meta
}
