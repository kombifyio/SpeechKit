package main

import "github.com/wailsapp/wails/v3/pkg/application"

func newQuickNoteWindowOptions(url string) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "Quick Note",
		Width:            840,
		Height:           700,
		MinWidth:         640,
		MinHeight:        420,
		Frameless:        true,
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(16, 20, 28, 238),
		URL:              url,
		Windows: application.WindowsWindow{
			Theme:        application.Dark,
			BackdropType: application.Mica,
			DisableIcon:  true,
		},
	}
}

func newQuickCaptureWindowOptions(url string) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "Quick Capture",
		Width:            520,
		Height:           240,
		MinWidth:         440,
		MinHeight:        200,
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(16, 20, 28, 238),
		URL:              url,
		Windows: application.WindowsWindow{
			Theme:        application.Dark,
			BackdropType: application.Mica,
			DisableIcon:  true,
		},
	}
}
