package flows

import "context"

// Flow is SpeechKit's small typed execution wrapper. It replaces the prior
// framework-specific flow type while preserving the public Run boundary used
// by Assist, summaries, and the Voice Agent.
type Flow[Input, Output any] struct {
	run func(context.Context, Input) (Output, error)
}

func New[Input, Output any](run func(context.Context, Input) (Output, error)) *Flow[Input, Output] {
	return &Flow[Input, Output]{run: run}
}

func (f *Flow[Input, Output]) Run(ctx context.Context, input Input) (Output, error) {
	var zero Output
	if f == nil || f.run == nil {
		return zero, ErrNotConfigured
	}
	return f.run(ctx, input)
}
