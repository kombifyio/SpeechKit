//go:build linux

package core

import (
	"log/slog"
	"net/http/pprof"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// registerPprof mounts the net/http/pprof handlers on app.Mux when
// [server.debug] pprof_enabled is set.
//
// pprof exposes process internals (goroutines, heap, CPU profiles), so it is
// OFF by default. When enabled it still sits behind the normal auth middleware
// chain, and it additionally refuses to mount on a non-loopback listener unless
// the operator explicitly also sets pprof_public — defence in depth so a
// debug flag left on in production does not silently expose profiles.
func registerPprof(app *App) {
	if app == nil || app.Mux == nil || app.Cfg == nil {
		return
	}
	dbg := app.Cfg.Server.Debug
	if !dbg.PprofEnabled {
		return
	}
	if !config.IsLoopbackListenAddr(app.Cfg.Server.ListenAddr) && !dbg.PprofPublic {
		slog.Warn("pprof enabled but refused on a non-loopback listener; set [server.debug] pprof_public=true to override",
			"listen_addr", app.Cfg.Server.ListenAddr)
		return
	}

	app.Mux.HandleFunc("/debug/pprof/", pprof.Index)
	app.Mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	app.Mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	app.Mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	app.Mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	slog.Warn("pprof debug endpoints enabled at /debug/pprof/ (auth-gated)", "public", dbg.PprofPublic)
}
