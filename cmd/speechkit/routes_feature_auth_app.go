package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/auth"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/features"
	"github.com/kombifyio/SpeechKit/internal/stt"
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

func registerAppRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, installState *config.InstallState) {
	updateManager := ensureAppUpdateManager(state)

	mux.HandleFunc("/app/control-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

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
			resp["installMode"] = latest.InstallMode
			if latest.InstallMode == appUpdateInstallModeManualUnsigned {
				resp["manualDownloadRequired"] = true
			}
			if latest.InstallMode == appUpdateInstallModeVerified && latest.DownloadURL != "" {
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
		if latest.InstallMode != appUpdateInstallModeVerified {
			http.Error(w, "manual download required for unsigned release", http.StatusConflict)
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
		if problem := localSetupCompletionProblem(cfg, installState); problem != "" {
			http.Error(w, problem, http.StatusConflict)
			return
		}
		installState.SetupDone = true
		if err := config.SaveInstallState(installState); err != nil {
			slog.Warn("save setup completion", "err", err)
		}
		// Sync the runtime overlay state to the saved config preference now
		// that the user has completed onboarding. During onboarding the
		// overlay was force-hidden (see runDesktopApp → state.setOverlayEnabled).
		// Most users want the overlay visible by default, which matches the
		// default cfg.UI.OverlayEnabled = true; a user who toggled it off in
		// the wizard sees no overlay regardless.
		if state != nil && cfg != nil {
			state.setOverlayEnabled(cfg.UI.OverlayEnabled)
			state.refreshOverlayWindows()
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]bool{"setupDone": true})
	})

	mux.HandleFunc("/app/install-mode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if installState == nil {
			http.Error(w, "install state not initialized", http.StatusServiceUnavailable)
			return
		}
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		var next config.InstallMode
		switch strings.TrimSpace(strings.ToLower(body.Mode)) {
		case "local":
			next = config.InstallModeLocal
		case "cloud", "server":
			// Server is functionally a Cloud install from the device's
			// perspective: no local model is required on this machine,
			// the actual STT/LLM provider (a hosted vendor or a self-
			// hosted SpeechKit server) is configured separately. The
			// ServerConnection block in cfg distinguishes the two at
			// runtime.
			next = config.InstallModeCloud
		default:
			http.Error(w, "unknown install mode", http.StatusBadRequest)
			return
		}
		installState.Mode = next
		if err := config.SaveInstallState(installState); err != nil {
			slog.Warn("save install-mode", "err", err)
		}
		// Echo the resolved mode (after the server→cloud collapse) so the
		// client can confirm the server's interpretation rather than blindly
		// trusting its own input.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"mode": string(installState.Mode),
		})
	})

	// Wake-word one-click endpoints. The Settings UI's WakewordPanel writes
	// the full settings form which goes through routes_settings.go and
	// triggers restartDesktopWakeword on changes. The endpoints below are
	// the simpler surface for the onboarding wizard: a single button click
	// enables the feature with sensible defaults and returns the
	// runtime-ready status. They never touch fields the user did not
	// explicitly pick — phrase_id, default_mode and threshold only get
	// overwritten when the request body provides a value.
	mux.HandleFunc("/app/wakeword/enable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if cfg == nil || state == nil {
			http.Error(w, "wake-word: runtime state not ready", http.StatusServiceUnavailable)
			return
		}
		var body wakewordEnableRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body) // empty body is OK
		}

		cfg.Wakeword.Enabled = true
		if v := strings.TrimSpace(body.Backend); v != "" {
			cfg.Wakeword.Backend = config.NormalizeWakewordBackend(v)
		} else if strings.TrimSpace(cfg.Wakeword.Backend) == "" {
			cfg.Wakeword.Backend = config.WakewordBackendSherpaKWS
		}
		if v := strings.TrimSpace(body.PhraseID); v != "" {
			cfg.Wakeword.PhraseID = v
		} else if strings.TrimSpace(cfg.Wakeword.PhraseID) == "" {
			cfg.Wakeword.PhraseID = "hey_quby"
		}
		if v := strings.TrimSpace(body.DefaultMode); v != "" {
			cfg.Wakeword.DefaultMode = config.NormalizeWakewordDefaultMode(v)
		} else if strings.TrimSpace(cfg.Wakeword.DefaultMode) == "" {
			cfg.Wakeword.DefaultMode = "voice_agent"
		}
		if body.Threshold > 0 && body.Threshold <= 1 {
			cfg.Wakeword.Threshold = body.Threshold
		}

		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("wakeword: save config failed", "err", err)
			http.Error(w, "wake-word: save config failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		state.mu.Lock()
		hkMgr := state.hkManager
		state.mu.Unlock()
		if mode, ok := hkMgr.(*modeHotkeyManager); ok {
			restartDesktopWakeword(r.Context(), cfg, state, mode)
		} else {
			slog.Warn("wakeword: hotkey manager unavailable — runtime will start on next launch")
		}

		writeWakewordStateResponse(w, cfg, state)
	})

	mux.HandleFunc("/app/wakeword/disable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if cfg == nil || state == nil {
			http.Error(w, "wake-word: runtime state not ready", http.StatusServiceUnavailable)
			return
		}
		cfg.Wakeword.Enabled = false
		if err := config.Save(cfgPath, cfg); err != nil {
			http.Error(w, "wake-word: save config failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		state.mu.Lock()
		hkMgr := state.hkManager
		state.mu.Unlock()
		if mode, ok := hkMgr.(*modeHotkeyManager); ok {
			restartDesktopWakeword(r.Context(), cfg, state, mode)
		}
		writeWakewordStateResponse(w, cfg, state)
	})

	mux.HandleFunc("/app/wakeword/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeWakewordStateResponse(w, cfg, state)
	})
}

func localSetupCompletionProblem(cfg *config.Config, installState *config.InstallState) string {
	if installState == nil || installState.Mode != config.InstallModeLocal {
		return ""
	}
	if cfg == nil {
		return "Local setup cannot be completed because the runtime config is unavailable."
	}
	modelPath := configuredLocalSTTModelPath(cfg)
	provider := stt.NewLocalProvider(cfg.Local.Port, modelPath, cfg.Local.GPU)
	status := provider.VerifyInstallation()
	if !status.BinaryFound {
		return "Local setup cannot be completed because the bundled whisper-server runtime is missing. Reinstall SpeechKit."
	}
	if !status.ModelFound {
		slog.Info("local setup completed without a ready speech model",
			"model_path", status.ModelPath,
			"problems", status.Problems)
	}
	return ""
}

type wakewordEnableRequest struct {
	Backend     string  `json:"backend,omitempty"`
	PhraseID    string  `json:"phraseId,omitempty"`
	DefaultMode string  `json:"defaultMode,omitempty"`
	Threshold   float64 `json:"threshold,omitempty"`
}

func writeWakewordStateResponse(w http.ResponseWriter, cfg *config.Config, state *appState) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp := map[string]any{
		"enabled":     false,
		"listening":   false,
		"backend":     config.WakewordBackendSherpaKWS,
		"phraseId":    "",
		"defaultMode": "voice_agent",
		"status":      "",
	}
	if cfg != nil {
		resp["enabled"] = cfg.Wakeword.Enabled
		resp["backend"] = config.NormalizeWakewordBackend(cfg.Wakeword.Backend)
		resp["phraseId"] = strings.TrimSpace(cfg.Wakeword.PhraseID)
		resp["defaultMode"] = config.NormalizeWakewordDefaultMode(cfg.Wakeword.DefaultMode)
		resp["threshold"] = cfg.Wakeword.Threshold
	}
	if state != nil {
		resp["listening"] = state.currentWakewordRuntime() != nil
		resp["status"] = state.wakewordStatusValue()
	}
	_ = json.NewEncoder(w).Encode(resp)
}
