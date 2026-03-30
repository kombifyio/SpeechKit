package speechkit

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/dictation"
)

const DefaultMinPCMBytes = 3200

type AudioRecorder interface {
	Start() error
	Stop() ([]byte, error)
	SetPCMHandler(func([]byte))
}

type SegmentCollector interface {
	FeedPCM([]byte) error
	CollectStopSegments(fullPCM []byte) ([]dictation.Segment, error)
}

type SegmentCollectorFactory func() SegmentCollector

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

type RecordingController struct {
	recorder         AudioRecorder
	submitter        JobSubmitter
	observer         RecordingObserver
	segmenterFactory SegmentCollectorFactory
	recordingMessage string
	minPCMBytes      int

	recording bool
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
	return c != nil && c.recording
}

func (c *RecordingController) Start(opts RecordingStartOptions) error {
	if c == nil {
		return fmt.Errorf("speechkit: recording controller not configured")
	}
	c.recording = true
	c.current = opts
	c.collector = nil

	if c.segmenterFactory != nil {
		c.collector = c.segmenterFactory()
	}
	if c.collector != nil {
		c.recorder.SetPCMHandler(func(pcm []byte) {
			if err := c.collector.FeedPCM(pcm); err != nil {
				c.onLog(fmt.Sprintf("Dictation processor fallback: %v", err), "warn")
				c.recorder.SetPCMHandler(nil)
				c.collector = nil
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
		c.recording = false
		c.collector = nil
		c.recorder.SetPCMHandler(nil)
		c.onLog(fmt.Sprintf("Capture error: %v", err), "error")
		c.onState("idle", "")
		return err
	}

	return nil
}

func (c *RecordingController) Stop(opts RecordingStopOptions) error {
	if c == nil || !c.recording {
		return nil
	}

	c.recording = false
	c.recorder.SetPCMHandler(nil)
	pcm, stopErr := c.recorder.Stop()
	if stopErr != nil {
		c.onLog(fmt.Sprintf("Capture stop warning: %v", stopErr), "warn")
	}

	dur := audio.PCMDurationSecs(pcm)
	c.onLog(fmt.Sprintf("%s: %.1fs audio", opts.Label, dur), "info")

	if len(pcm) < c.minPCMBytes {
		c.onLog("Too short, skipped", "error")
		c.onState("idle", "")
		c.collector = nil
		return nil
	}

	segments := FallbackDictationSegments(pcm)
	if c.collector != nil {
		collected, err := c.collector.CollectStopSegments(pcm)
		if err != nil {
			c.onLog(fmt.Sprintf("Dictation processor fallback: %v", err), "warn")
		} else {
			segments = collected
		}
	}
	c.collector = nil

	for _, segment := range segments {
		prefix := ""
		if segment.Paragraph {
			prefix = "\n\n"
		}

		if err := c.submitter.Submit(TranscriptionJob{
			Submission: Submission{
				PCM:          segment.PCM,
				WAV:          audio.PCMToWAV(segment.PCM),
				DurationSecs: audio.PCMDurationSecs(segment.PCM),
				Language:     c.current.Language,
				Prefix:       prefix,
				QuickNote:    c.current.QuickNote,
				QuickNoteID:  c.current.QuickNoteID,
			},
			Target: c.current.Target,
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
