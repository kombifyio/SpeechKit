package main

import "github.com/kombifyio/SpeechKit/internal/models"

func defaultActiveProfiles(catalog models.Catalog) map[string]string {
	profiles := make(map[string]string)
	for _, modality := range []models.Modality{
		models.ModalitySTT,
		models.ModalityTTS,
		models.ModalityRealtimeVoice,
		models.ModalityAgent,
	} {
		if profile, ok := catalog.DefaultProfile(modality); ok {
			profiles[string(modality)] = profile.ID
		}
	}
	return profiles
}
