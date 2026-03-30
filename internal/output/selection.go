package output

import (
	"context"
	"runtime"
	"strings"
	"time"
)

var (
	selectionGetClipboardText = getClipboardText
	selectionSetClipboardText = setClipboardText
	selectionTriggerCopy      = simulateCtrlC
	selectionPause            = time.Sleep
)

// CaptureSelectedText copies the current selection, reads clipboard text, and restores the backup.
func CaptureSelectedText(ctx context.Context) (string, error) {
	_ = ctx

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	backup, hasBackup := selectionGetClipboardText()
	selectionTriggerCopy()
	selectionPause(80 * time.Millisecond)

	selected, _ := selectionGetClipboardText()
	selected = strings.TrimSpace(selected)

	if hasBackup {
		_ = selectionSetClipboardText(backup)
	}

	return selected, nil
}
