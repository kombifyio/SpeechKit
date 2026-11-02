//go:build linux

package core

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/config"
)

// The release channels a process can serve. Early Access is the second of
// kombify's three stages (Local, Early Access, Stable; D-2026-10-30-02): it is
// the real server on production configuration and the production database at
// Stable's address, without the background load Stable carries
// (D-2026-10-30-04).
const (
	releaseChannelStable      = "stable"
	releaseChannelEarlyAccess = "early-access"
)

// resolveReleaseChannel reads KOMBIFY_RELEASE_CHANNEL; unset means stable and
// anything unknown refuses to start rather than running as stable.
func resolveReleaseChannel() (string, error) {
	switch channel := strings.ToLower(strings.TrimSpace(os.Getenv("KOMBIFY_RELEASE_CHANNEL"))); channel {
	case "", releaseChannelStable:
		return releaseChannelStable, nil
	case releaseChannelEarlyAccess:
		return channel, nil
	}
	return "", fmt.Errorf("core.Run: unknown release channel")
}

// EarlyAccess reports whether this process is the Early Access service.
func (a *App) EarlyAccess() bool { return a != nil && a.ReleaseChannel == releaseChannelEarlyAccess }

// applyEarlyAccessBounds keeps the Early Access service to what a free-plan
// instance (512 MB, 0.1 CPU) can serve for the people who opted in. Hosted
// providers, request-time usage metering and the production store stay; what
// goes is everything that runs without a request or needs a local runtime:
//   - the host-local whisper runtime, including its registration in the STT
//     router (a registered local provider is tried first for short audio);
//   - the Box media listener and its supervisor, which start that runtime;
//   - the on-box wake-word sidecar (buildWakeVerifier, the cloud pass decides);
//   - the Wyoming TCP listener;
//   - boot-time warm-up of the shared AI dependencies;
//   - saving audio files, which would sit on this instance beyond the reach
//     of the retention that Stable applies to the shared database.
//
// The native voice-provider cleanup loop and the store's retention passes are
// switched off where they start (buildVoiceAgentHandler, ensureStore). A
// Postgres store must run as a runtime role, because Early Access never
// migrates: its schema is migrated by the release path before it serves.
func applyEarlyAccessBounds(cfg *config.Config) error {
	// ensureStore trims the backend name before it selects the store, so the
	// admission trims it too.
	if strings.TrimSpace(cfg.Store.Backend) == "postgres" && !cfg.Store.PostgresRuntimeRole {
		return fmt.Errorf("core.Run: early-access requires store.postgres_runtime_role; it never migrates")
	}
	if err := requireProductionDatabase(cfg); err != nil {
		return err
	}
	cfg.Local.Enabled = false
	cfg.Server.DeviceAgent.BoxMedia.Enabled = false
	cfg.Server.Wyoming.Enabled = false
	cfg.VoiceAgent.Wake.LocalSTTModel = ""
	cfg.General.EagerWarmup = false
	// Audio files land on the instance disk, which is ephemeral on the
	// free plan and which Stable cannot reach to apply retention to.
	cfg.Store.SaveAudio = false
	slog.Info("early access runs no background work for tenants", "event", "early_access_background_disabled",
		"local_stt", "off", "box_media", "off", "wake_sidecar", "off", "wyoming", "off",
		"native_cleanup", "off", "store_retention", "off", "save_audio", "off", "migration", "never")
	return nil
}

// requireProductionDatabase is the Early Access counterpart of a stage's
// non-production plane check: Early Access runs production configuration on
// the production database (D-2026-10-30-04), so a process whose Postgres DSN
// names anything but a kombify-prd-* ClickHouse Managed Postgres host refuses
// to start instead of serving real people from synthetic data. /healthz reports
// plane=production only after this passed.
func requireProductionDatabase(cfg *config.Config) error {
	if strings.TrimSpace(cfg.Store.Backend) != "postgres" {
		return fmt.Errorf("core.Run: early-access requires the production Postgres store")
	}
	parsed, err := url.Parse(strings.TrimSpace(cfg.Store.PostgresDSN))
	host := ""
	if err == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	if err != nil || strings.Contains(parsed.Host, ",") || parsed.Query().Has("host") || parsed.Query().Has("hostaddr") ||
		parsed.Query().Has("service") || !strings.HasPrefix(host, "kombify-prd-") || !strings.HasSuffix(host, ".pg.clickhouse.cloud") {
		return fmt.Errorf("core.Run: early-access requires the production database, never kombify-stg-pg")
	}
	return nil
}

// earlyAccessExecutionScope is the health projection of the boundaries
// applyEarlyAccessBounds and the two switches installed.
func earlyAccessExecutionScope() map[string]string {
	return map[string]string{"http": "opted-in-users", "plane": "production", "startup": "verify-only", "scheduled": "disabled", "queues": "disabled"}
}

// serverRevision is the source revision /healthz reports on every channel:
// KOMBIFY_SPEECHKIT_REVISION, else the native Render build's commit, else
// empty for a process that is not revision-bound.
func serverRevision() string {
	for _, name := range []string{"KOMBIFY_SPEECHKIT_REVISION", "RENDER_GIT_COMMIT"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
