package main

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
)

func registerAudioDeviceRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	mux.HandleFunc("/audio/devices", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		devices, err := audio.ListCaptureDevices(audio.Config{
			Backend: audio.Backend(cfg.Audio.Backend),
		})
		if err != nil {
			writeJSON(w, map[string]any{
				"selectedDeviceId": cfg.Audio.DeviceID,
				"devices":          []audio.DeviceInfo{},
				"error":            err.Error(),
			})
			return
		}
		state.mu.Lock()
		selectedDeviceID := state.audioDeviceID
		state.mu.Unlock()
		writeJSON(w, map[string]any{
			"selectedDeviceId": selectedDeviceID,
			"devices":          devices,
		})
	})
	mux.HandleFunc("/audio/device", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		deviceID := strings.TrimSpace(r.FormValue("device_id"))
		if deviceID == "" {
			deviceID = strings.TrimSpace(r.FormValue("selected_audio_device_id"))
		}
		cfg.Audio.DeviceID = deviceID
		state.setAudioDevice(deviceID)
		if state.audioSession != nil {
			if err := state.audioSession.ReconfigureDevice(deviceID); err != nil {
				slog.Warn("audio device reconfigure", "err", err)
			}
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save audio device config", "err", err)
		}
		writeJSON(w, map[string]string{"selectedDeviceId": deviceID})
	})
	mux.HandleFunc("/audio/output/devices", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		devices, err := audio.ListOutputDevices(audio.Config{
			Backend: audio.Backend(cfg.Audio.Backend),
		})
		if err != nil {
			writeJSON(w, map[string]any{
				"selectedDeviceId": cfg.Audio.OutputDeviceID,
				"devices":          []audio.DeviceInfo{},
				"error":            err.Error(),
			})
			return
		}
		state.mu.Lock()
		selectedDeviceID := state.audioOutputDeviceID
		state.mu.Unlock()
		if selectedDeviceID == "" {
			selectedDeviceID = cfg.Audio.OutputDeviceID
		}
		writeJSON(w, map[string]any{
			"selectedDeviceId": selectedDeviceID,
			"devices":          devices,
		})
	})
	mux.HandleFunc("/audio/output/device", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		deviceID := strings.TrimSpace(r.FormValue("device_id"))
		if deviceID == "" {
			deviceID = strings.TrimSpace(r.FormValue("selected_output_device_id"))
		}
		cfg.Audio.OutputDeviceID = deviceID
		state.setAudioOutputDevice(r.Context(), deviceID)
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save audio output device config", "err", err)
		}
		writeJSON(w, map[string]string{"selectedDeviceId": deviceID})
	})
}
