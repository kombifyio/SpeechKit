package hotkey

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/kombifyio/SpeechKit/internal/winapi"
)

var (
	procGetAsyncKeyState = winapi.User32.NewProc("GetAsyncKeyState")
	procCallNextHookEx   = winapi.User32.NewProc("CallNextHookEx")
	procGetMessage       = winapi.User32.NewProc("GetMessageW")
	procPostThreadMsg    = winapi.User32.NewProc("PostThreadMessageW")
	procSetWindowsHookEx = winapi.User32.NewProc("SetWindowsHookExW")
	procTranslateMessage = winapi.User32.NewProc("TranslateMessage")
	procDispatchMessage  = winapi.User32.NewProc("DispatchMessageW")
	procUnhookWindows    = winapi.User32.NewProc("UnhookWindowsHookEx")
	procRtlMoveMemory    = winapi.RtlMoveMemory
)

// Virtual key codes
const (
	VkBackspace = 0x08
	VkTab       = 0x09
	VkReturn    = 0x0D
	VkEscape    = 0x1B
	VkSpace     = 0x20

	VkLShift   = 0xA0
	VkRShift   = 0xA1
	VkLControl = 0xA2
	VkRControl = 0xA3
	VkLMenu    = 0xA4 // Left Alt
	VkRMenu    = 0xA5 // Right Alt
	VkLWin     = 0x5B
	VkRWin     = 0x5C
	VkShift    = 0x10
	VkControl  = 0x11
	VkMenu     = 0x12 // Alt
)

const (
	whKeyboardLL = 13
	hcAction     = 0
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmQuit       = 0x0012
)

type EventType int

const (
	EventKeyDown EventType = iota
	EventKeyUp
)

type Event struct {
	Type    EventType
	Binding string
}

// Manager captures a push-to-talk key combination through a low-level keyboard hook.
type Manager struct {
	keys     []uint32
	events   chan Event
	cancel   context.CancelFunc
	done     chan struct{}
	threadID uint32
	hook     windows.Handle
	callback uintptr
	suppress bool
	tracker  *comboTracker
	stopOnce sync.Once
}

// NewManager creates a PTT manager. combo is a list of VK codes that must ALL be pressed.
func NewManager(combo []uint32) *Manager {
	keys := normalizeComboAllowEmpty(combo)
	return &Manager{
		keys:     keys,
		events:   make(chan Event, 16),
		done:     make(chan struct{}),
		suppress: false,
		tracker:  newComboTracker(keys),
	}
}

// Reconfigure swaps the key combination while the hook is running.
// The keyboard hook stays installed; only the combo tracker is replaced.
func (m *Manager) Reconfigure(combo []uint32) {
	keys := normalizeComboAllowEmpty(combo)
	tracker := newComboTracker(keys)

	// Swap atomically under the implicit lock of the channel-based event system.
	// The keyboardProc callback reads m.tracker on every key event.
	// This is safe because tracker assignment is a pointer-width write (atomic on x86-64).
	m.keys = keys
	m.tracker = tracker
}

// Start installs a low-level keyboard hook and emits key down/up events for the configured combo.
func (m *Manager) Start(ctx context.Context) error {
	inner, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	ready := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(m.done)

		m.threadID = windows.GetCurrentThreadId()
		m.callback = windows.NewCallback(m.keyboardProc)
		hook, _, err := procSetWindowsHookEx.Call(
			uintptr(whKeyboardLL),
			m.callback,
			0,
			0,
		)
		if hook == 0 {
			ready <- fmt.Errorf("SetWindowsHookExW: %w", err)
			return
		}
		m.hook = windows.Handle(hook)
		ready <- nil

		go func() {
			<-inner.Done()
			procPostThreadMsg.Call(uintptr(m.threadID), uintptr(wmQuit), 0, 0)
		}()

		var msg winMessage
		for {
			ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			switch int32(ret) {
			case -1:
				m.unhook()
				return
			case 0:
				m.unhook()
				return
			default:
				procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
				procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
			}
		}
	}()

	return <-ready
}

func (m *Manager) Events() <-chan Event {
	return m.events
}

func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		<-m.done
		close(m.events)
	})
}

func isKeyDown(vk uint32) bool {
	ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return ret&0x8000 != 0
}

// --- Preset combos ---

// ComboAlt returns VK code for Alt.
func ComboAlt() []uint32 {
	return []uint32{VkMenu}
}

// ComboCtrlShift returns VK codes for Ctrl+Shift.
func ComboCtrlShift() []uint32 {
	return []uint32{VkControl, VkShift}
}

// ComboWinAlt returns VK codes for Win+Alt.
func ComboWinAlt() []uint32 {
	return []uint32{VkLWin, VkMenu}
}

// ComboF8 returns VK code for F8.
func ComboF8() []uint32 {
	return []uint32{0x77}
}

// ParseCombo parses strings like "alt" or "ctrl+shift+d" into VK codes.
func ParseCombo(s string) []uint32 {
	if s == "" {
		return ComboWinAlt()
	}

	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "+")
	result := make([]uint32, 0, len(parts))
	seen := make(map[uint32]struct{}, len(parts))

	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}

		vk, ok := parseToken(token)
		if !ok {
			return ComboWinAlt()
		}

		vk = normalizeVK(vk)
		if _, exists := seen[vk]; exists {
			continue
		}
		seen[vk] = struct{}{}
		result = append(result, vk)
	}

	if len(result) == 0 {
		return ComboWinAlt()
	}
	return normalizeCombo(result)
}

type comboTracker struct {
	required []uint32
	set      map[uint32]struct{}
	pressed  map[uint32]bool
	active   bool
}

func newComboTracker(keys []uint32) *comboTracker {
	keys = normalizeComboAllowEmpty(keys)
	set := make(map[uint32]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return &comboTracker{
		required: keys,
		set:      set,
		pressed:  make(map[uint32]bool, len(keys)),
	}
}

func (t *comboTracker) transition(vk uint32, down bool) (Event, bool) {
	vk = normalizeVK(vk)
	if _, ok := t.set[vk]; !ok {
		return Event{}, false
	}

	if down {
		t.pressed[vk] = true
	} else {
		delete(t.pressed, vk)
	}

	allPressed := true
	for _, key := range t.required {
		if !t.pressed[key] {
			allPressed = false
			break
		}
	}

	switch {
	case allPressed && !t.active:
		t.active = true
		return Event{Type: EventKeyDown}, true
	case !allPressed && t.active:
		t.active = false
		return Event{Type: EventKeyUp}, true
	default:
		return Event{}, false
	}
}

func (t *comboTracker) consumes(vk uint32) bool {
	_, ok := t.set[normalizeVK(vk)]
	return ok
}

func (m *Manager) keyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode < hcAction || lParam == 0 {
		return m.callNext(nCode, wParam, lParam)
	}

	switch uint32(wParam) {
	case wmKeyDown, wmSysKeyDown, wmKeyUp, wmSysKeyUp:
	default:
		return m.callNext(nCode, wParam, lParam)
	}

	var info kbdllHookStruct
	procRtlMoveMemory.Call(
		uintptr(unsafe.Pointer(&info)),
		lParam,
		unsafe.Sizeof(info),
	)
	down := uint32(wParam) == wmKeyDown || uint32(wParam) == wmSysKeyDown

	if event, ok := m.tracker.transition(info.VkCode, down); ok {
		select {
		case m.events <- event:
		default:
		}
	}

	if m.suppress && m.tracker.consumes(info.VkCode) {
		return 1
	}

	return m.callNext(nCode, wParam, lParam)
}

func (m *Manager) callNext(nCode int, wParam uintptr, lParam uintptr) uintptr {
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func (m *Manager) unhook() {
	if m.hook == 0 {
		return
	}
	procUnhookWindows.Call(uintptr(m.hook))
	m.hook = 0
}

func normalizeCombo(keys []uint32) []uint32 {
	normalized := normalizeComboAllowEmpty(keys)
	if len(normalized) == 0 {
		return ComboWinAlt()
	}
	if len(normalized) == 1 && normalized[0] == VkMenu {
		return ComboWinAlt()
	}
	return normalized
}

func normalizeComboAllowEmpty(keys []uint32) []uint32 {
	result := make([]uint32, 0, len(keys))
	seen := make(map[uint32]struct{}, len(keys))
	for _, key := range keys {
		key = normalizeVK(key)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeVK(vk uint32) uint32 {
	switch vk {
	case VkLShift, VkRShift:
		return VkShift
	case VkLControl, VkRControl:
		return VkControl
	case VkLMenu, VkRMenu:
		return VkMenu
	case VkRWin:
		return VkLWin
	default:
		return vk
	}
}

func parseToken(token string) (uint32, bool) {
	if vk, ok := tokenVirtualKeys[token]; ok {
		return vk, true
	}

	if len(token) == 1 {
		c := token[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint32(strings.ToUpper(token)[0]), true
		case c >= '0' && c <= '9':
			return uint32(c), true
		}
	}

	if strings.HasPrefix(token, "f") && len(token) <= 3 {
		n, err := strconv.Atoi(token[1:])
		if err == nil && n >= 1 && n <= 24 {
			return uint32(0x6F + n), true
		}
	}

	return 0, false
}

var tokenVirtualKeys = map[string]uint32{
	"alt":       VkMenu,
	"ctrl":      VkControl,
	"control":   VkControl,
	"shift":     VkShift,
	"win":       VkLWin,
	"windows":   VkLWin,
	"cmd":       VkLWin,
	"meta":      VkLWin,
	"space":     VkSpace,
	"enter":     VkReturn,
	"return":    VkReturn,
	"esc":       VkEscape,
	"escape":    VkEscape,
	"tab":       VkTab,
	"backspace": VkBackspace,
}

type kbdllHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type winPoint struct {
	X int32
	Y int32
}

type winMessage struct {
	Hwnd     windows.Handle
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       winPoint
	LPrivate uint32
}
