// Package pipeline is the composable capture-to-transcript engine of
// SpeechKit. It implements the contracts declared in the root speechkit
// package — [speechkit.AudioRecorder], [speechkit.SegmentCollector],
// [speechkit.Transcriber], [speechkit.TranscriptOutput] — and wires them into
// three reusable building blocks:
//
//   - [RecordingController] drives one capture session: start/stop, idle
//     auto-stop, segment collection and submission of [speechkit.TranscriptionJob]
//     values to a [speechkit.JobSubmitter].
//   - [TranscriptionWorker] consumes jobs on a bounded queue, runs them through
//     a [TranscriptionRunner] (provider call, persistence, low-confidence
//     tagging, live-commit flushing) and delivers the resulting
//     [speechkit.Transcript] to the host's output.
//   - [DictationSegmenter] and [TranscriptSessionLedger] split long captures
//     into ordered segments and keep multi-channel sessions (microphone plus
//     system audio) attributable.
//
// Hosts that only need the ready-made dictation flow should use
// [github.com/kombifyio/SpeechKit/pkg/speechkit/dictation]; this
// package is for hosts that compose their own pipeline or replace one stage.
package pipeline
