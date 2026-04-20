package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

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
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

func registerAppRoutes(mux *http.ServeMux, cfgPath string, state *appState, installState *config.InstallState) {
	updateManager := ensureAppUpdateManager(state)

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
		if latest, ok := cachedLatestRelease(); ok && latest.Version != "" && isNewerReleaseVersion(latest.Version, AppVersion) { //nolint:contextcheck // cachedLatestRelease triggers background refresh goroutine that must not be bound to request context
			resp["latestVersion"] = latest.Version
			resp["updateURL"] = latest.ReleaseURL
			if latest.DownloadURL != "" {
				resp["downloadURL"] = latest.DownloadURL
				resp["downloadSizeBytes"] = latest.DownloadSize
				resp["assetName"] = latest.DownloadName
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/app/update/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(updateManager.AllJobs())
	})

	mux.HandleFunc("/app/update/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		latest, ok := cachedLatestRelease() //nolint:contextcheck // background refresh goroutine must not be bound to request context
		if !ok || latest.Version == "" || !isNewerReleaseVersion(latest.Version, AppVersion) {
			http.Error(w, "no update available", http.StatusNotFound)
			return
		}
		if strings.TrimSpace(latest.DownloadURL) == "" {
			http.Error(w, "download unavailable for latest release", http.StatusNotFound)
			return
		}

		requestedVersion := strings.TrimSpace(r.FormValue("version"))
		if requestedVersion != "" && requestedVersion != latest.Version {
			http.Error(w, "requested version is no longer current", http.StatusConflict)
			return
		}

		job := updateManager.Start(latest, resolveAppUpdateDir(cfgPath)) //nolint:contextcheck // download job runs in background and must not be bound to request context
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(job)
	})

	mux.HandleFunc("/app/update/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		jobID := strings.TrimSpace(r.FormValue("job_id"))
		if jobID == "" {
			http.Error(w, "job_id required", http.StatusBadRequest)
			return
		}
		if !updateManager.CancelJob(jobID) {
			http.Error(w, "job not found or already completed", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "cancelled"})
	})

	mux.HandleFunc("/app/update/open", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		jobID := strings.TrimSpace(r.FormValue("job_id"))
		if jobID == "" {
			http.Error(w, "job_id required", http.StatusBadRequest)
			return
		}
		path, ok := updateManager.CompletedFile(jobID)
		if !ok {
			http.Error(w, "installer not ready", http.StatusConflict)
			return
		}
		if err := verifyInstallerBeforeOpen(path); err != nil {
			http.Error(w, "installer verification failed: "+err.Error(), http.StatusConflict)
			return
		}
		if err := openInstallerFileInShell(path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "opened", "filePath": path})
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
