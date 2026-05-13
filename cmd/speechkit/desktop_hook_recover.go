package main

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// recoverHook intercepts a panic inside a Wails event-loop callback so a
// programming error in one hook does not crash the entire event loop
// and take the desktop client down. The panic is recorded in slog with
// hook name and stack trace so it is diagnosable from the field log.
//
// Use as `defer recoverHook("hook_name")` at the start of any callback
// registered via app.Event.On / OnApplicationEvent / window.RegisterHook
// / window.OnWindowEvent.
func recoverHook(hookName string) {
	if r := recover(); r != nil {
		slog.Error("desktop.hook.panic",
			"hook", hookName,
			"panic", fmt.Sprintf("%v", r),
			"stack", string(debug.Stack()))
	}
}
