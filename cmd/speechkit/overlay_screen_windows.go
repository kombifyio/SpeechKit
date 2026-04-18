package main

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

const monitorDefaultToNearest = 0x00000002

var (
	user32Overlay            = windows.NewLazyDLL("user32.dll")
	procOverlayForeground    = user32Overlay.NewProc("GetForegroundWindow")
	procOverlayMonitorFromHW = user32Overlay.NewProc("MonitorFromWindow")
	procOverlayMonitorInfo   = user32Overlay.NewProc("GetMonitorInfoW")
	procSetDPIAwareness      = user32Overlay.NewProc("SetProcessDPIAware")
)

func init() {
	// Ensure the process is DPI-aware so GetMonitorInfoW returns correct pixel coords.
	// This must happen before any window creation. Safe to call even if already DPI-aware.
	procSetDPIAwareness.Call() //nolint:errcheck // Windows API call, return value not meaningful
}

type activeWindowScreenLocator struct{}

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

func newActiveWindowScreenLocator() overlayScreenLocator {
	return activeWindowScreenLocator{}
}

func (activeWindowScreenLocator) OverlayScreenBounds() (screenBounds, bool) {
	hwnd, _, _ := procOverlayForeground.Call()
	if hwnd == 0 {
		return screenBounds{}, false
	}

	monitor, _, _ := procOverlayMonitorFromHW.Call(hwnd, monitorDefaultToNearest)
	if monitor == 0 {
		return screenBounds{}, false
	}

	info := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	ok, _, _ := procOverlayMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))) //nolint:gosec // Windows API requires unsafe.Pointer
	if ok == 0 {
		return screenBounds{}, false
	}

	return physicalScreenBoundsToDip(screenBounds{
		X:      int(info.RcWork.Left),
		Y:      int(info.RcWork.Top),
		Width:  int(info.RcWork.Right - info.RcWork.Left),
		Height: int(info.RcWork.Bottom - info.RcWork.Top),
	}, currentPhysicalToDipRect()), true
}

func currentPhysicalToDipRect() func(application.Rect) application.Rect {
	app := application.Get()
	if app == nil || app.Screen == nil {
		return nil
	}
	return app.Screen.PhysicalToDipRect
}

func physicalScreenBoundsToDip(bounds screenBounds, convert func(application.Rect) application.Rect) screenBounds {
	if convert == nil {
		return bounds
	}
	rect := convert(application.Rect{
		X:      bounds.X,
		Y:      bounds.Y,
		Width:  bounds.Width,
		Height: bounds.Height,
	})
	return screenBounds{
		X:      rect.X,
		Y:      rect.Y,
		Width:  rect.Width,
		Height: rect.Height,
	}
}
