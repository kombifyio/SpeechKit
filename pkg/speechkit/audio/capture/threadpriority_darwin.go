//go:build darwin

package capture

// threadPriorityAboveNormal mirrors the Windows constant so the shared
// drain goroutine (capture_malgo_cgo.go) compiles unchanged on macOS.
const threadPriorityAboveNormal = 1

// setCurrentThreadPriority is a no-op on macOS for now: the drain thread
// runs at the default QoS. Raising it to QOS_CLASS_USER_INTERACTIVE with
// pthread_set_qos_class_self_np is the thread-QoS follow-up of the macOS
// port (kombify-SpeechKit-mcos epic; not its own bead yet) and needs no
// cgo in this package until then.
func setCurrentThreadPriority(int32) error {
	return nil
}
