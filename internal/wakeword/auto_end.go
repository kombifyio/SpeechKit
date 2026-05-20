package wakeword

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

// AutoEndConfig steuert wann eine wake-word-getriggerte Session automatisch
// endet. Typischerweise aus Client-Config (z.B. TOML [wakeword.auto_end])
// gefuellt; DefaultAutoEndConfig liefert die Framework-Defaults die jeder
// Client-Target als Baseline nutzt.
//
// SpeechKit's Voice-Agent ist by-design fuer mehrstuendige Dialoge ausgelegt
// — daher gibt es bewusst keinen Hard-Cap auf die Session-Dauer. Auto-End
// wird ueber Stille (SilenceCutoff) plus optionale Exit-Phrasen geregelt.
type AutoEndConfig struct {
	// SilenceCutoff: Dauer ohne User-Audio-Aktivitaet, nach der die Session
	// automatisch endet. Zero disables silence-based auto-end.
	SilenceCutoff time.Duration

	// ExitPhrases: case-insensitive Substring-Matcher gegen User-Transkript-
	// Snippets. Match feuert sofortiges Session-Ende. Empty slice disables
	// exit-phrase auto-end.
	//
	// Funktioniert nur wenn der gewaehlte Voice-Agent-Provider User-Transkripte
	// liefert (Gemini Live mit EnableInputAudioTranscription, OpenAI Realtime
	// mit Input-Transcription, Local Cascaded immer). Ohne Transkripte bleibt
	// SilenceCutoff allein die End-Garantie.
	ExitPhrases []string
}

// DefaultAutoEndConfig liefert die Framework-Baseline:
//   - SilenceCutoff: 10s
//   - ExitPhrases: DE+EN-Closer (case-insensitive)
//
// Client-Targets ueberschreiben einzelne Felder via Config oder ersetzen das
// ganze Default-Set, wenn ihre Locale anders aussieht.
func DefaultAutoEndConfig() AutoEndConfig {
	return AutoEndConfig{
		SilenceCutoff: 10 * time.Second,
		ExitPhrases: []string{
			"danke", "tschuess", "tschüss", "ende", "stop",
			"thanks", "thank you", "bye", "goodbye",
		},
	}
}

// EndReason ist das Ergebnis des AutoEnd-Watchers; vom Client-Adapter an den
// session.end-Audit-Event weitergereicht.
type EndReason string

const (
	EndReasonSilence    EndReason = "silence"
	EndReasonExitPhrase EndReason = "exit_phrase"
)

// AutoEndPolicy ist ein per-Session-Watcher. Provider-agnostisch — kennt
// keine Gemini/OpenAI/Pipeline-Details. Der Client-Adapter wired:
//
//   - Audio-Frames oder VAD-Onset -> NotifyActivity
//   - User-Transkript-Snippets -> NotifyTranscript
//   - EndSignal -> Session-Stop-Pfad
//
// Lifecycle: NewAutoEndPolicy -> Start -> Notify*-Calls -> EndSignal feuert
// (genau einmal) -> Close (idempotent).
type AutoEndPolicy struct {
	cfg    AutoEndConfig
	logger *slog.Logger

	mu      sync.Mutex
	started bool
	closed  bool
	fired   bool
	timer   *time.Timer
	endCh   chan EndReason
}

// NewAutoEndPolicy konstruiert eine Policy aus Config. Logger darf nil sein
// (faellt auf slog.Default zurueck). Ein Zero-Value-Config (SilenceCutoff=0
// UND len(ExitPhrases)=0) wird durch DefaultAutoEndConfig ersetzt, damit
// Clients die das AutoEnd-Feature nicht aktiv konfigurieren trotzdem das
// Framework-Default-Verhalten bekommen.
func NewAutoEndPolicy(cfg AutoEndConfig, logger *slog.Logger) *AutoEndPolicy {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.SilenceCutoff <= 0 && len(cfg.ExitPhrases) == 0 {
		cfg = DefaultAutoEndConfig()
	}
	return &AutoEndPolicy{
		cfg:    cfg,
		logger: logger,
		endCh:  make(chan EndReason, 1),
	}
}

// Config gibt die zugrundeliegende Konfiguration zurueck (Kopie).
func (p *AutoEndPolicy) Config() AutoEndConfig {
	cfg := p.cfg
	if len(p.cfg.ExitPhrases) > 0 {
		cfg.ExitPhrases = append([]string(nil), p.cfg.ExitPhrases...)
	}
	return cfg
}

// Start armiert den Silence-Timer und markiert die Session als aktiv.
// Idempotent — wiederholte Aufrufe sind no-ops.
func (p *AutoEndPolicy) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.closed {
		return
	}
	p.started = true
	p.resetSilenceTimerLocked()
}

// NotifyActivity setzt den Silence-Timer zurueck. Vom Client-Adapter bei
// User-Audio-Aktivitaet (PCM-Frame, VAD-Onset) aufgerufen. No-op nach Close
// oder vor Start.
func (p *AutoEndPolicy) NotifyActivity() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.closed || p.fired {
		return
	}
	p.resetSilenceTimerLocked()
}

// NotifyTranscript prueft text gegen die Exit-Phrase-Liste. Match
// (case-insensitive substring) feuert EndSignal mit EndReasonExitPhrase.
// Resetet auch den Silence-Timer (Transkript ist Aktivitaet).
func (p *AutoEndPolicy) NotifyTranscript(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.closed || p.fired {
		return
	}
	p.resetSilenceTimerLocked()
	lowered := strings.ToLower(text)
	for _, phrase := range p.cfg.ExitPhrases {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if phrase == "" {
			continue
		}
		if strings.Contains(lowered, phrase) {
			p.logger.Info("wakeword: exit phrase matched, ending session",
				"phrase", phrase, "transcript_snippet", text)
			p.fireLocked(EndReasonExitPhrase)
			return
		}
	}
}

// EndSignal liefert einen Channel der genau einmal feuert wenn ein
// Auto-End-Trigger matched. Nach dem Fire soll der Consumer Close aufrufen.
// Wenn Close aufgerufen wird ohne dass die Policy gefeuert hat, wird der
// Channel ohne Wert geschlossen — der Receiver bekommt zero-value + ok=false.
func (p *AutoEndPolicy) EndSignal() <-chan EndReason {
	return p.endCh
}

// Close stoppt den Silence-Timer und schliesst den endCh wenn noch nicht
// gefeuert. Idempotent — wiederholte Aufrufe sind no-ops. Notify*-Calls nach
// Close sind ebenfalls no-ops und panicken nicht.
func (p *AutoEndPolicy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if !p.fired {
		close(p.endCh)
	}
}

func (p *AutoEndPolicy) resetSilenceTimerLocked() {
	if p.cfg.SilenceCutoff <= 0 {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.cfg.SilenceCutoff, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.closed || p.fired || !p.started {
			return
		}
		p.logger.Info("wakeword: silence cutoff reached, ending session",
			"silence_cutoff", p.cfg.SilenceCutoff)
		p.fireLocked(EndReasonSilence)
	})
}

func (p *AutoEndPolicy) fireLocked(reason EndReason) {
	if p.fired || p.closed {
		return
	}
	p.fired = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	select {
	case p.endCh <- reason:
		close(p.endCh)
	default:
		// Buffered channel of size 1 already used — should not happen
		// because fired-guard prevents double-send.
		p.logger.Warn("wakeword: AutoEndPolicy end channel full", "reason", reason)
	}
}
