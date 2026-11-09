//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/cloudlink"
	"github.com/kombifyio/SpeechKit/app/internal/server/core"
)

// RunCloudLink is `speechkit-server cloud-link start|status|unlink`. It runs
// next to (or inside the container of) the serving process and shares its
// state directory: a link made here is picked up by the running server on
// its next tick, without a restart.
func RunCloudLink(args []string, opts Options) int {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "dev"
	}
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "usage: speechkit-server cloud-link start|status|unlink [--config path] [--json]")
	}
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	action := args[0]
	fs := flag.NewFlagSet("cloud-link "+action, flag.ContinueOnError)
	configPath := fs.String("config", defaultConfigPath(), "path to config.toml")
	asJSON := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cloud-link: load config %q: %v\n", *configPath, err)
		return 2
	}
	config.ApplyServerRuntimeDefaults(cfg)
	if _, err := config.ApplyServerModelSettingsFile(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "cloud-link: load server model settings: %v\n", err)
		return 2
	}
	if _, err := config.ApplyServerDeploymentEnv(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "cloud-link: apply deployment env: %v\n", err)
		return 2
	}
	settings, resolveErr := cloudlink.Resolve(cfg)
	manager := cloudlink.NewManager(settings, resolveErr, version, func() cloudlink.Report {
		return cloudlink.ReportFromConfig(cfg, version, effectiveModes(cfg))
	})
	ctx, stop := core.NotifySignals(context.Background())
	defer stop()

	switch action {
	case "status":
		st := manager.Status()
		if *asJSON {
			return printJSON(st)
		}
		printStatus(st)
		return 0
	case "start":
		return cloudLinkStart(ctx, manager, *asJSON)
	case "unlink":
		res, err := manager.Unlink(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cloud-link unlink:", err)
			return 1
		}
		if *asJSON {
			return printJSON(res)
		}
		switch {
		case !res.WasLinked:
			fmt.Println("This server was not linked; nothing to remove.")
		default:
			fmt.Printf("Unlinked: local credential removed, registry entry removed: %t, refresh token revoked: %t.\n",
				res.RegistryRemoved, res.TokenRevoked)
		}
		if res.Warning != "" {
			fmt.Fprintln(os.Stderr, "warning:", res.Warning)
		}
		return 0
	default:
		usage(os.Stderr)
		return 2
	}
}

func cloudLinkStart(ctx context.Context, manager *cloudlink.Manager, asJSON bool) int {
	da, err := manager.Begin(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cloud-link start:", err)
		return 1
	}
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(da)
	} else {
		fmt.Printf("To link this server to your account, open\n\n  %s\n\nand confirm the code %s (valid until %s).\n",
			da.VerificationURIComplete, da.UserCode, da.ExpiresAt.Local().Format(time.Kitchen))
		fmt.Println("Waiting for approval; Ctrl-C stops waiting.")
	}
	if err := manager.Complete(ctx, da); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "cloud-link start: stopped before approval; nothing was stored")
			return 130
		}
		fmt.Fprintln(os.Stderr, "cloud-link start:", err)
		return 1
	}
	poll, err := manager.CheckIn(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cloud-link start: linked, but the first check-in failed:", err)
		fmt.Fprintln(os.Stderr, "the running server retries it; see `speechkit-server cloud-link status`")
		return 1
	}
	st := manager.Status()
	fmt.Printf("Linked as %s; first check-in accepted, next in %s.\n", st.DeviceID, poll)
	return 0
}

func printStatus(st cloudlink.Status) {
	fmt.Printf("state: %s\n", st.State)
	if st.Reason != "" {
		fmt.Printf("reason: %s\n", st.Reason)
	}
	if st.DeviceID != "" {
		fmt.Printf("device id: %s\n", st.DeviceID)
	}
	if st.LinkedAt != nil {
		fmt.Printf("linked at: %s\n", st.LinkedAt.Format(time.RFC3339))
	}
	if st.LastCheckInAt != nil {
		fmt.Printf("last check-in (this process): %s\n", st.LastCheckInAt.Format(time.RFC3339))
	}
	if st.LastError != "" {
		fmt.Printf("last error: %s\n", st.LastError)
	}
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return 1
	}
	return 0
}
