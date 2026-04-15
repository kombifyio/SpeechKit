package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestPhysicalScreenBoundsToDipRoundTripsApplicationScaling(t *testing.T) {
	wantDip := screenBounds{X: 120, Y: 80, Width: 1440, Height: 900}
	got := physicalScreenBoundsToDip(screenBounds{X: 240, Y: 160, Width: 2880, Height: 1800}, func(rect application.Rect) application.Rect {
		return application.Rect{
			X:      rect.X / 2,
			Y:      rect.Y / 2,
			Width:  rect.Width / 2,
			Height: rect.Height / 2,
		}
	})

	if got != wantDip {
		t.Fatalf("dip bounds = %+v, want %+v", got, wantDip)
	}
}
