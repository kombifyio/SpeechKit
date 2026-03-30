package main

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type desktopCommandHandler struct {
	cfg                 *config.Config
	state               *appState
	recordingController *speechkit.RecordingController
	feedbackStore       store.Store
	showDashboard       func(string)
	quickNotes          quickNoteCommandService
	actions             *quickActionCoordinator
}

type quickNoteCommandService interface {
	ArmRecording(noteID int64)
	OpenEditor(noteID int64) error
	OpenCapture(ctx context.Context) (int64, error)
	CloseCapture() error
}

func (h desktopCommandHandler) Handle(ctx context.Context, command speechkit.Command) error {
	switch command.Type {
	case speechkit.CommandShowDashboard:
		if h.showDashboard != nil {
			source := command.Metadata["source"]
			if source == "" {
				source = "command"
			}
			h.showDashboard(source)
		}
		return nil
	case speechkit.CommandStartDictation:
		return h.startDictation(ctx, command)
	case speechkit.CommandStopDictation:
		return h.stopDictation(command)
	case speechkit.CommandSetActiveMode:
		mode := command.Metadata["mode"]
		if mode == "" {
			return fmt.Errorf("mode missing")
		}
		h.state.setActiveMode(mode)
		return nil
	case speechkit.CommandOpenQuickNote:
		if h.quickNotes == nil {
			return fmt.Errorf("quick notes not configured")
		}
		return h.quickNotes.OpenEditor(command.NoteID)
	case speechkit.CommandOpenQuickCapture:
		if h.quickNotes == nil {
			return fmt.Errorf("quick notes not configured")
		}
		_, err := h.quickNotes.OpenCapture(ctx)
		return err
	case speechkit.CommandCloseQuickCapture:
		if h.quickNotes == nil {
			return fmt.Errorf("quick notes not configured")
		}
		return h.quickNotes.CloseCapture()
	case speechkit.CommandArmQuickNoteRecording:
		if h.quickNotes == nil {
			return fmt.Errorf("quick notes not configured")
		}
		h.quickNotes.ArmRecording(command.NoteID)
		return nil
	case speechkit.CommandCopyLastTranscription, speechkit.CommandInsertLastTranscription, speechkit.CommandSummarizeSelection:
		if h.actions == nil {
			return fmt.Errorf("quick actions not configured")
		}
		return h.actions.Execute(ctx, command)
	default:
		return fmt.Errorf("unsupported command: %s", command.Type)
	}
}

func (h desktopCommandHandler) startDictation(ctx context.Context, command speechkit.Command) error {
	if h.recordingController == nil {
		return fmt.Errorf("recording controller not configured")
	}

	quickNote := h.state.currentQuickNoteContext()

	if quickNote.enabled && quickNote.noteID == 0 && h.feedbackStore != nil {
		noteID, err := h.feedbackStore.SaveQuickNote(ctx, "", h.cfg.General.Language, "", 0, 0, nil)
		if err != nil {
			h.state.addLog(fmt.Sprintf("Quick Note init failed: %v", err), "error")
			return err
		}
		quickNote.noteID = noteID
		h.state.setQuickCaptureNoteID(noteID)
	}

	label := command.Metadata["label"]
	if label == "" {
		label = "Recording started"
	}

	return h.recordingController.Start(speechkit.RecordingStartOptions{
		Label:       label,
		Target:      output.CaptureTarget(),
		Language:    h.cfg.General.Language,
		QuickNote:   quickNote.enabled,
		QuickNoteID: quickNote.noteID,
	})
}

func (h desktopCommandHandler) stopDictation(command speechkit.Command) error {
	if h.recordingController == nil {
		return fmt.Errorf("recording controller not configured")
	}

	label := command.Metadata["label"]
	if label == "" {
		label = "Captured"
	}

	if err := h.recordingController.Stop(speechkit.RecordingStopOptions{Label: label}); err != nil {
		return err
	}

	h.state.setLevel(0)
	h.state.clearQuickNoteRecording()

	return nil
}
