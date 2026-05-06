package main

import (
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func parseAudioAndProviderSettingsForm(req *http.Request, cfg *config.Config, f *settingsFormData) string {
	f.AudioDeviceID = firstNonEmpty(
		trimmedFormValue(req, "audio_device_id"),
		trimmedFormValue(req, "selected_audio_device_id"),
		cfg.Audio.DeviceID,
	)
	f.HFModel = valueOrDefault(trimmedFormValue(req, "hf_model"), cfg.HuggingFace.Model)
	if config.ManagedHuggingFaceAvailableInBuild() && !isSupportedHFModel(f.HFModel) {
		return msgUnsupportedModel
	}
	return ""
}
