package pipeline

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// liveSessionLimit bounds how many provider-native dictation sessions the
// worker remembers. A session's batch tail (Stop after a failed stream) can
// commit after the stream ended, so an entry outlives its session; the oldest
// entries are dropped.
const liveSessionLimit = 8

// errLiveOutputHeld marks a live final the worker kept back because an
// earlier delivery in the same session was blocked. Nothing was sent.
var errLiveOutputHeld = fmt.Errorf("%w: live insertion held until the recording ends", speechkit.ErrOutputBlocked)

// liveSession is one provider-native live dictation as the worker sees it:
// the joined text of its finals (one history entry for the whole recording)
// and, once a delivery was blocked while the microphone was open, the output
// held back until the session ends.
type liveSession struct {
	parts      []liveSessionPart
	text       string
	durationMs int64
	latencyMs  int64

	holding        bool
	ended          bool
	held           string
	heldJob        speechkit.TranscriptionJob
	heldTranscript speechkit.Transcript

	// deliverMu serialises the session's deliveries (a live-commit timer
	// flush can race the receiver's) and makes the session's end wait for
	// one in flight, so a blocked fragment is never lost between them.
	deliverMu sync.Mutex

	// persistMu serialises the entry's create and updates; rowID is the
	// history entry, rowless records a store that cannot extend entries.
	persistMu sync.Mutex
	rowID     int64
	rowless   bool
}

// liveSessionPart is one committed final; order is its segment, with a batch
// tail (no segment) last, so slow commits cannot reorder the history text.
type liveSessionPart struct {
	order uint64
	text  string
}

type liveSessions struct {
	mu    sync.Mutex
	byID  map[uint64]*liveSession
	order []uint64
}

// open registers a provider-native session the first time one of its finals
// arrives; later calls return the same entry.
func (l *liveSessions) open(id uint64) *liveSession {
	if id == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if session, ok := l.byID[id]; ok {
		return session
	}
	if l.byID == nil {
		l.byID = make(map[uint64]*liveSession)
	}
	session := &liveSession{}
	l.byID[id] = session
	l.order = append(l.order, id)
	for len(l.order) > liveSessionLimit {
		delete(l.byID, l.order[0])
		l.order = l.order[1:]
	}
	return session
}

// get returns the session a job belongs to, or nil when the job is not part
// of a provider-native live dictation (batch captures carry no session ID).
func (l *liveSessions) get(id uint64) *liveSession {
	if id == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.byID[id]
}

// append adds a committed final to the session's joined text and returns the
// snapshot the history entry should hold.
func (l *liveSessions) append(session *liveSession, segment uint64, text string, durationMs, latencyMs int64) (joined string, totalMs, firstLatencyMs int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	order := segment
	if order == 0 {
		order = math.MaxUint64
	}
	at, _ := slices.BinarySearchFunc(session.parts, order, func(p liveSessionPart, o uint64) int {
		switch {
		case p.order < o:
			return -1
		case p.order > o:
			return 1
		default:
			return 0
		}
	})
	// Equal orders (several tails) keep arrival order.
	for at < len(session.parts) && session.parts[at].order == order {
		at++
	}
	session.parts = slices.Insert(session.parts, at, liveSessionPart{order: order, text: text})
	texts := make([]string, 0, len(session.parts))
	for _, part := range session.parts {
		texts = append(texts, part.text)
	}
	session.text = JoinTranscriptFragments(texts...)
	session.durationMs += durationMs
	if session.latencyMs == 0 {
		session.latencyMs = latencyMs
	}
	return session.text, session.durationMs, session.latencyMs
}

// hold keeps fragment back when the session is already holding. It reports
// whether the caller must skip delivery.
func (l *liveSessions) hold(session *liveSession, fragment string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !session.holding || session.ended {
		return false
	}
	session.held += fragment
	return true
}

// startHolding records a blocked delivery while the microphone is open: the
// blocked fragment and every later final of the session wait for its end.
func (l *liveSessions) startHolding(session *liveSession, fragment string, job speechkit.TranscriptionJob, transcript speechkit.Transcript) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if session.ended || session.holding {
		return false
	}
	session.holding = true
	session.held = fragment
	session.heldJob = job
	session.heldTranscript = transcript
	return true
}

// end marks the session over and hands back what it held.
func (l *liveSessions) end(id uint64) (held string, job speechkit.TranscriptionJob, transcript speechkit.Transcript, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	session := l.byID[id]
	if session == nil {
		return "", speechkit.TranscriptionJob{}, speechkit.Transcript{}, false
	}
	session.ended = true
	if !session.holding {
		return "", speechkit.TranscriptionJob{}, speechkit.Transcript{}, false
	}
	held, job, transcript = session.held, session.heldJob, session.heldTranscript
	session.holding = false
	session.held = ""
	return held, job, transcript, held != ""
}

// liveSessionFor returns the live session a commit belongs to. Quick Notes,
// meeting captures and recording sessions keep their own records, so only
// plain dictation is merged.
func (w *TranscriptionWorker) liveSessionFor(job speechkit.TranscriptionJob, transcript speechkit.Transcript) *liveSession {
	if job.QuickNote || job.CaptureChannel != "" || job.RecordingSessionID != 0 {
		return nil
	}
	return w.live.get(transcript.SessionID)
}

// deliverLive sends one live final, or holds it when the session's output is
// already held. A blocked delivery while the microphone is open starts the
// hold, so the rest of the dictation is inserted once when it ends instead of
// fragment by fragment into a target that just refused text.
func (w *TranscriptionWorker) deliverLive(ctx context.Context, session *liveSession, job speechkit.TranscriptionJob, transcript, inject speechkit.Transcript) error {
	if session == nil {
		return w.output.Deliver(ctx, inject, job.Target)
	}
	session.deliverMu.Lock()
	defer session.deliverMu.Unlock()
	if w.live.hold(session, inject.Text) {
		return errLiveOutputHeld
	}
	err := w.output.Deliver(ctx, inject, job.Target)
	if errors.Is(err, speechkit.ErrOutputBlocked) &&
		w.live.startHolding(session, inject.Text, job, transcript) {
		w.onLog("Live insertion blocked; the rest of this dictation is inserted when the recording ends", "warn")
	}
	return err
}

// EndDictationStreamSession implements [speechkit.DictationStreamSessionEnder].
// When a delivery was blocked during the session, the held text is inserted
// once, like a full-capture dictation inserts at the end. If the target still
// refuses it, the text stays in history as one entry.
func (w *TranscriptionWorker) EndDictationStreamSession(ctx context.Context, sessionID uint64, _ speechkit.DictationStreamSinkOptions) {
	if w == nil {
		return
	}
	if session := w.live.get(sessionID); session != nil {
		session.deliverMu.Lock()
		defer session.deliverMu.Unlock()
	}
	held, job, transcript, ok := w.live.end(sessionID)
	if !ok || w.output == nil {
		return
	}
	inject := transcript
	inject.Text = held
	started := time.Now()
	err := w.output.Deliver(ctx, inject, job.Target)
	finalization := speechkit.NewTranscriptionFinalization(inject, nil, true, false).WithOutputResult(err)
	w.onFinalization(job, inject, finalization)
	if err != nil {
		w.onLog(outputNotConfirmedMessage(err), "warn")
	} else {
		w.onLog("Held live dictation inserted", "success")
	}
	w.onLog(fmt.Sprintf("STT timing: output_delivery=%dms", time.Since(started).Milliseconds()), "info")
	w.onState(finalizationState(finalization, held))
}

// persistLiveSessionAsync saves a live final into its session's single
// history entry: the first final creates it, later ones rewrite it with the
// joined text. A store without that capability saves each final on its own.
func (w *TranscriptionWorker) persistLiveSessionAsync(parent context.Context, session *liveSession, job speechkit.TranscriptionJob, transcript speechkit.Transcript, finalization speechkit.TranscriptionFinalization) {
	if w.runner == nil {
		return
	}
	sessionStore, ok := w.runner.store.(speechkit.TranscriptionSessionStore)
	if !ok {
		w.persistTranscriptionAsync(parent, job, transcript, finalization)
		return
	}
	durationMs := int64(job.DurationSecs * 1000)
	w.live.append(session, transcript.SegmentID, transcript.Text, durationMs, transcript.Duration.Milliseconds())

	w.persistWG.Add(1)
	go func() {
		defer w.persistWG.Done()
		defer w.recoverWorkerGoroutine("persistLiveSession")

		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
		defer cancel()

		session.persistMu.Lock()
		defer session.persistMu.Unlock()
		if session.rowless {
			w.saveTranscription(ctx, job, transcript, finalization, durationMs)
			return
		}
		// Read the joined text under persistMu: whichever goroutine runs
		// last writes the newest text, so out-of-order runs never regress it.
		w.live.mu.Lock()
		text, totalMs, latencyMs := session.text, session.durationMs, session.latencyMs
		w.live.mu.Unlock()

		created := session.rowID == 0
		var err error
		if created {
			var id int64
			id, err = sessionStore.CreateTranscription(ctx, text, transcript.Language, transcript.Provider, transcript.Model, totalMs, latencyMs, persistableAudio(job.Submission))
			if errors.Is(err, errors.ErrUnsupported) {
				session.rowless = true
				w.saveTranscription(ctx, job, transcript, finalization, durationMs)
				return
			}
			session.rowID = id
		} else {
			err = sessionStore.UpdateTranscriptionText(ctx, session.rowID, text, totalMs, latencyMs)
		}
		if err != nil {
			w.onFinalization(job, transcript, finalization.WithPersistenceResult(err))
			w.onLog("Transcription history could not be saved", "warn")
			return
		}
		w.onFinalization(job, transcript, finalization.WithPersistenceResult(nil))
		// Only the entry's creation adds a transcription; updates extend it.
		w.runner.notifyCommit(speechkit.Completion{
			Transcript:             transcript,
			TranscriptionPersisted: created,
			AudioDurationMs:        durationMs,
		})
	}()
}
