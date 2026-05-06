package main

import (
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
)

func registerSettingsCredentialRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router) {
	mux.HandleFunc("/settings/provider-credentials/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := saveProviderCredential(r.Context(), r.FormValue("provider"), r.FormValue("credential"), cfg, state, sttRouter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/provider-integrations/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		enabled := parseSettingsBool(r.FormValue("enabled"))
		message, err := updateProviderIntegration(r.Context(), cfgPath, r.FormValue("provider"), enabled, cfg, state, sttRouter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/provider-credentials/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := clearProviderCredential(r.Context(), r.FormValue("provider"), cfg, state, sttRouter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
	mux.HandleFunc("/settings/provider-credentials/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, msgFormParseError, http.StatusBadRequest)
			return
		}
		message, err := testProviderCredential(r.Context(), r.FormValue("provider"), r.FormValue("credential"), cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"message": message})
	})
}
