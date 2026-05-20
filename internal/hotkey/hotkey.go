package hotkey

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
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

const pollInterval = 20 * time.Millisecond

type EventType int

const (
	EventKeyDown EventType = iota
	EventKeyUp
)

// EventSource identifies the origin of a hotkey event.
type EventSource string

const (
	// EventSourceHotkey is the default — the event came from a real
	// keyboard press picked up by the platform hotkey manager.
	EventSourceHotkey EventSource = "hotkey"

	// EventSourceWakeword indicates the event was synthesized by the
	// wake-word adapter to simulate a hotkey press. Consumers that need
	// different downstream behaviour for wake-word-triggered activations
	// (e.g. attach an AutoEndPolicy, ignore hold-to-talk semantics)
	// branch on this value.
	EventSourceWakeword EventSource = "wakeword"
)

type Event struct {
	Type    EventType
	Binding string

	// Source identifies the origin of this event. Native keyboard-hook
	// events leave this zero (empty string) which callers must treat as
	// equivalent to EventSourceHotkey. Synthesized events (wake-word,
	// future programmatic triggers) set this explicitly.
	Source EventSource
}

// Manager captures a push-to-talk key combination through a low-level keyboard hook.
type Manager struct {
	mu       sync.Mutex
	keys     []uint32
	events   chan Event
	cancel   context.CancelFunc
	done     chan struct{}
	pollDone chan struct{}
	threadID uint32
	hook     windows.Handle
	callback uintptr
	suppress bool
	tracker  *comboTracker
	active   bool
	stopOnce sync.Once
}

// NewManager creates a PTT manager. combo is a list of VK codes that must ALL be pressed.
func NewManager(combo []uint32) *Manager {
	keys := normalizeComboAllowEmpty(combo)
	return &Manager{
		keys:     keys,
		events:   make(chan Event, 16),
		done:     make(chan struct{}),
		pollDone: make(chan struct{}),
		suppress: false,
		tracker:  newComboTracker(keys),
	}
}

// Reconfigure swaps the key combination while the hook is running.
// The keyboard hook stays installed; only the combo tracker is replaced.
func (m *Manager) Reconfigure(combo []uint32) {
	keys := normalizeComboAllowEmpty(combo)
	tracker := newComboTracker(keys)

	wasActive := false
	m.mu.Lock()
	if m.active {
		wasActive = true
		m.active = false
	}
	m.keys = keys
	m.tracker = tracker
	m.mu.Unlock()

	if wasActive {
		m.emit(Event{Type: EventKeyUp})
	}
}

// Start installs a low-level keyboard hook and emits key down/up events for the configured combo.
func (m *Manager) Start(ctx context.Context) error {
	inner, cancel := context.WithCancel(ctx) //nolint:gosec // cancel stored in struct field, called on job cancellation
	m.cancel = cancel
	ready := make(chan error, 1)
	go m.pollLoop(inner)

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
			procPostThreadMsg.Call(uintptr(m.threadID), uintptr(wmQuit), 0, 0) //nolint:errcheck // Windows API call, return value not meaningful
		}()

		var msg winMessage
		for {
			ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0) //nolint:gosec // Windows API requires unsafe.Pointer
			switch int32(ret) {                                                      // #nosec G115 -- GetMessageW returns -1, 0, or a positive BOOL-compatible value.
			case -1:
				m.unhook()
				return
			case 0:
				m.unhook()
				return
			default:
				procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg))) //nolint:errcheck,gosec // Windows API requires unsafe.Pointer; return value not meaningful
				procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))  //nolint:errcheck,gosec // Windows API requires unsafe.Pointer; return value not meaningful
			}
		}
	}()

	if err := <-ready; err != nil {
		// Keep running on the polling fallback. Modifier-only combinations like
		// Win+Alt cannot use RegisterHotKey reliably, and the poller gives us a
		// second global signal when Windows declines or starves the low-level hook.
		return nil //nolint:nilerr // non-fatal: polling fallback is already running and handles this combo
	}
	return nil
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
		<-m.pollDone
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

func (m *Manager) keyboardProc(nCode int, wParam, lParam uintptr) uintptr {
	if nCode < hcAction || lParam == 0 {
		return m.callNext(nCode, wParam, lParam)
	}

	switch uint32(wParam) { // #nosec G115 -- keyboard message IDs are DWORD-compatible values.
	case wmKeyDown, wmSysKeyDown, wmKeyUp, wmSysKeyUp:
	default:
		return m.callNext(nCode, wParam, lParam)
	}

	var info kbdllHookStruct
	procRtlMoveMemory.Call( //nolint:errcheck // Windows API call, return value not meaningful
		uintptr(unsafe.Pointer(&info)), //nolint:gosec // Windows API requires unsafe.Pointer
		lParam,
		unsafe.Sizeof(info),
	)
	down := uint32(wParam) == wmKeyDown || uint32(wParam) == wmSysKeyDown // #nosec G115 -- keyboard message IDs are DWORD-compatible values.

	m.mu.Lock()
	event, ok := m.tracker.transition(info.VkCode, down)
	consume := m.suppress && m.tracker.consumes(info.VkCode)
	m.mu.Unlock()

	if ok {
		m.setActive(event.Type == EventKeyDown)
	}

	if consume {
		return 1
	}

	return m.callNext(nCode, wParam, lParam)
}

func (m *Manager) pollLoop(ctx context.Context) {
	defer close(m.pollDone)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.setActive(false)
			return
		case <-ticker.C:
			m.setActive(m.comboPressed())
		}
	}
}

func (m *Manager) comboPressed() bool {
	m.mu.Lock()
	keys := append([]uint32(nil), m.keys...)
	m.mu.Unlock()
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if !isNormalizedKeyDown(key) {
			return false
		}
	}
	return true
}

func isNormalizedKeyDown(vk uint32) bool {
	switch normalizeVK(vk) {
	case VkShift:
		return isKeyDown(VkShift) || isKeyDown(VkLShift) || isKeyDown(VkRShift)
	case VkControl:
		return isKeyDown(VkControl) || isKeyDown(VkLControl) || isKeyDown(VkRControl)
	case VkMenu:
		return isKeyDown(VkMenu) || isKeyDown(VkLMenu) || isKeyDown(VkRMenu)
	case VkLWin:
		return isKeyDown(VkLWin) || isKeyDown(VkRWin)
	default:
		return isKeyDown(vk)
	}
}

func (m *Manager) setActive(active bool) {
	m.mu.Lock()
	if m.active == active {
		m.mu.Unlock()
		return
	}
	m.active = active
	m.mu.Unlock()

	if active {
		m.emit(Event{Type: EventKeyDown})
		return
	}
	m.emit(Event{Type: EventKeyUp})
}

func (m *Manager) emit(event Event) {
	select {
	case m.events <- event:
	default:
	}
}

func (m *Manager) callNext(nCode int, wParam, lParam uintptr) uintptr {
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam) //nolint:gosec // G115: nCode is Windows hook code, value within bounds
	return ret
}

func (m *Manager) unhook() {
	if m.hook == 0 {
		return
	}
	procUnhookWindows.Call(uintptr(m.hook)) //nolint:errcheck // Windows API call, return value not meaningful
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
			return functionKeyVirtualKeys[n-1], true
		}
	}

	return 0, false
}

var functionKeyVirtualKeys = [...]uint32{
	0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77,
	0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, 0x7F,
	0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
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
