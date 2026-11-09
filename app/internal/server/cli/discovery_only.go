//go:build linux

package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/core"
	"github.com/kombifyio/SpeechKit/app/internal/server/discovery"
)

type discoveryFlags struct {
	configPath, listen, modes, discoveryModes, instance, url string
	port                                                     int
	probe                                                    bool
}

// discoveryOnlyMain is `speechkit-server --discovery-only`: flag-driven, so
// no configuration file is needed. A file is read only when --config is
// given explicitly, and the --discovery-* flags override it. No HTTP
// listener, provider, store, secret or production-auth check is involved:
// nothing is served.
func discoveryOnlyMain(fs *flag.FlagSet, banner, version string, f discoveryFlags) int {
	if f.probe {
		if err := discovery.ProbeAnnounceOnly(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	explicit := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { explicit[fl.Name] = true })
	cfg := &config.Config{}
	if explicit["config"] {
		loaded, err := config.Load(f.configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: load config %q: %v\n", banner, f.configPath, err)
			return 2
		}
		cfg = loaded
	}
	modes := f.discoveryModes
	if modes == "" {
		modes = f.modes
	}
	if err := discovery.ApplyAnnounceOnlyOptions(cfg, discovery.AnnounceOnlyOptions{
		Listen: f.listen, Port: f.port, Modes: modes, Instance: f.instance, URL: f.url,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", banner, err)
		return 2
	}
	setupLogger(cfg)
	ctx, stop := core.NotifySignals(context.Background())
	defer stop()
	if err := discovery.RunAnnounceOnly(ctx, cfg, version, effectiveModes(cfg)); err != nil {
		slog.Error(banner+" discovery-only failed", "err", err)
		return 1
	}
	return 0
}
