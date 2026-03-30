package output

import (
	"context"

	"golang.org/x/sys/windows"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

// Target stores the intended destination window for paste output.
type Target struct {
	HWND windows.Handle
}

// OutputHandler defines how transcribed text is delivered to the user.
type OutputHandler interface {
	Handle(ctx context.Context, result *stt.Result, target Target) error
}
