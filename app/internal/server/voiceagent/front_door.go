//go:build linux

package voiceagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/voiceagent/a2a"
)

// Front door: deterministic, transcript-level routing for a lean always-on
// device (Box, C6, watch). The device only opens a session on its default
// route; the server decides from the FINAL transcript of each turn what the
// person asked for and tells the device with a typed, session-bound receipt:
//
//   - "start the voice agent" / "let's talk"  -> conversation_mode receipt
//     (mode conversation): the device keeps listening after each answer.
//   - "end the conversation" / "that's all"   -> conversation_mode receipt
//     (mode oneshot).
//   - "ask the Companion ..." / "Companion, ..." -> route_switch receipt (to
//     agent): the device moves to the registered Companion route and replays
//     the utterance as the first text turn.
//
// The decision is a handful of anchored RE2 patterns over the normalized
// transcript: no provider round trip, microseconds, no new failure mode. A
// miss leaves the turn with the realtime model, which can still hand over
// through the kombify_handoff_companion tool (front_door_tool.go).
//
// Opt-in per session through the top-level start field front_door. An older
// server ignores the field (the start frame is plain JSON), so a device can
// send it before the server supports it. The route_switch receipt
// additionally requires routes ["agent"]; unknown routes are ignored.

const (
	// EventRouteSwitch is the event_type of a route_switch receipt.
	EventRouteSwitch = "route_switch"

	frontModeOneShot      = "oneshot"
	frontModeConversation = "conversation"

	routeAgent = "agent"

	maxIntentWords      = 14
	maxHandoffUtterance = 512
	muteNextWindow      = 10 * time.Second
)

// RouteSwitchReceipt asks the client to continue on another route. It is only
// sent for a session that opted in, and only the authenticated server emits it.
type RouteSwitchReceipt struct {
	Version        int    `json:"version"`
	To             string `json:"to"`
	VoiceSessionID string `json:"voice_session_id"`
	RequestID      string `json:"request_id"`
	Reason         string `json:"reason"`
	// Utterance is the final transcript that caused the switch; the client
	// replays it as the first text frame on the target session.
	Utterance string `json:"utterance,omitempty"`
}

type voiceIntentKind int

const (
	intentNone voiceIntentKind = iota
	intentConversation
	intentOneShot
	intentCompanion
)

func (k voiceIntentKind) String() string {
	switch k {
	case intentConversation:
		return "conversation"
	case intentOneShot:
		return "oneshot"
	case intentCompanion:
		return "companion"
	default:
		return "none"
	}
}

var intentFold = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "é", "e", "è", "e")

// normalizeIntentText lower-cases, folds umlauts, drops apostrophes and turns
// every other non-alphanumeric run into one space, so "Das war's!" and
// "das wars" are the same string. A comma survives as its own token because
// "Companion, wie spät ist es" addresses the Companion where
// "Companion apps are great" does not.
func normalizeIntentText(text string) string {
	text = intentFold.Replace(strings.ToLower(strings.TrimSpace(text)))
	var b strings.Builder
	space := true
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case r == '\'' || r == '’' || r == '`':
			// "that's" -> "thats"
		case r == ',':
			if !space {
				b.WriteByte(' ')
			}
			b.WriteByte(',')
			b.WriteByte(' ')
			space = true
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

const (
	// Fillers before a MODE command ("danke, das wars", "ok, lass uns reden").
	modeFiller = `(?:(?:hey|hi|hallo|ok|okay|bitte|dann|also|mal|und|danke|gut|so|jetzt|please|thanks|then|well|great) ,? ?)*`
	// Fillers before a Companion address. Thanks and acknowledgements are not
	// an address: "Danke Companion" and "Ok Kubi" are not requests.
	companionFiller = `(?:(?:hey|hi|hallo|also|und|bitte|please) ,? ?)*`

	companionNameRE = `(?:companion|kompanion)`
	anyNameRE       = `(?:companion|kompanion|kubi|kuby|kubie|cubi|kuebi)`
	articleRE       = `(?:(?:den|dem|die|das|deinen|deinem|unseren|unserem|meinen|meinem|the|my|our|your) )?`

	// What may follow the name after an addressing verb ("frag den Companion
	// ob ...", "ask the companion to ..."). Anything else ("Lass den Kubi in
	// Ruhe", "Get the companion cube", "Tell the companion story again") is
	// talk ABOUT the name, not a request to it.
	afterNameRE = `(?: ,|$| (?:ob|wie|was|wann|wo|wer|warum|wieso|welche|dass|bitte|mir|nach|wegen|zu|um|er|sie|es|du|weiter|rueber|ueber|mal|doch|fuer|to|if|whether|what|how|when|where|who|why|that|about|for|please|me|over|can|could|will)(?: |$))`
	// What may follow the name when it starts the sentence: a comma, nothing,
	// or an imperative, modal or question word ("Companion, ...", "Companion
	// zeig mir ...", "Companion what is ..."). "Companion apps are great" is
	// not an address.
	startNameFollowRE = `(?: ,|$| (?:mach|mache|zeig|zeige|sag|sage|starte|start|schalte|schalt|pruef|pruefe|check|erinner|erinnere|schreib|schreibe|such|suche|finde|ruf|rufe|oeffne|loesche|erstelle|plane|buche|sende|schick|schicke|kannst|koenntest|bitte|was|wie|wann|wo|wer|warum|wieso|welche|show|tell|open|create|delete|send|remind|can|could|please|what|how|when|where|who|why|do|does)(?: |$))`
)

var (
	// "start the voice agent" family. Questions ("wie starte ich ...") never
	// match because every pattern is anchored on the imperative. The verbs
	// mach/schalte need their particle (an/ein/auf): "Mach das Gespräch" is
	// not a start command.
	conversationObject = `(?:voice ?agent|voiceagent|voice assistent|sprachagent|sprachassistent(?:en)?|echtzeit ?(?:agent|assistent(?:en)?|modus)|live ?(?:modus|gespraech)|gespraech|unterhaltung|dialog|konversation)`
	conversationPats   = []*regexp.Regexp{
		regexp.MustCompile(`^` + modeFiller + `(?:(?:kannst|koenntest|magst|wuerdest) du )?(?:bitte )?(?:starte|start|starten|aktiviere|aktivieren|oeffne|beginne|beginn|fang|fange|lade) (?:(?:mal|bitte|jetzt|uns|mir|doch|einfach) )*(?:(?:den|das|die|einen|ein|eine|mit dem|mit der|meinen|dein|deinen) )?` + conversationObject + `(?: (?:an|ein|auf|bitte|jetzt|mal|mit mir|los|starten|aktivieren))*$`),
		regexp.MustCompile(`^` + modeFiller + `(?:(?:kannst|koenntest|magst|wuerdest) du )?(?:bitte )?(?:mach|mache|schalte|schalt|stell|stelle) (?:(?:mal|bitte|jetzt|doch|einfach) )*(?:(?:den|das|die|einen|ein|eine|meinen|deinen) )?` + conversationObject + ` (?:an|ein|auf|los)(?: (?:bitte|jetzt|mal))*$`),
		regexp.MustCompile(`^` + modeFiller + `(?:(?:kannst|koenntest|magst|wuerdest) du )?(?:bitte )?(?:den|das|die|einen|ein|eine|meinen|deinen) ` + conversationObject + ` (?:bitte )?(?:starten|aktivieren|an ?machen|einschalten|oeffnen|beginnen)$`),
		regexp.MustCompile(`^` + modeFiller + `lass uns (?:(?:bitte|mal|jetzt|ein bisschen|etwas|noch|weiter|doch|einfach) )*(?:reden|sprechen|unterhalten|quatschen|plaudern|ein gespraech fuehren)(?: (?:bitte|mal|jetzt|doch|einfach|gleich))*$`),
		regexp.MustCompile(`^` + modeFiller + `ich (?:moechte|will|wuerde gern|wuerde gerne) (?:mit dir |mich mit dir )?(?:reden|sprechen|ein gespraech fuehren|mich unterhalten|unterhalten)$`),
		regexp.MustCompile(`^` + modeFiller + `unterhalte dich mit mir$`),
		regexp.MustCompile(`^` + modeFiller + `(?:bleib|bleibe) (?:bitte )?(?:im gespraech|dran|da|an)$`),
		regexp.MustCompile(`^` + modeFiller + `(?:voice ?agent|sprachagent|sprachassistent) (?:(?:bitte|jetzt|mal) )*(?:an|ein|starten|aktivieren|los)$`),
		regexp.MustCompile(`^` + modeFiller + `(?:(?:can|could) you )?(?:please )?(?:start|begin|open|activate|launch|switch to|turn on) (?:the |a |my )?(?:voice ?agent|conversation|realtime (?:mode|assistant|agent)|live (?:mode|conversation)|voice mode)(?: (?:on|now|please|with me))*$`),
		regexp.MustCompile(`^` + modeFiller + `(?:lets|let us) (?:talk|chat|have a (?:conversation|chat))(?: (?:please|now))*$`),
		regexp.MustCompile(`^` + modeFiller + `(?:keep (?:listening|talking)|talk to me|stay on the line)$`),
		regexp.MustCompile(`^` + modeFiller + `voice ?agent (?:on|start|now|please)$`),
	}

	endObject   = `(?:gespraech|unterhaltung|voice ?agent|sprachagent|sprachassistent(?:en)?|dialog|konversation)`
	oneShotPats = []*regexp.Regexp{
		regexp.MustCompile(`^` + modeFiller + `(?:beende|beenden|stopp|stoppe|stoppen|schliesse|schliess) (?:(?:bitte|jetzt|das|die|den|mal|einfach|unser|unsere|unseren|dieses|diese) )*` + endObject + `(?: (?:bitte|jetzt|beenden|ende|aus|ab|zu))*$`),
		regexp.MustCompile(`^` + modeFiller + `(?:mach|mache|schalte|schalt) (?:(?:bitte|jetzt|das|die|den|mal|einfach|unser|unsere|unseren|dieses|diese) )*` + endObject + ` (?:aus|ab|zu)(?: (?:bitte|jetzt))*$`),
		regexp.MustCompile(`^` + modeFiller + `hoere? (?:bitte )?(?:jetzt )?auf mit (?:dem |der |unserem |unserer )?` + endObject + `$`),
		regexp.MustCompile(`^` + modeFiller + `(?:gespraech|unterhaltung|voice ?agent|sprachagent) (?:(?:jetzt|bitte) )*(?:beenden|beende|ende|aus|stoppen|schliessen)$`),
		regexp.MustCompile(`^(?:(?:ok|okay|gut|so|danke|dankeschoen|vielen dank|ja|nein|super|prima) ,? ?)*(?:das war(?:s| es)|das ist alles|das wars fuer jetzt|wir sind fertig|ich bin fertig|mehr brauche ich nicht|das reicht|das reicht mir|genug)(?: (?:danke|fuer jetzt|schoen|dankeschoen|vielen dank))*$`),
		regexp.MustCompile(`^(?:tschuess|tschuss|auf wiedersehen|bis spaeter|ciao|bis bald)$`),
		regexp.MustCompile(`^` + modeFiller + `(?:end|stop|close|finish|exit|quit) (?:the |this |our |my )?(?:conversation|voice ?agent|chat|dialog)(?: (?:now|please))*$`),
		regexp.MustCompile(`^(?:(?:ok|okay|thanks|thank you|great|good) ,? ?)*(?:thats (?:all|it)|that is all|were done|we are done|i am done|im done|nothing else|stop listening|goodbye|bye|see you)(?: (?:thanks|thank you|for now))*$`),
	}

	// A leading address ("Companion,", "Kubi") is dropped before the mode
	// commands match, so "Kubi, beende das Gespräch" ends the conversation
	// and "Danke Companion, das wars" is a thank-you, not a request.
	leadingAddress = regexp.MustCompile(`^(?:(?:hey|hi|hallo|ok|okay|danke|thanks|bitte|please) ,? ?)*` + anyNameRE + `(?: ,)? ?`)

	// The turn is addressed to the Companion.
	companionPats = []*regexp.Regexp{
		// The name starts the sentence ("Companion, ...", "Companion zeig mir ...").
		// Only "Companion" does: the wake name Kubi alone is no address.
		regexp.MustCompile(`^` + companionFiller + companionNameRE + startNameFollowRE),
		// An addressing verb names it ("frag den Companion ...", "sag Kubi ...").
		regexp.MustCompile(`^` + companionFiller + `(?:(?:kannst|koenntest|magst|wuerdest) du )?(?:bitte )?(?:frag|frage|fragen|sag|sage|sagen|ruf|rufe|rufen|hol|hole|holen|wecke|wecken|aktiviere|aktivieren|starte|starten|uebergib|uebergebe|gib|gebe|leite|leit|verbinde|verbinden|schalte|schalt|lass|lasse) (?:(?:mal|doch|bitte|das|es|dies|diese|die|an|weiter|rueber|ueber|jetzt|bei|zu|mich|mit) )*` + articleRE + anyNameRE + afterNameRE),
		// "Kannst du den Companion fragen, ob ...".
		regexp.MustCompile(`^` + companionFiller + `(?:kannst|koenntest|magst|wuerdest|kann|moechtest) du (?:bitte )?(?:mal )?` + articleRE + anyNameRE + ` (?:fragen|bitten|anrufen|rufen|wecken)\b`),
		regexp.MustCompile(`^` + companionFiller + `(?:(?:can|could) you )?(?:please )?(?:ask|tell|call|wake|hand|pass|forward|switch to|connect me to|put me through to) (?:(?:this|it|that|the question|me) )*(?:(?:over )?to )?` + articleRE + anyNameRE + afterNameRE),
	}

	questionOpeners = map[string]bool{
		"wie": true, "was": true, "warum": true, "wieso": true, "weshalb": true, "wann": true, "wo": true,
		"wer": true, "welche": true, "welcher": true, "welches": true, "wozu": true, "womit": true,
		"how": true, "what": true, "why": true, "when": true, "where": true, "who": true, "which": true,
	}
)

type voiceIntent struct {
	kind voiceIntentKind
	// utterance is the original transcript, trimmed, for a Companion hand-over.
	utterance string
}

// stripLeadingWake removes the session's own wake phrase from the start of a
// normalized transcript ("hey kubi mach das licht an" -> "mach das licht an"):
// when wake verification fails open or finds no cut point, the first final
// transcript still carries the wake words.
func stripLeadingWake(norm, core string) string {
	if core == "" {
		return norm
	}
	pattern := regexp.MustCompile(`^(?:(?:hey|hi|hallo|ok|okay) )?` + regexp.QuoteMeta(core) + `(?: ,)? ?`)
	if loc := pattern.FindStringIndex(norm); loc != nil {
		return strings.TrimSpace(norm[loc[1]:])
	}
	return norm
}

// classifyVoiceIntent decides, from one final transcript, whether the person
// asked to change the conversation mode or to talk to the Companion. wakeCore
// is the distinctive word of the session's wake phrase (empty without one). It
// is total, allocation-light and has no I/O.
func classifyVoiceIntent(transcript, wakeCore string) voiceIntent {
	norm := stripLeadingWake(normalizeIntentText(transcript), wakeCore)
	if norm == "" {
		return voiceIntent{}
	}
	// A mode command wins over an address in front of it, and its patterns
	// run on text without commas.
	modeText := norm
	if loc := leadingAddress.FindStringIndex(modeText); loc != nil && loc[1] < len(modeText) {
		modeText = strings.TrimSpace(modeText[loc[1]:])
	}
	modeText = strings.TrimSpace(strings.ReplaceAll(modeText, " ,", ""))
	modeText = strings.ReplaceAll(modeText, ",", "")
	words := strings.Count(modeText, " ") + 1
	first := modeText
	if i := strings.IndexByte(modeText, ' '); i >= 0 {
		first = modeText[:i]
	}
	if words <= maxIntentWords && !questionOpeners[first] {
		for _, re := range oneShotPats {
			if re.MatchString(modeText) {
				return voiceIntent{kind: intentOneShot}
			}
		}
		for _, re := range conversationPats {
			if re.MatchString(modeText) {
				return voiceIntent{kind: intentConversation}
			}
		}
	}
	for _, re := range companionPats {
		if re.MatchString(norm) {
			return voiceIntent{kind: intentCompanion, utterance: strings.TrimSpace(transcript)}
		}
	}
	return voiceIntent{}
}

// frontDoor is the per-session state of the opt-in front door.
type frontDoor struct {
	mu       sync.Mutex // small state below
	sendMu   sync.Mutex // one route_switch send at a time
	mode     string
	routes   map[string]bool
	wakeCore string
	seq      int

	// The turn: final transcript segments accumulate until the model starts
	// replying, so a diarized turn is classified as a whole. A switch is sent
	// at most once per turn (the rule and the model handoff can race on it).
	turnID       int
	turnText     string
	switchedTurn int
	// replySeen: output started since the last final input transcript, so the
	// next one begins a new turn. muteNextUntilMs (Unix ms, 0 = off): the reply
	// had not started when the switch went out; the one that starts within the
	// window is muted.
	replySeen       atomic.Bool
	muteNextUntilMs atomic.Int64

	// localHandoff marks that the handoff tool is ours, not a bridge tool.
	localHandoff bool
}

func newFrontDoor(opt *FrontDoorStart, wakePhrase string) *frontDoor {
	routes := map[string]bool{}
	for _, route := range opt.Routes {
		if route == routeAgent {
			routes[route] = true
		}
	}
	f := &frontDoor{mode: frontModeOneShot, routes: routes, turnID: 1}
	if phrase := intentFold.Replace(strings.ToLower(strings.TrimSpace(wakePhrase))); phrase != "" {
		parts := strings.FieldsFunc(phrase, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
		if len(parts) > 0 {
			f.wakeCore = parts[len(parts)-1]
		}
	}
	return f
}

// addTranscript adds one final transcript segment to the current turn and
// returns the turn's text so far.
func (f *frontDoor) addTranscript(segment string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.replySeen.Swap(false) {
		f.turnID++
		f.turnText = ""
	}
	if f.turnText != "" {
		f.turnText += " "
	}
	f.turnText += strings.TrimSpace(segment)
	return f.turnText
}

func (f *frontDoor) nextRequestID(sessionID, kind string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", sessionID, kind, f.seq)))
	return fmt.Sprintf("front-%d-%s", f.seq, hex.EncodeToString(sum[:4]))
}

// configureFrontDoor opts a plain session into transcript routing. A session
// bound to a registered agent or an own endpoint keeps its own control and is
// left alone without an error; so is a session whose client sent no usable
// front_door field.
func (a *Adapter) configureFrontDoor(start StartFrame, cfg *LiveConfigFrame) bool {
	opt := start.FrontDoor
	if opt == nil || opt.Version != 1 {
		return false
	}
	if cfg.DirectEndpoint != nil || cfg.AgentTargetID != "" || a.Session == nil || a.Session.ID == "" {
		return false
	}
	phrase := ""
	if start.Wake != nil {
		phrase = start.Wake.Phrase
	}
	a.front = newFrontDoor(opt, phrase)
	return true
}

// routeByTranscript applies the front-door decision to a final user transcript.
func (a *Adapter) routeByTranscript(ctx context.Context, transcript string) {
	front := a.front
	if front == nil {
		return
	}
	turnText := front.addTranscript(transcript)
	intent := classifyVoiceIntent(turnText, front.wakeCore)
	switch intent.kind {
	case intentConversation:
		a.sendFrontMode(ctx, front, frontModeConversation, "spoken_command")
	case intentOneShot:
		a.sendFrontMode(ctx, front, frontModeOneShot, "spoken_command")
	case intentCompanion:
		a.sendRouteSwitch(ctx, front, "explicit_companion", intent.utterance)
	}
}

func (a *Adapter) sendFrontMode(ctx context.Context, front *frontDoor, mode, reason string) {
	front.mu.Lock()
	previous := front.mode
	front.mode = mode
	front.mu.Unlock()
	if previous == mode {
		return
	}
	receipt := a2a.VoiceModeReceipt{
		Version:               1,
		Mode:                  mode,
		VoiceSessionID:        a.Session.ID,
		ConversationSessionID: a.Session.ID,
		RequestID:             front.nextRequestID(a.Session.ID, mode),
		Status:                "accepted",
	}
	if err := a.sendConversationReceipt(ctx, receipt); err != nil {
		front.mu.Lock()
		front.mode = previous
		front.mu.Unlock()
		slog.Warn("voiceagent: front door mode receipt not delivered", "session_id", a.Session.ID, "mode", mode, "err", err)
		return
	}
	slog.Info("voiceagent: front door mode", "session_id", a.Session.ID, "mode", mode, "reason", reason)
}

// sendRouteSwitch hands the conversation to the registered Companion route.
// Only a client that listed routes ["agent"] receives it. It reports whether
// a route_switch went out for the current turn, now or earlier: false means
// nothing was sent. The realtime reply of this turn is muted.
func (a *Adapter) sendRouteSwitch(ctx context.Context, front *frontDoor, reason, utterance string) bool {
	if !front.routes[routeAgent] {
		return false
	}
	front.sendMu.Lock()
	defer front.sendMu.Unlock()
	front.mu.Lock()
	turn := front.turnID
	already := front.switchedTurn == turn
	front.mu.Unlock()
	if already {
		return true
	}
	if len(utterance) > maxHandoffUtterance {
		cut := maxHandoffUtterance
		for cut > 0 && !utf8.RuneStart(utterance[cut]) {
			cut--
		}
		utterance = utterance[:cut]
	}
	receipt := RouteSwitchReceipt{
		Version:        1,
		To:             routeAgent,
		VoiceSessionID: a.Session.ID,
		RequestID:      front.nextRequestID(a.Session.ID, "route"),
		Reason:         reason,
		Utterance:      utterance,
	}
	frame := EventFrame{Type: MsgEvent, EventFrameFields: a.eventFrameFields(nil, EventRouteSwitch), Route: &receipt}
	if err := a.writeFrameChecked(ctx, frame); err != nil {
		slog.Warn("voiceagent: front door route switch not delivered", "session_id", a.Session.ID, "err", err)
		return false
	}
	front.mu.Lock()
	front.switchedTurn = turn
	front.mu.Unlock()
	a.muteReplyForSwitch(front)
	slog.Info("voiceagent: front door route switch", "session_id", a.Session.ID, "to", routeAgent, "reason", reason)
	return true
}

// writeFrameChecked writes one JSON frame and reports a failed delivery,
// unlike sendJSON: a route_switch the device never received must not be
// reported as handed over.
func (a *Adapter) writeFrameChecked(ctx context.Context, frame any) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() || a.terminal {
		return errors.New("voiceagent: session closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.Conn.Write(writeCtx, websocket.MessageText, data)
}

// muteReplyForSwitch silences the realtime reply of the switched turn the way
// a client cancel does (handleCancel): a reply in flight is muted through its
// epoch and the provider is told to stop where its protocol allows it. When
// the reply has not started yet it is muted as it starts; when it already
// finished nothing is muted, so a later reply is never touched.
func (a *Adapter) muteReplyForSwitch(front *frontDoor) {
	a.writeMu.Lock()
	active := a.replyActive.get()
	if active {
		a.mutedThrough = max(a.mutedThrough, a.outputEpoch)
	}
	a.writeMu.Unlock()
	if !active {
		front.muteNextUntilMs.Store(time.Now().Add(muteNextWindow).UnixMilli())
	}
	pending := false
	if response, ok := a.Provider.(LivePendingResponse); ok {
		pending = response.ResponsePending()
	}
	if active || pending {
		if canceller, ok := a.Provider.(LiveResponseCanceller); ok {
			if err := canceller.CancelResponse(); err != nil {
				slog.Warn("voiceagent: provider response cancel failed after route switch", "session_id", a.Session.ID)
			}
		}
	}
}
