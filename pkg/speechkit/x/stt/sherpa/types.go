package sherpa

// Config configures engine.NewRecognizer.
type Config struct {
	// ModelDir is the absolute path of a nemo_transducer model directory
	// (see [ResolveModelDir]).
	ModelDir string
	// Threads is the number of CPU threads the model runs on; values below 1
	// use one thread.
	Threads int
}

// Result is the transcript of one recording.
type Result struct {
	Text string
	// Segments are the spans the recording was decoded in, in order, with
	// their times in seconds. A recording that fits one pass has one.
	Segments []Segment
	// Duration is the audio length in seconds.
	Duration float64
}

// Segment is the transcript of one decoded span.
type Segment struct {
	Start float64
	End   float64
	Text  string
}
