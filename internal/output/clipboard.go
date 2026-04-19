package output

import (
	"context"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/winapi"
)

const (
	cfUnicodeText  = 13
	gmemMoveable   = 0x0002
	keyEventFKeyUp = 0x0002
	vkControl      = 0x11
	vkC            = 0x43
	vkV            = 0x56
	restoreDelay   = 150 * time.Millisecond
	swRestore      = 9
)

var (
	procOpenClipboard    = winapi.User32.NewProc("OpenClipboard")
	procCloseClipboard   = winapi.User32.NewProc("CloseClipboard")
	procEmptyClipboard   = winapi.User32.NewProc("EmptyClipboard")
	procSetClipboardData = winapi.User32.NewProc("SetClipboardData")
	procGetClipboardData = winapi.User32.NewProc("GetClipboardData")
	procGetForeground    = winapi.User32.NewProc("GetForegroundWindow")
	procSetForeground    = winapi.User32.NewProc("SetForegroundWindow")
	procBringWindowTop   = winapi.User32.NewProc("BringWindowToTop")
	procSetFocus         = winapi.User32.NewProc("SetFocus")
	procShowWindow       = winapi.User32.NewProc("ShowWindow")
	procAttachThread     = winapi.User32.NewProc("AttachThreadInput")
	procWindowThreadPID  = winapi.User32.NewProc("GetWindowThreadProcessId")

	procGlobalAlloc  = winapi.Kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = winapi.Kernel32.NewProc("GlobalLock")
	procGlobalUnlock = winapi.Kernel32.NewProc("GlobalUnlock")
	procGlobalFree   = winapi.Kernel32.NewProc("GlobalFree")

	procKeybdEvent    = winapi.User32.NewProc("keybd_event")
	procRtlMoveMemory = winapi.RtlMoveMemory
	procLstrlenW      = winapi.Kernel32.NewProc("lstrlenW")
)

// ClipboardHandler injects text via clipboard + Ctrl+V simulation.
type ClipboardHandler struct{}

func NewClipboardHandler() *ClipboardHandler {
	return &ClipboardHandler{}
}

func CaptureTarget() Target {
	return Target{HWND: currentForegroundWindow()}
}

func (h *ClipboardHandler) Handle(ctx context.Context, result *stt.Result, target Target) error {
	if result == nil || result.Text == "" {
		return nil
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	backup, hasBackup := getClipboardText()

	if err := setClipboardText(result.Text); err != nil {
		return fmt.Errorf("set clipboard: %w", err)
	}

	restoreForegroundWindow(choosePasteWindow(target.HWND, currentForegroundWindow()))
	simulateCtrlV()

	// Wait for the target app to consume the Ctrl+V keystroke before restoring
	// the previous clipboard content. Done inline (same OS-thread) to avoid
	// a background goroutine race where a user's subsequent copy gets clobbered.
	if hasBackup {
		time.Sleep(restoreDelay)
		_ = setClipboardText(backup)
	}

	return nil
}

func choosePasteWindow(target, fallback windows.Handle) windows.Handle {
	if target != 0 {
		return target
	}
	return fallback
}

func currentForegroundWindow() windows.Handle {
	hwnd, _, _ := procGetForeground.Call()
	return windows.Handle(hwnd)
}

func restoreForegroundWindow(hwnd windows.Handle) {
	if hwnd == 0 {
		return
	}

	current := currentForegroundWindow()
	if current == hwnd {
		return
	}

	currentThread := windows.GetCurrentThreadId()
	targetThread := windowThread(hwnd)
	foregroundThread := windowThread(current)

	attachInput(currentThread, targetThread, true)
	defer attachInput(currentThread, targetThread, false)

	attachInput(foregroundThread, targetThread, true)
	defer attachInput(foregroundThread, targetThread, false)

	procShowWindow.Call(uintptr(hwnd), uintptr(swRestore)) //nolint:errcheck // Windows API call, return value not meaningful
	procBringWindowTop.Call(uintptr(hwnd))                 //nolint:errcheck // Windows API call, return value not meaningful
	procSetForeground.Call(uintptr(hwnd))                  //nolint:errcheck // Windows API call, return value not meaningful
	procSetFocus.Call(uintptr(hwnd))                       //nolint:errcheck // Windows API call, return value not meaningful
}

func windowThread(hwnd windows.Handle) uint32 {
	if hwnd == 0 {
		return 0
	}
	threadID, _, _ := procWindowThreadPID.Call(uintptr(hwnd), 0)
	return uint32(threadID) //nolint:gosec // G115: Windows API thread ID fits uint32
}

func attachInput(fromThread, toThread uint32, attach bool) {
	if fromThread == 0 || toThread == 0 || fromThread == toThread {
		return
	}
	var flag uintptr
	if attach {
		flag = 1
	}
	procAttachThread.Call(uintptr(fromThread), uintptr(toThread), flag) //nolint:errcheck // Windows API call, return value not meaningful
}

func openClipboard() error {
	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return fmt.Errorf("OpenClipboard: %w", err)
	}
	return nil
}

func closeClipboard() {
	procCloseClipboard.Call() //nolint:errcheck // Windows API call, return value not meaningful
}

func getClipboardText() (string, bool) {
	if err := openClipboard(); err != nil {
		return "", false
	}
	defer closeClipboard()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}

	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h) //nolint:errcheck // Windows API call, return value not meaningful

	// Get string length via lstrlenW (avoids uintptr->unsafe.Pointer conversion)
	length, _, _ := procLstrlenW.Call(ptr)
	if length == 0 {
		return "", true
	}

	// Copy into Go-allocated buffer via RtlMoveMemory
	buf := make([]uint16, length+1)
	procRtlMoveMemory.Call( //nolint:errcheck // Windows API call, return value not meaningful
		uintptr(unsafe.Pointer(&buf[0])), //nolint:gosec // Windows API requires unsafe.Pointer
		ptr,
		(length+1)*2,
	)
	return windows.UTF16ToString(buf[:length]), true
}

func setClipboardText(text string) error {
	if err := openClipboard(); err != nil {
		return err
	}
	defer closeClipboard()

	procEmptyClipboard.Call() //nolint:errcheck // Windows API call, return value not meaningful

	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("utf16 convert: %w", err)
	}

	size := uintptr(len(utf16) * 2)
	hMem, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}

	ptr, _, err := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem) //nolint:errcheck // Windows API call, return value not meaningful
		return fmt.Errorf("GlobalLock: %w", err)
	}

	// Copy UTF-16 data via RtlMoveMemory (avoids uintptr->unsafe.Pointer conversion)
	procRtlMoveMemory.Call( //nolint:errcheck // Windows API call, return value not meaningful
		ptr,
		uintptr(unsafe.Pointer(&utf16[0])), //nolint:gosec // Windows API requires unsafe.Pointer
		size,
	)

	procGlobalUnlock.Call(hMem) //nolint:errcheck // Windows API call, return value not meaningful

	r, _, err := procSetClipboardData.Call(cfUnicodeText, hMem)
	if r == 0 {
		procGlobalFree.Call(hMem) //nolint:errcheck // Windows API call, return value not meaningful
		return fmt.Errorf("SetClipboardData: %w", err)
	}

	return nil
}

// SetClipboardText replaces the current clipboard content with the provided text.
func SetClipboardText(text string) error {
	return setClipboardText(text)
}

func simulateCtrlV() {
	simulateCtrlChord(vkV)
}

func simulateCtrlC() {
	simulateCtrlChord(vkC)
}

func simulateCtrlChord(vk uint8) {
	procKeybdEvent.Call(vkControl, 0, 0, 0) //nolint:errcheck // Windows API call, return value not meaningful
	time.Sleep(2 * time.Millisecond)
	procKeybdEvent.Call(uintptr(vk), 0, 0, 0) //nolint:errcheck // Windows API call, return value not meaningful
	time.Sleep(2 * time.Millisecond)
	procKeybdEvent.Call(uintptr(vk), 0, keyEventFKeyUp, 0) //nolint:errcheck // Windows API call, return value not meaningful
	time.Sleep(2 * time.Millisecond)
	procKeybdEvent.Call(vkControl, 0, keyEventFKeyUp, 0) //nolint:errcheck // Windows API call, return value not meaningful
}
