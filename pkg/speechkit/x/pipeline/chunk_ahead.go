package pipeline

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"

	speechkitaudio "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

// Chunk-ahead transcription. A dictation that is inserted once at the end used
// to be transcribed in one piece after Stop, so the wait grew with the length of
// the recording (and, on a busy machine, with everything else the machine did).
// With RecordingStartOptions.ChunkAhead the controller cuts the recording at
// natural pauses while it is made, transcribes each finished chunk in the
// background, and at Stop transcribes only the audio after the last cut. The
// job then carries one segment per chunk whose transcript is already there (see
// speechkit.Submission.Pretranscribed) plus the tail, and the worker stitches
// them exactly like the segments of a fragmented recording: one space between
// the parts, no overlap, so no word is repeated or dropped at a cut.
//
// Cuts fall in the middle of a pause of at least chunkPauseFrames frames once a
// chunk is chunkMinSecs long; a chunk that finds no pause by chunkMaxSecs is cut
// at its quietest frame in the last chunkHardWindowSecs.
const (
	chunkMinSecs        = 20
	chunkMaxSecs        = 30
	chunkHardWindowSecs = 8
	chunkFrameMs        = 50
	chunkPauseFrames    = 5 // 250 ms of quiet
	chunkFrameBytes     = chunkFrameMs * pcmBytesPerMs
	chunkMinFrames      = chunkMinSecs * 1000 / chunkFrameMs
	chunkMaxFrames      = chunkMaxSecs * 1000 / chunkFrameMs
	chunkHardFrames     = chunkHardWindowSecs * 1000 / chunkFrameMs
	chunkAttemptBase    = 60 * time.Second
	// chunkMinTailSecs is the shortest tail worth a provider call; a shorter
	// one holds only the silence the last cut left.
	chunkMinTailSecs = 0.2
)

// SetChunkTranscriber sets the transcriber the chunk-ahead of a recording with
// ChunkAhead uses: the one that transcribes the recording's job.
func (c *RecordingController) SetChunkTranscriber(t speechkit.Transcriber) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chunkTranscriber = t
}

type aheadChunk struct {
	index int
	secs  float64

	mu    sync.Mutex
	pcm   []byte // kept until the background attempt succeeded, for a retry at Stop
	ready chan struct{}
	text  speechkit.Transcript
	err   error
}

type chunkAhead struct {
	tr       speechkit.Transcriber
	language string
	log      func(string, string)

	ctx    context.Context
	cancel context.CancelFunc
	work   chan *aheadChunk

	mu         sync.Mutex
	closed     bool
	handedOff  bool
	pending    []byte
	frames     []float64
	frameCount int // frames of pending already measured
	quietRun   int
	levelSum   float64
	levelN     int
	cutBytes   int
	chunks     []*aheadChunk
}

func newChunkAhead(tr speechkit.Transcriber, language string, log func(string, string)) *chunkAhead {
	ctx, cancel := context.WithCancel(context.Background())
	a := &chunkAhead{tr: tr, language: language, log: log, ctx: ctx, cancel: cancel, work: make(chan *aheadChunk, 64)}
	go a.loop()
	return a
}

// feed adds captured audio and cuts finished chunks off it.
func (a *chunkAhead) feed(pcm []byte) {
	if a == nil || len(pcm) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.pending = append(a.pending, pcm...)
	for (a.frameCount+1)*chunkFrameBytes <= len(a.pending) {
		frame := a.pending[a.frameCount*chunkFrameBytes : (a.frameCount+1)*chunkFrameBytes]
		a.frames = append(a.frames, speechkitaudio.PCMLevel(frame))
		a.frameCount++
		idx := a.frameCount - 1
		level := a.frames[idx]
		a.levelSum += level
		a.levelN++
		if a.frameCount < chunkMinFrames {
			continue
		}
		if level < a.quietThreshold() {
			a.quietRun++
		} else {
			a.quietRun = 0
		}
		switch {
		case a.quietRun >= chunkPauseFrames:
			a.cutLocked(idx - a.quietRun/2)
		case a.frameCount >= chunkMaxFrames:
			a.cutLocked(a.quietestFrameLocked())
		}
	}
}

// quietThreshold is the level below which a frame counts as a pause: a third
// of the recording's mean level, between a floor above room noise and a ceiling
// below quiet speech.
func (a *chunkAhead) quietThreshold() float64 {
	mean := 0.0
	if a.levelN > 0 {
		mean = a.levelSum / float64(a.levelN)
	}
	return math.Min(0.03, math.Max(0.004, mean/3))
}

func (a *chunkAhead) quietestFrameLocked() int {
	best, bestLevel := a.frameCount-1, math.MaxFloat64
	for i := a.frameCount - 1; i >= a.frameCount-chunkHardFrames && i >= chunkMinFrames; i-- {
		if a.frames[i] < bestLevel {
			best, bestLevel = i, a.frames[i]
		}
	}
	return best
}

// cutLocked ends a chunk after frame index idx of the pending audio.
func (a *chunkAhead) cutLocked(idx int) {
	end := (idx + 1) * chunkFrameBytes
	if idx < 0 || end > len(a.pending) {
		return
	}
	chunk := &aheadChunk{
		index: len(a.chunks) + 1,
		secs:  float64(end/pcmBytesPerMs) / 1000,
		pcm:   append([]byte(nil), a.pending[:end]...),
		ready: make(chan struct{}),
	}
	a.cutBytes += end
	a.pending = append([]byte(nil), a.pending[end:]...)
	a.frames = append([]float64(nil), a.frames[idx+1:]...)
	a.frameCount -= idx + 1
	a.quietRun = 0
	a.chunks = append(a.chunks, chunk)
	select {
	case a.work <- chunk:
	default:
		// The queue is full (a provider that stalls for many minutes): the
		// chunk is transcribed at Stop like a failed one.
		a.fail(chunk, fmt.Errorf("chunk queue full"))
	}
}

// loop transcribes the chunks one after the other: a local engine decodes one
// recording at a time anyway, and a cloud route is not asked for many parallel
// calls of one dictation.
func (a *chunkAhead) loop() {
	for chunk := range a.work {
		if a.ctx.Err() != nil {
			a.fail(chunk, a.ctx.Err())
			continue
		}
		started := time.Now()
		ctx, cancel := context.WithTimeout(a.ctx, chunkAttemptBase+time.Duration(chunk.secs*float64(time.Second))*2)
		transcript, err := a.tr.Transcribe(ctx, speechkit.PCMToWAV(chunk.pcm), chunk.secs, a.language)
		cancel()
		if err != nil {
			a.fail(chunk, err)
			a.log(fmt.Sprintf("Chunk-ahead: chunk %d (%.1fs) failed in the background, it is transcribed at Stop: %v", chunk.index, chunk.secs, err), "warn")
			continue
		}
		chunk.mu.Lock()
		chunk.text, chunk.pcm = transcript, nil
		chunk.mu.Unlock()
		close(chunk.ready)
		a.log(fmt.Sprintf("Chunk-ahead: chunk %d (%.1fs audio) transcribed in the background in %dms", chunk.index, chunk.secs, time.Since(started).Milliseconds()), "info")
	}
}

func (a *chunkAhead) fail(chunk *aheadChunk, err error) {
	chunk.mu.Lock()
	chunk.err = err
	chunk.mu.Unlock()
	close(chunk.ready)
}

// result is the chunk's transcript: the background one, or, when that attempt
// failed, one transcription of the chunk's audio here.
func (a *chunkAhead) result(chunk *aheadChunk) func(context.Context) (speechkit.Transcript, error) {
	return func(ctx context.Context) (speechkit.Transcript, error) {
		select {
		case <-chunk.ready:
		case <-ctx.Done():
			return speechkit.Transcript{}, ctx.Err()
		}
		chunk.mu.Lock()
		text, pcm, err := chunk.text, chunk.pcm, chunk.err
		chunk.mu.Unlock()
		if err == nil {
			return text, nil
		}
		return a.tr.Transcribe(ctx, speechkit.PCMToWAV(pcm), chunk.secs, a.language)
	}
}

// discard drops the chunk-ahead of a recording nobody transcribes (cancelled,
// too short, handled by a provider stream). Chunks already handed to a job keep
// running.
func (a *chunkAhead) discard() {
	if a == nil {
		return
	}
	a.mu.Lock()
	handed := a.handedOff
	if !a.closed {
		a.closed = true
		if !handed {
			close(a.work)
		}
	}
	a.mu.Unlock()
	if !handed {
		a.cancel()
	}
}

// finish ends the chunk-ahead at Stop and returns the segments of the job: one
// per cut chunk (already transcribed or being transcribed) and the tail of the
// recording after the last cut. ok is false, and the recording is transcribed
// whole as before, when no chunk was cut or the audio the handler saw is not
// the recorder's complete capture.
func (a *chunkAhead) finish(fullPCM []byte, capturedBytes int, language string) (segments []speechkit.Submission, ok bool) {
	if a == nil {
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || len(a.chunks) == 0 || capturedBytes != len(fullPCM) || a.cutBytes > len(fullPCM) {
		return nil, false
	}
	a.closed, a.handedOff = true, true
	close(a.work)
	for _, chunk := range a.chunks {
		segments = append(segments, speechkit.Submission{
			DurationSecs:   chunk.secs,
			Language:       language,
			Pretranscribed: a.result(chunk),
		})
	}
	tail := fullPCM[a.cutBytes:]
	if secs := speechkit.PCMDurationSecs(tail); secs >= chunkMinTailSecs && speechkitaudio.PCMLevel(tail) >= noiseFloorRMS {
		segments = append(segments, speechkit.Submission{
			PCM:          tail,
			WAV:          speechkit.PCMToWAV(tail),
			DurationSecs: secs,
			Language:     language,
		})
	}
	return segments, true
}

func (a *chunkAhead) chunkCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.chunks)
}

// chunkedSecs is the audio the pre-transcribed segments of a job cover.
func chunkedSecs(segments []speechkit.Submission) float64 {
	var total float64
	for _, segment := range segments {
		if segment.Pretranscribed != nil {
			total += segment.DurationSecs
		}
	}
	return total
}
