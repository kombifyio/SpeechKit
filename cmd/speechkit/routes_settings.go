package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/transcription"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/store"
)

var errMissingHuggingFaceToken = fmt.Errorf("missing hugging face token")
var errHFUnavailableBuild = errors.New("hugging face is not available in this build")

func registerSettingsRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, feedbackStore store.Store) {
	registerSettingsCoreRoutes(mux, cfgPath, cfg, state, sttRouter, feedbackStore)
	registerSettingsCredentialRoutes(mux, cfgPath, cfg, state, sttRouter)
	registerAudioDeviceRoutes(mux, cfgPath, cfg, state)
	registerModeRoutes(mux, cfgPath, cfg, state)
	registerModelRoutes(mux, cfgPath, cfg, state, sttRouter)
}

func saveSettings(ctx context.Context, req *http.Request, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, feedbackStore store.Store) string {
	if err := req.ParseForm(); err != nil {
		return msgFormParseError
	}

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		return errMsg
	}
	if !form.OverlayMovable {
		form.OverlayFreeX = 0
		form.OverlayFreeY = 0
		form.OverlayMonitorPositions = map[string]config.OverlayFreePosition{}
	}

	nextCfg := buildNextConfig(form, cfg)
	applySelectedVoiceAgentProfile(&nextCfg, filteredModelCatalog())
	oldDictateEnabled := cfg.General.DictateEnabled
	oldAssistEnabled := cfg.General.AssistEnabled
	oldVoiceAgentEnabled := cfg.General.VoiceAgentEnabled
	oldDictateHotkey := cfg.General.DictateHotkey
	oldAssistHotkey := cfg.General.AssistHotkey
	oldVoiceAgentHotkey := cfg.General.VoiceAgentHotkey
	oldVoiceAgentProfileID := cfg.VoiceAgent.AgentProfileID
	oldAudioDeviceID := cfg.Audio.DeviceID

	managedHFEnabled := config.ApplyManagedIntegrationDefaults(&nextCfg)
	needsHFRefresh := managedHFEnabled ||
		!cfg.HuggingFace.Enabled ||
		form.HFModel != cfg.HuggingFace.Model
	shouldValidateHF := nextCfg.HuggingFace.Enabled && needsHFRefresh
	if shouldValidateHF {
		if err := refreshHuggingFaceProvider(ctx, &nextCfg, sttRouter, managedHFEnabled); err != nil {
			if errors.Is(err, errMissingHuggingFaceToken) {
				return msgHFTokenMissing
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	if !nextCfg.HuggingFace.Enabled {
		sttRouter.SetHuggingFace(nil)
	}

	if err := refreshProviderRuntimes(ctx, &nextCfg, state, sttRouter); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}

	*cfg = nextCfg
	voiceAgentProfileChanged := oldVoiceAgentProfileID != cfg.VoiceAgent.AgentProfileID

	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	if err := transcription.SyncVocabularyDictionaryStore(ctx, feedbackStore, form.Language, form.VocabularyDictionary); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}

	state.applyRuntimeSettings(
		form.DictateEnabled,
		form.AssistEnabled,
		form.VoiceAgentEnabled,
		form.DictateHotkey,
		form.AssistHotkey,
		form.VoiceAgentHotkey,
		form.DictateHotkeyBehavior,
		form.AssistHotkeyBehavior,
		form.VoiceAgentHotkeyBehavior,
		form.ActiveMode,
		form.AudioDeviceID,
		runtimeAvailableProviders(ctx, sttRouter),
		form.Visualizer,
		form.Design,
		form.AssistOverlayMode,
		form.VoiceAgentOverlayMode,
		form.OverlayPosition,
		form.VocabularyDictionary,
		form.OverlayMovable,
		form.OverlayFreeX,
		form.OverlayFreeY,
		form.OverlayMonitorPositions,
	)
	if voiceAgentProfileChanged {
		resetInactiveVoiceAgentSession(state)
		if err := refreshServerDelegates(cfg, state); err != nil {
			slog.Warn("refresh server delegates after voice agent profile change", "err", err)
		}
	}
	state.applyDesktopSettings(
		oldDictateEnabled,
		oldAssistEnabled,
		oldVoiceAgentEnabled,
		oldDictateHotkey,
		oldAssistHotkey,
		oldVoiceAgentHotkey,
		form.DictateEnabled,
		form.AssistEnabled,
		form.VoiceAgentEnabled,
		form.DictateHotkey,
		form.AssistHotkey,
		form.VoiceAgentHotkey,
		oldAudioDeviceID,
		form.AudioDeviceID,
		form.OverlayEnabled,
	)

	return msgSaved
}

func resetOverlayPosition(cfgPath string, cfg *config.Config, state *appState) string {
	if cfg == nil || state == nil {
		return fmt.Sprintf(msgSaveFailed, errors.New("settings state unavailable"))
	}

	cfg.UI.OverlayFreeX = 0
	cfg.UI.OverlayFreeY = 0
	cfg.UI.OverlayMonitorPositions = map[string]config.OverlayFreePosition{}

	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}

	state.mu.Lock()
	state.overlayFreeX = 0
	state.overlayFreeY = 0
	state.overlayMonitorCenters = map[string]config.OverlayFreePosition{}
	state.syncSpeechKitSnapshotLocked()
	state.mu.Unlock()

	state.refreshOverlayWindows()

	return msgSaved
}

func refreshHuggingFaceProvider(ctx context.Context, cfg *config.Config, sttRouter *router.Router, skipHealthCheck bool) error {
	if !config.ManagedHuggingFaceAvailableInBuild() {
		return errHFUnavailableBuild
	}
	token, _, err := config.ResolveHuggingFaceToken(cfg)
	if err != nil {
		return err
	}
	if token == "" {
		return errMissingHuggingFaceToken
	}

	provider := newHuggingFaceProvider(cfg.HuggingFace.Model, token)
	if !skipHealthCheck {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := provider.Health(checkCtx)
		cancel()
		if err != nil {
			return err
		}
	}
	sttRouter.SetHuggingFace(provider)
	return nil
}

func saveHuggingFaceToken(ctx context.Context, req *http.Request, cfg *config.Config, state *appState, sttRouter *router.Router) string {
	if err := req.ParseForm(); err != nil {
		return msgFormParseError
	}
	if !config.ManagedHuggingFaceAvailableInBuild() {
		return msgHFUnavailableBuild
	}
	token := strings.TrimSpace(req.FormValue("hf_token"))
	if token == "" {
		return msgHFTokenRequired
	}
	if err := secrets.SetUserHuggingFaceToken(token); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	cfg.HuggingFace.Enabled = true
	if strings.TrimSpace(cfg.HuggingFace.Model) == "" {
		cfg.HuggingFace.Model = "openai/whisper-large-v3"
	}
	if cfg.HuggingFace.Enabled {
		if err := refreshHuggingFaceProvider(ctx, cfg, sttRouter, false); err != nil {
			if errors.Is(err, errMissingHuggingFaceToken) {
				return msgHFTokenMissing
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	if err := refreshProviderRuntimes(ctx, cfg, state, sttRouter); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	return msgHFTokenSaved
}

func clearHuggingFaceToken(ctx context.Context, cfg *config.Config, state *appState, sttRouter *router.Router) string {
	if !config.ManagedHuggingFaceAvailableInBuild() {
		return msgHFUnavailableBuild
	}
	if err := secrets.ClearUserHuggingFaceToken(); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	if cfg.HuggingFace.Enabled {
		if err := refreshHuggingFaceProvider(ctx, cfg, sttRouter, true); err != nil {
			if errors.Is(err, errMissingHuggingFaceToken) {
				sttRouter.SetHuggingFace(nil)
				if refreshErr := refreshProviderRuntimes(ctx, cfg, state, sttRouter); refreshErr != nil {
					return fmt.Sprintf(msgSaveFailed, refreshErr)
				}
				return msgHFTokenCleared
			}
			return fmt.Sprintf(msgModelUnreachable, err)
		}
	}
	if err := refreshProviderRuntimes(ctx, cfg, state, sttRouter); err != nil {
		return fmt.Sprintf(msgSaveFailed, err)
	}
	return msgHFTokenCleared
}

func isSupportedHFModel(model string) bool {
	switch model {
	case "openai/whisper-large-v3-turbo", "openai/whisper-large-v3":
		return true
	default:
		return false
	}
}

func filteredModelCatalog() models.Catalog {
	catalog := models.DefaultCatalog()
	filtered := make([]models.Profile, 0, len(catalog.Profiles))
	for _, profile := range catalog.Profiles {
		switch profile.Modality {
		case models.ModalitySTT, models.ModalityUtility, models.ModalityAssist, models.ModalityRealtimeVoice:
		default:
			continue
		}
		filtered = append(filtered, profile)
	}
	catalog.Profiles = filtered
	return catalog
}

func isSupportedOverlayVisualizer(visualizer string) bool {
	switch visualizer {
	case "pill", "circle":
		return true
	default:
		return false
	}
}

func isSupportedOverlayDesign(design string) bool {
	switch design {
	case "default", "kombify":
		return true
	default:
		return false
	}
}

func isSupportedOverlayPosition(pos string) bool {
	switch pos {
	case "top", "bottom", "left", "right":
		return true
	default:
		return false
	}
}
