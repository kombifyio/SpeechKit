package main

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/auth"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/features"
)

func registerFeatureRoutes(mux *http.ServeMux, installState *config.InstallState) {
	mux.HandleFunc("/features", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(features.Detect(installState))
	})
}

func registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider == nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"available": false,
				"loggedIn":  false,
			})
			return
		}
		identity, _ := provider.GetIdentity(r.Context())
		resp := map[string]interface{}{
			"available": true,
			"loggedIn":  provider.IsLoggedIn(),
		}
		if identity != nil {
			resp["email"] = identity.Email
			resp["plan"] = identity.Plan
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Auth not available in this build. Use the kombify build for cloud features.",
			})
			return
		}
		resp, err := provider.StartDeviceCodeFlow(r.Context())
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/auth/poll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider == nil {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Auth not available"})
			return
		}
		_ = r.ParseForm()
		deviceCode := r.FormValue("device_code")
		_, err := provider.PollDeviceCode(r.Context(), deviceCode)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"pending": true,
				"error":   err.Error(),
			})
			return
		}
		identity, err := provider.GetIdentity(r.Context())
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"pending":       false,
				"authenticated": provider.IsLoggedIn(),
				"error":         err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"pending":       false,
			"authenticated": provider.IsLoggedIn(),
			"identity":      identity,
		})
	})
	mux.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		provider := auth.GetAuthProvider()
		if provider != nil {
			_ = provider.Logout(r.Context())
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Logged out"})
	})
}

func registerAppRoutes(mux *http.ServeMux, installState *config.InstallState) {
	mux.HandleFunc("/app/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp := map[string]interface{}{
			"version": AppVersion,
		}
		// Check for updates (non-blocking, cached)
		if latest, url, ok := cachedLatestRelease(); ok && latest != "" && latest != AppVersion {
			resp["latestVersion"] = latest
			resp["updateURL"] = url
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/app/setup-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"setupDone": installState.SetupDone,
		})
	})

	mux.HandleFunc("/app/complete-setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		installState.SetupDone = true
		if err := config.SaveInstallState(installState); err != nil {
			slog.Warn("save setup completion", "err", err)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]bool{"setupDone": true})
	})
}
