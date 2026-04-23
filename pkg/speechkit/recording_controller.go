package speechkit

import (
	"fmt"
	"sync"
)

const DefaultMinPCMBytes = 3200

// AudioRecorder is the hardware abstraction for microphone capture.
type AudioRecorder interface {
	Start() error
	Stop() ([]byte, error)
	SetPCMHandler(func([]byte))
}

// SegmentCollector accumulates real-time PCM frames and splits them into
// dictation segments when recording stops.
type SegmentCollector interface {
	FeedPCM([]byte) error
	CollectStopSegments(fullPCM []byte) ([]AudioSegment, error)
}

type SegmentCollectorFactory func() SegmentCollector

// JobSubmitter accepts a [TranscriptionJob] for async processing.
type JobSubmitter interface {
	Submit(TranscriptionJob) error
}

type RecordingObserver interface {
	OnState(status, text string)
	OnLog(message, kind string)
}

type RecordingStartOptions struct {
	Label       string
	Target      any
	Language    string
	QuickNote   bool
	QuickNoteID int64
}

type RecordingStopOptions struct {
	Label string
}

// RecordingController manages the start/stop lifecycle of a single recording
// session and hands audio segments to the submission queue.
type RecordingController struct {
	recorder         AudioRecorder
	submitter        JobSubmitter
	observer         RecordingObserver
	segmenterFactory SegmentCollectorFactory
	recordingMessage string
	minPCMBytes      int

	mu        sync.Mutex
	recording bool
	sessionID uint64
	current   RecordingStartOptions
	collector SegmentCollector
}

func NewRecordingController(recorder AudioRecorder, submitter JobSubmitter, observer RecordingObserver, segmenterFactory SegmentCollectorFactory) *RecordingController {
	return &RecordingController{
		recorder:         recorder,
		submitter:        submitter,
		observer:         observer,
		segmenterFactory: segmenterFactory,
		recordingMessage: "Speak now",
		minPCMBytes:      DefaultMinPCMBytes,
	}
}

func (c *RecordingController) IsRecording() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recording
}

func (c *RecordingController) Start(opts RecordingStartOptions) error {
	if c == nil {
		return fmt.Errorf("speechkit: recording controller not configured")
	}

	var (
		collector SegmentCollector
		sessionID uint64
	)

	c.mu.Lock()
	c.recording = true
	c.current = opts
	c.collector = nil
	c.sessionID++
	sessionID = c.sessionID

	if c.segmenterFactory != nil {
		collector = c.segmenterFactory()
		c.collector = collector
	}
	c.mu.Unlock()

	if collector != nil {
		c.recorder.SetPCMHandler(func(pcm []byte) {
			c.mu.Lock()
			if c.sessionID != sessionID || !c.recording {
				c.mu.Unlock()
				return
			}
			activeCollector := c.collector
			c.mu.Unlock()
			if activeCollector == nil {
				return
			}
			if err := activeCollector.FeedPCM(pcm); err != nil {
				c.onLog(fmt.Sprintf("Dictation processor fallback: %v", err), "warn")
				c.mu.Lock()
				if c.sessionID == sessionID {
					c.collector = nil
				}
				c.mu.Unlock()
				c.recorder.SetPCMHandler(nil)
			}
		})
	} else {
		c.recorder.SetPCMHandler(nil)
	}

	c.onState("recording", c.recordingMessage)
	if opts.Label != "" {
		c.onLog(opts.Label, "info")
	}

	if err := c.recorder.Start(); err != nil {
		c.mu.Lock()
		if c.sessionID == sessionID {
			c.recording = false
			c.collector = nil
		}
		c.mu.Unlock()
		c.recorder.SetPCMHandler(nil)
		c.onLog(fmt.Sprintf("Capture error: %v", err), "error")
		c.onState("idle", "")
		return err
	}

	return nil
}

func (c *RecordingController) Stop(opts RecordingStopOptions) error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	if !c.recording {
		c.mu.Unlock()
		return nil
	}
	c.recording = false
	current := c.current
	collector := c.collector
	c.collector = nil
	c.mu.Unlock()

	c.recorder.SetPCMHandler(nil)
	pcm, stopErr := c.recorder.Stop()
	if stopErr != nil {
		c.onLog(fmt.Sprintf("Capture stop warning: %v", stopErr), "warn")
	}

	dur := PCMDurationSecs(pcm)
	c.onLog(fmt.Sprintf("%s: %.1fs audio", opts.Label, dur), "info")

	if len(pcm) < c.minPCMBytes {
		c.onLog("Too short, skipped", "error")
		c.onState("idle", "")
		c.collector = nil
		return nil
	}

	segments := FallbackDictationSegments(pcm)
	if collector != nil {
		collected, err := collector.CollectStopSegments(pcm)
		if err != nil {
			c.onLog(fmt.Sprintf("Dictation processor fallback: %v", err), "warn")
		} else {
			segments = collected
		}
	}

	for _, segment := range segments {
		prefix := ""
		if segment.Paragraph {
			prefix = "\n\n"
		}

		if err := c.submitter.Submit(TranscriptionJob{
			Submission: Submission{
				PCM:          segment.PCM,
				WAV:          PCMToWAV(segment.PCM),
				DurationSecs: PCMDurationSecs(segment.PCM),
				Language:     current.Language,
				Prefix:       prefix,
				QuickNote:    current.QuickNote,
				QuickNoteID:  current.QuickNoteID,
			},
			Target: current.Target,
		}); err != nil {
			c.onLog(fmt.Sprintf("Queue error: %v", err), "error")
			c.onState("idle", "")
			return err
		}
	}

	return nil
}

func (c *RecordingController) onState(status, text string) {
	if c.observer != nil {
		c.observer.OnState(status, text)
	}
}

func (c *RecordingController) onLog(message, kind string) {
	if c.observer != nil {
		c.observer.OnLog(message, kind)
	}
}
