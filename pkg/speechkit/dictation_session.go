package speechkit

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultDictationPause                  = 1500 * time.Millisecond
	DefaultDictationMinSegment             = 1200 * time.Millisecond
	DefaultDictationMinIntermediateSegment = 6 * time.Second
	DefaultDictationParagraphPause         = 4 * time.Second
	DefaultDictationPadding                = 480 * time.Millisecond
	DefaultDictationOverlap                = 200 * time.Millisecond

	dictationFrameSize       = 512
	dictationFrameBytes      = dictationFrameSize * AudioBytesPerSample
	dictationSpeechThreshold = 0.5
)

// AudioSegment is a transcribable utterance extracted from a dictation
// recording. PCM is raw 16kHz S16 mono audio.
type AudioSegment struct {
	PCM       []byte
	Duration  time.Duration
	Paragraph bool
	Final     bool
}

// VoiceActivityDetector is the public VAD contract consumed by
// DictationSegmenter. It intentionally matches SpeechKit's internal Silero
// detector shape without exposing internal packages.
type VoiceActivityDetector interface {
	ProcessFrame([]int16) (float32, error)
	Reset()
}

// DictationSegmenter implements [SegmentCollector] using VAD-based pause
// detection to split continuous speech into discrete segments.
type DictationSegmenter struct {
	detector VoiceActivityDetector

	mu         sync.Mutex
	pause      time.Duration
	minSegment time.Duration
	// minIntermediateSegment keeps short dictations together even when the
	// speaker makes a natural pause. Once a chunk reaches this duration, the
	// next pause may close it as an intermediate transcription segment.
	minIntermediateSegment time.Duration
	paragraphPause         time.Duration
	padding                time.Duration
	overlap                time.Duration
	pending                []byte
	preRoll                []byte
	active                 []byte
	tailSilence            []byte
	inSpeech               bool
	silenceTime            time.Duration
	idleSilenceTime        time.Duration
	activeParagraph        bool
	nextParagraph          bool
	emittedAny             bool
	segments               []AudioSegment
	ingestedBytes          int

	// nowFunc is injectable for tests; production uses time.Now.
	nowFunc func() time.Time
	// idleSince is the wall-clock time at which the segmenter last
	// transitioned out of speech (or session start). Zero value means
	// "currently in speech" so a poller can short-circuit. Used by the
	// RecordingController's idle watcher to auto-stop a dictate session
	// after a configurable silence threshold.
	idleSince time.Time
}

func NewDictationSegmenter(detector VoiceActivityDetector, pauseThreshold time.Duration) *DictationSegmenter {
	if detector == nil {
		return nil
	}
	if pauseThreshold <= 0 {
		pauseThreshold = DefaultDictationPause
	}

	now := time.Now()
	return &DictationSegmenter{
		detector:               detector,
		pause:                  pauseThreshold,
		minSegment:             DefaultDictationMinSegment,
		minIntermediateSegment: DefaultDictationMinIntermediateSegment,
		paragraphPause:         DefaultDictationParagraphPause,
		padding:                DefaultDictationPadding,
		overlap:                DefaultDictationOverlap,
		idleSince:              now,
	}
}

// SetMinIntermediateSegment configures the minimum active utterance duration
// that can be emitted before Stop(). Shorter utterances stay merged across
// natural pauses so dictation does not over-fragment; <=0 emits on every
// pause-bounded segment.
func (s *DictationSegmenter) SetMinIntermediateSegment(d time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.minIntermediateSegment = d
}

// IdleSince returns the wall-clock time at which the segmenter most
// recently transitioned out of speech (or, for a fresh session that
// has not yet seen speech, the construction time). Returns the zero
// value when speech is currently being captured — the poller treats
// zero as "user is actively speaking, silence timer should reset."
//
// Satisfies the [IdleObserver] contract consumed by RecordingController
// to drive silence-based auto-stop.
func (s *DictationSegmenter) IdleSince() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inSpeech {
		return time.Time{}
	}
	return s.idleSince
}

func (s *DictationSegmenter) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func (s *DictationSegmenter) FeedPCM(pcm []byte) error {
	if s == nil || s.detector == nil || len(pcm) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.ingestedBytes += len(pcm)
	s.pending = append(s.pending, pcm...)
	for len(s.pending) >= dictationFrameBytes {
		frame := s.pending[:dictationFrameBytes]
		s.pending = s.pending[dictationFrameBytes:]

		segments, err := s.feedFrame(frame)
		if err != nil {
			return err
		}
		s.segments = append(s.segments, segments...)
	}
	return nil
}

// DrainReadySegments returns pause-bounded intermediate segments that were
// completed during FeedPCM calls. It leaves the active utterance in place so a
// later Stop() only flushes the remaining tail.
func (s *DictationSegmenter) DrainReadySegments() []AudioSegment {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.segments) == 0 {
		return nil
	}
	segments := cloneAudioSegments(s.segments)
	s.segments = nil
	return segments
}

func (s *DictationSegmenter) CollectStopSegments(fullPCM []byte) ([]AudioSegment, error) {
	if s == nil || s.detector == nil {
		return FallbackDictationSegments(fullPCM), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var flushed []AudioSegment
	emittedAny := s.emittedAny
	if len(s.pending) > 0 {
		s.active = append(s.active, s.pending...)
	}
	s.pending = nil
	s.appendUnfedStopTail(fullPCM)
	if len(s.active) > 0 {
		segmentPCM := append([]byte(nil), s.active...)
		if len(s.tailSilence) > 0 {
			segmentPCM = append(segmentPCM, s.tailSilence...)
		}
		if segment := s.buildSegment(segmentPCM, true); segment != nil {
			flushed = append(flushed, *segment)
		}
	}

	segments := cloneAudioSegments(s.segments)
	segments = append(segments, flushed...)
	s.segments = nil

	s.detector.Reset()
	s.resetSession()

	if len(segments) == 0 {
		if emittedAny {
			return nil, nil
		}
		return FallbackDictationSegments(fullPCM), nil
	}
	return segments, nil
}

func (s *DictationSegmenter) appendUnfedStopTail(fullPCM []byte) {
	if len(fullPCM) <= s.ingestedBytes {
		return
	}
	tail := fullPCM[s.ingestedBytes:]
	if len(tail) == 0 {
		return
	}

	switch {
	case s.inSpeech || len(s.active) > 0:
		s.active = append(s.active, tail...)
	case len(s.pending) > 0:
		s.pending = append(s.pending, tail...)
	case len(s.segments) == 0:
		s.pending = append(s.pending, tail...)
	}
}

// FallbackDictationSegments wraps all of fullPCM in a single segment.
// Used when VAD-based segmentation is unavailable or produces no output.
func FallbackDictationSegments(fullPCM []byte) []AudioSegment {
	if len(fullPCM) == 0 {
		return nil
	}

	return []AudioSegment{{
		PCM:      append([]byte(nil), fullPCM...),
		Duration: time.Duration(PCMDurationSecs(fullPCM) * float64(time.Second)),
		Final:    true,
	}}
}

func cloneAudioSegments(segments []AudioSegment) []AudioSegment {
	if len(segments) == 0 {
		return nil
	}
	out := make([]AudioSegment, 0, len(segments))
	for _, segment := range segments {
		clone := segment
		clone.PCM = append([]byte(nil), segment.PCM...)
		out = append(out, clone)
	}
	return out
}

func (s *DictationSegmenter) feedFrame(frame []byte) ([]AudioSegment, error) {
	samples := make([]int16, dictationFrameSize)
	for i := 0; i < dictationFrameSize; i++ {
		offset := i * AudioBytesPerSample
		samples[i] = int16(binary.LittleEndian.Uint16(frame[offset : offset+AudioBytesPerSample])) // #nosec G115 -- PCM S16LE decoding reinterprets identical-width sample bits.
	}

	prob, err := s.detector.ProcessFrame(samples)
	if err != nil {
		return nil, fmt.Errorf("vad frame: %w", err)
	}

	speaking := prob > dictationSpeechThreshold
	frameDur := dictationFrameDuration()

	if speaking {
		if !s.inSpeech {
			if len(s.active) == 0 {
				s.activeParagraph = s.emittedAny && s.nextParagraph
			}
			s.nextParagraph = false
			s.idleSilenceTime = 0
			s.inSpeech = true
			s.silenceTime = 0
			// Clear idleSince so the poller does not see stale silence
			// while the user is mid-utterance.
			s.idleSince = time.Time{}
			if len(s.preRoll) > 0 {
				s.active = append(s.active, s.preRoll...)
				s.preRoll = nil
			}
		}
		if len(s.tailSilence) > 0 {
			s.active = append(s.active, s.tailSilence...)
			s.tailSilence = nil
		}
		s.active = append(s.active, frame...)
		s.silenceTime = 0
		s.idleSilenceTime = 0
		return nil, nil
	}

	if !s.inSpeech {
		s.addIdleSilence(frameDur)
		s.appendPreRoll(frame)
		return nil, nil
	}

	s.silenceTime += frameDur
	s.appendTailSilence(frame)
	if s.silenceTime < s.pause {
		return nil, nil
	}

	segmentPCM := s.active
	if len(s.tailSilence) > 0 {
		segmentPCM = append(segmentPCM, s.tailSilence...)
	}

	if !s.shouldEmitIntermediate(segmentPCM) {
		s.active = segmentPCM
		s.tailSilence = nil
		s.inSpeech = false
		s.idleSince = s.now()
		s.setIdleSilence(s.silenceTime)
		s.silenceTime = 0
		return nil, nil
	}

	segment := s.buildSegment(segmentPCM, false)

	s.active = nil
	s.tailSilence = nil
	s.inSpeech = false
	// User stopped speaking — anchor the idle clock to "now" so the
	// post-utterance silence countdown starts here, not from session
	// start.
	s.idleSince = s.now()
	s.setIdleSilence(s.silenceTime)
	s.silenceTime = 0

	if segment == nil {
		s.activeParagraph = false
		return nil, nil
	}
	return []AudioSegment{*segment}, nil
}

func (s *DictationSegmenter) addIdleSilence(d time.Duration) {
	s.setIdleSilence(s.idleSilenceTime + d)
}

func (s *DictationSegmenter) setIdleSilence(d time.Duration) {
	if d < 0 {
		d = 0
	}
	s.idleSilenceTime = d
	if s.emittedAny && len(s.active) == 0 && s.paragraphPause > 0 && d >= s.paragraphPause {
		s.nextParagraph = true
	}
}

func (s *DictationSegmenter) shouldEmitIntermediate(pcm []byte) bool {
	if s.minIntermediateSegment <= 0 {
		return true
	}
	return dictationDuration(pcm) >= s.minIntermediateSegment
}

func (s *DictationSegmenter) buildSegment(pcm []byte, final bool) *AudioSegment {
	if len(pcm) == 0 {
		return nil
	}

	duration := dictationDuration(pcm)
	if duration < s.minSegment {
		return nil
	}

	segment := &AudioSegment{
		PCM:       append([]byte(nil), pcm...),
		Duration:  duration,
		Paragraph: s.emittedAny && s.activeParagraph,
		Final:     final,
	}

	s.emittedAny = true
	s.activeParagraph = false
	s.nextParagraph = false
	s.appendOverlapTail(pcm)
	return segment
}

func (s *DictationSegmenter) appendPreRoll(frame []byte) {
	if limit := maxDictationBytes(s.padding, s.overlap); limit > 0 {
		s.preRoll = append(s.preRoll, frame...)
		s.preRoll = trimLeftDictationBytes(s.preRoll, limit)
	}
}

func (s *DictationSegmenter) appendOverlapTail(pcm []byte) {
	limit := dictationBytesForDuration(s.overlap)
	if limit <= 0 {
		s.preRoll = nil
		return
	}
	s.preRoll = append(s.preRoll[:0], tailDictationBytes(pcm, limit)...)
}

func (s *DictationSegmenter) appendTailSilence(frame []byte) {
	limit := dictationBytesForDuration(s.padding)
	if limit <= 0 {
		s.tailSilence = nil
		return
	}
	s.tailSilence = append(s.tailSilence, frame...)
	s.tailSilence = trimLeftDictationBytes(s.tailSilence, limit)
}

func (s *DictationSegmenter) resetSession() {
	s.pending = nil
	s.preRoll = nil
	s.active = nil
	s.tailSilence = nil
	s.inSpeech = false
	s.silenceTime = 0
	s.idleSilenceTime = 0
	s.activeParagraph = false
	s.nextParagraph = false
	s.emittedAny = false
	s.ingestedBytes = 0
	// A new dictate session starts idle relative to "now" so the
	// silence-timeout watcher gives the user a full window before
	// auto-stopping.
	s.idleSince = s.now()
}

func dictationFrameDuration() time.Duration {
	return time.Duration(dictationFrameSize) * time.Second / AudioSampleRate
}

func dictationDuration(pcm []byte) time.Duration {
	return time.Duration(len(pcm)) * time.Second / (AudioSampleRate * AudioBytesPerSample)
}

func dictationBytesForDuration(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d * time.Duration(AudioSampleRate*AudioBytesPerSample) / time.Second)
}

func maxDictationBytes(a, b time.Duration) int {
	if a >= b {
		return dictationBytesForDuration(a)
	}
	return dictationBytesForDuration(b)
}

func trimLeftDictationBytes(buf []byte, limit int) []byte {
	if limit <= 0 || len(buf) <= limit {
		return append([]byte(nil), buf...)
	}
	return append([]byte(nil), buf[len(buf)-limit:]...)
}

func tailDictationBytes(buf []byte, limit int) []byte {
	if limit <= 0 || len(buf) <= limit {
		return append([]byte(nil), buf...)
	}
	return append([]byte(nil), buf[len(buf)-limit:]...)
}
