package main

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/store"
)

type quickNoteHost interface {
	OpenEditor(noteID int64) error
	OpenCapture(noteID int64) error
	CloseCapture() error
}

type desktopQuickNoteService struct {
	cfg           *config.Config
	state         *appState
	feedbackStore store.Store
	host          quickNoteHost
}

func (s desktopQuickNoteService) ArmRecording(noteID int64) {
	if s.state == nil {
		return
	}
	s.state.armQuickNoteRecording(noteID)
	s.state.addLog("Quick Note recording armed", "info")
}

func (s desktopQuickNoteService) OpenEditor(noteID int64) error {
	if s.state != nil && s.state.currentQuickNoteContext().captureMode {
		if err := s.closeCapture(context.Background()); err != nil {
			return err
		}
	}
	if s.host == nil {
		return nil
	}
	return s.host.OpenEditor(noteID)
}

func (s desktopQuickNoteService) OpenCapture(ctx context.Context) (int64, error) {
	if s.state != nil && s.state.currentQuickNoteContext().captureMode {
		if err := s.closeCapture(ctx); err != nil {
			return 0, err
		}
	}

	var noteID int64
	if s.feedbackStore != nil {
		language := ""
		if s.cfg != nil {
			language = s.cfg.General.Language
		}
		id, err := s.feedbackStore.SaveQuickNote(ctx, "", language, "capture", 0, 0, nil)
		if err != nil {
			if s.state != nil {
				s.state.addLog(fmt.Sprintf("Quick Capture: failed to create note: %v", err), "error")
			}
			return 0, err
		}
		noteID = id
	}

	if s.state != nil {
		s.state.armQuickCapture(noteID)
	}
	if s.host != nil {
		if err := s.host.OpenCapture(noteID); err != nil {
			if s.state != nil {
				s.state.clearQuickNoteRecording()
			}
			return 0, err
		}
	}
	if s.state != nil {
		s.state.addLog(fmt.Sprintf("Quick Capture opened (note #%d)", noteID), "info")
	}
	return noteID, nil
}

func (s desktopQuickNoteService) CloseCapture() error {
	return s.closeCapture(context.Background())
}

func (s desktopQuickNoteService) closeCapture(ctx context.Context) error {
	var noteID int64
	if s.state != nil {
		noteID = s.state.currentQuickNoteContext().noteID
	}
	if s.state != nil {
		s.state.clearQuickNoteRecording()
	}
	if s.host == nil {
		return s.cleanupEmptyQuickCapture(ctx, noteID)
	}
	if err := s.host.CloseCapture(); err != nil {
		return err
	}
	if s.state != nil {
		s.state.addLog("Quick Capture window closed", "info")
	}
	return s.cleanupEmptyQuickCapture(ctx, noteID)
}

func (s desktopQuickNoteService) cleanupEmptyQuickCapture(ctx context.Context, noteID int64) error {
	if s.feedbackStore == nil || noteID <= 0 {
		return nil
	}

	note, err := s.feedbackStore.GetQuickNote(ctx, noteID)
	if err != nil || note == nil {
		return nil
	}
	if note.Text != "" || note.AudioPath != "" || note.DurationMs > 0 {
		return nil
	}
	return s.feedbackStore.DeleteQuickNote(ctx, noteID)
}

type wailsQuickNoteHost struct {
	state *appState
}

func (h wailsQuickNoteHost) OpenEditor(noteID int64) error {
	if h.state == nil {
		return nil
	}
	h.state.mu.Lock()
	wApp := h.state.wailsApp
	h.state.mu.Unlock()
	if wApp == nil {
		return nil
	}

	idParam := ""
	if noteID > 0 {
		idParam = fmt.Sprintf("?id=%d", noteID)
	}

	application.InvokeSync(func() {
		noteWin := wApp.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:  "Quick Note",
			Width:  440,
			Height: 340,
			URL:    "/quicknote.html" + idParam,
			Windows: application.WindowsWindow{
				Theme: application.Dark,
			},
		})
		noteWin.Focus()
	})
	return nil
}

func (h wailsQuickNoteHost) OpenCapture(noteID int64) error {
	if h.state == nil {
		return nil
	}
	h.state.mu.Lock()
	wApp := h.state.wailsApp
	h.state.mu.Unlock()
	if wApp == nil {
		return nil
	}

	application.InvokeSync(func() {
		win := wApp.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            "",
			Width:            340,
			Height:           200,
			URL:              fmt.Sprintf("/quickcapture.html?noteId=%d", noteID),
			Frameless:        true,
			BackgroundType:   application.BackgroundTypeSolid,
			BackgroundColour: application.NewRGBA(11, 15, 20, 255),
			Windows: application.WindowsWindow{
				Theme: application.Dark,
			},
		})
		win.Focus()
		h.state.mu.Lock()
		h.state.captureWin = win
		h.state.mu.Unlock()
	})
	return nil
}

func (h wailsQuickNoteHost) CloseCapture() error {
	if h.state == nil {
		return nil
	}
	h.state.mu.Lock()
	win := h.state.captureWin
	h.state.captureWin = nil
	h.state.mu.Unlock()

	if win == nil {
		return nil
	}
	application.InvokeSync(func() {
		win.Hide()
		win.Close()
	})
	return nil
}
