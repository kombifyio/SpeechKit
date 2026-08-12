//go:build linux

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	deviceagentserver "github.com/kombifyio/SpeechKit/internal/server/deviceagent"
	"github.com/kombifyio/SpeechKit/internal/server/deviceagent/claimstore"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

var errDeviceAgentTTSUnavailable = errors.New("device-agent bridge requires a ready TTS provider")

// wireDeviceAgentBridge builds the local-only, independently paired HA path.
// The returned ledger belongs to the caller and must remain open for the
// server lifetime. No route is mounted and no auth carve-out becomes active
// unless every dependency is ready.
func wireDeviceAgentBridge(ctx context.Context, cfg *config.Config, app *App) (*claimstore.Ledger, error) {
	if cfg == nil || app == nil || app.Mux == nil || app.Health == nil {
		return nil, errors.New("device-agent bridge requires initialized server state")
	}
	if !cfg.Server.DeviceAgent.Enabled {
		app.Health.SetReadyWithOptions("api.device_agent", StatusDisabled, "configured off", ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		return nil, nil
	}
	if err := config.ValidateServerProductionAuth(cfg); err != nil {
		return nil, fmt.Errorf("validate device-agent server configuration: %w", err)
	}
	if app.TTSRouter == nil || !app.TTSEnabled {
		return nil, errDeviceAgentTTSUnavailable
	}

	haToken := strings.TrimSpace(config.ResolveSecret(cfg.Assist.HomeAssistant.TokenEnv))
	if haToken == "" {
		return nil, fmt.Errorf("resolve Home Assistant token env %q: empty", cfg.Assist.HomeAssistant.TokenEnv)
	}
	ha, err := deviceagentserver.NewHomeAssistantClient(deviceagentserver.HomeAssistantOptions{
		BaseURL:  cfg.Assist.HomeAssistant.URL,
		Token:    haToken,
		AgentID:  cfg.Assist.HomeAssistant.AgentID,
		Language: cfg.Assist.HomeAssistant.Language,
	})
	if err != nil {
		return nil, fmt.Errorf("build Home Assistant client: %w", err)
	}

	settings := cfg.Server.DeviceAgent.EffectiveClaimSettings()
	store, err := claimstore.Open(ctx, claimstore.Options{
		Path:          cfg.Server.DeviceAgent.ClaimStorePath,
		MaxEntries:    settings.MaxClaims,
		Retention:     time.Duration(settings.ClaimRetentionSec) * time.Second,
		MaxRequestAge: time.Duration(settings.MaxRequestAgeSec) * time.Second,
		FutureSkew:    time.Duration(settings.FutureSkewSec) * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open durable claim ledger: %w", err)
	}
	closeOnError := func(cause error) (*claimstore.Ledger, error) {
		if closeErr := store.Close(); closeErr != nil {
			return nil, fmt.Errorf("%w (also close claim ledger: %w)", cause, closeErr)
		}
		return nil, cause
	}
	claims, err := deviceagentserver.NewDurableClaimLedger(store)
	if err != nil {
		return closeOnError(err)
	}

	bindings := make([]deviceagentserver.DeviceBindingOptions, 0, len(cfg.Server.DeviceAgent.Devices))
	rules := make([]deviceagentserver.RuleOptions, 0)
	for _, device := range cfg.Server.DeviceAgent.Devices {
		token := strings.TrimSpace(config.ResolveSecret(device.TokenEnv))
		if token == "" {
			return closeOnError(fmt.Errorf("device token env %q did not resolve", device.TokenEnv))
		}
		bindings = append(bindings, deviceagentserver.DeviceBindingOptions{
			PairingID:          device.PairingID,
			DeviceID:           device.DeviceID,
			RoomID:             device.RoomID,
			Token:              token,
			AllowedClientCIDRs: append([]string(nil), device.AllowedClientCIDRs...),
		})
		for _, rawRule := range device.LocalRules {
			notBefore, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(rawRule.NotBefore))
			if parseErr != nil {
				return closeOnError(fmt.Errorf("parse local rule %q not_before: %w", rawRule.RuleID, parseErr))
			}
			expiresAt, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(rawRule.ExpiresAt))
			if parseErr != nil {
				return closeOnError(fmt.Errorf("parse local rule %q expires_at: %w", rawRule.RuleID, parseErr))
			}
			rules = append(rules, deviceagentserver.RuleOptions{
				RuleID: rawRule.RuleID, DeviceID: device.DeviceID, RoomID: device.RoomID,
				TriggerText: rawRule.TriggerText, Locale: rawRule.Locale, Action: rawRule.Action,
				EntityID: rawRule.EntityID, NotBefore: notBefore, ExpiresAt: expiresAt,
			})
		}
	}
	policy, err := deviceagentserver.NewPolicy(rules...)
	if err != nil {
		return closeOnError(fmt.Errorf("build local command policy: %w", err))
	}

	bridge, err := deviceagentserver.NewBridge(deviceagentserver.BridgeOptions{
		ServerInstanceID: cfg.Server.DeviceAgent.ServerInstanceID,
		Bindings:         bindings,
		HomeAssistant:    ha,
		TTS:              app.TTSRouter,
		TTSReady:         app.TTSEnabled,
		Claims:           claims,
		Policy:           policy,
		MaxRequestAge:    time.Duration(settings.MaxRequestAgeSec) * time.Second,
		FutureSkew:       time.Duration(settings.FutureSkewSec) * time.Second,
	})
	if err != nil {
		return closeOnError(err)
	}
	bridge.Mount(app.Mux)
	app.DeviceAgentBridge = bridge
	app.DeviceAgentBridgeMounted = true
	app.Health.SetReady("api.device_agent", StatusOK, "local HA bridge listening")
	slog.Info("local device-agent bridge enabled",
		"protocol", "speechkit.device_agent.v1",
		"paired_devices", len(bindings),
		"routes", "/v1/device-agent/{register,events,assist,tts}")
	return store, nil
}

// deviceAgentAuthRoutes bypass only the general server credential so the
// handler can apply its independent per-device credential and direct-source
// CIDR checks. The API alias is deliberately absent.
func deviceAgentAuthRoutes() []middleware.PublicRoute {
	return []middleware.PublicRoute{
		{Path: "/v1/device-agent/register", Methods: []string{http.MethodPost}},
		{Path: "/v1/device-agent/events", Methods: []string{http.MethodPost}},
		{Path: "/v1/device-agent/assist", Methods: []string{http.MethodPost}},
		{Path: "/v1/device-agent/tts", Methods: []string{http.MethodPost}},
	}
}
