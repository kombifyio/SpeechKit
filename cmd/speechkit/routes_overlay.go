package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func registerOverlayRoutes(mux *http.ServeMux, cfgPath string, cfg *config.Config, state *appState) {
	mux.HandleFunc("/overlay/pill-panel/show", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.showPillPanel()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/pill-panel/hide", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.hidePillPanel()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/radial/show", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.showRadialMenu()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/radial/hide", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.hideRadialMenu()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/show-dashboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		state.showDashboardWindow("overlay")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(state.overlaySnapshot())
	})
	mux.HandleFunc("/overlay/free-center", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		centerX, centerY, ok := parseOverlayFreeCenterRequest(r)
		if !ok {
			http.Error(w, "invalid overlay center", http.StatusBadRequest)
			return
		}
		if !state.moveOverlayFreeCenter(centerX, centerY) {
			http.Error(w, "overlay is not movable", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/overlay/free-center/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		centerX, centerY, ok := parseOverlayFreeCenterRequest(r)
		if !ok {
			http.Error(w, "invalid overlay center", http.StatusBadRequest)
			return
		}
		if !state.moveOverlayFreeCenter(centerX, centerY) {
			http.Error(w, "overlay is not movable", http.StatusConflict)
			return
		}
		savedCenterX, savedCenterY, monitorPositions := state.overlayFreeCenterState()
		cfg.UI.OverlayFreeX = savedCenterX
		cfg.UI.OverlayFreeY = savedCenterY
		cfg.UI.OverlayMonitorPositions = monitorPositions
		if err := config.Save(cfgPath, cfg); err != nil {
			http.Error(w, "failed to save overlay center", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	_ = cfg
}

func parseOverlayFreeCenterRequest(r *http.Request) (int, int, bool) {
	if err := r.ParseForm(); err != nil {
		return 0, 0, false
	}
	centerX, err := strconv.Atoi(strings.TrimSpace(r.FormValue("center_x")))
	if err != nil {
		return 0, 0, false
	}
	centerY, err := strconv.Atoi(strings.TrimSpace(r.FormValue("center_y")))
	if err != nil {
		return 0, 0, false
	}
	return centerX, centerY, true
}
