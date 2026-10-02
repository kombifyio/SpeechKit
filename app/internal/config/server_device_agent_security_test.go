package config

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestServerDeviceAgentConfigTOMLShape(t *testing.T) {
	var cfg Config
	_, err := toml.Decode(`
[server.device_agent]
enabled = true
server_instance_id = "speechkit-home-01"
claim_store_path = "/var/lib/speechkit/device-agent-claims.db"
max_request_age_sec = 300
future_skew_sec = 30
claim_retention_sec = 3600
max_claims = 2048

[server.device_agent.box_media]
enabled = true
listen_addr = "192.168.10.10:8444"
certificate_file = "/etc/speechkit/box-media.crt"
private_key_file = "/etc/speechkit/box-media.key"
pinned_ca_file = "/etc/speechkit/box-media-ca.crt"
pinned_ca_sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
token_env = "SPEECHKIT_BOX_MEDIA_TOKEN"
device_id = "kitchen-speaker"
pairing_id = "pairing-kitchen-2026-07"
room_id = "kitchen"
transcript = "turn off the kitchen light"
command_id = "kitchen-light-off"
locale = "en-US"

[[server.device_agent.devices]]
device_id = "kitchen-speaker"
pairing_id = "pairing-kitchen-2026-07"
room_id = "kitchen"
token_env = "SPEECHKIT_DEVICE_KITCHEN_TOKEN"
allowed_client_cidrs = ["192.168.10.42/32"]

[[server.device_agent.devices.local_rules]]
rule_id = "kitchen-light-off"
trigger_text = "turn off the kitchen light"
locale = "en-US"
action = "turn_off"
entity_id = "light.kitchen"
not_before = "2026-07-01T00:00:00Z"
expires_at = "2026-07-31T00:00:00Z"
`, &cfg)
	if err != nil {
		t.Fatalf("decode device-agent config: %v", err)
	}
	bridge := cfg.Server.DeviceAgent
	if !bridge.Enabled || bridge.ServerInstanceID != "speechkit-home-01" || bridge.ClaimStorePath == "" {
		t.Fatalf("decoded bridge = %+v", bridge)
	}
	if bridge.MaxRequestAgeSec != 300 || bridge.FutureSkewSec != 30 || bridge.ClaimRetentionSec != 3600 || bridge.MaxClaims != 2048 {
		t.Fatalf("decoded claim settings = %+v", bridge.EffectiveClaimSettings())
	}
	if media := bridge.BoxMedia; !media.Enabled || media.ListenAddr != "192.168.10.10:8444" || media.TokenEnv != "SPEECHKIT_BOX_MEDIA_TOKEN" || media.CommandID != "kitchen-light-off" {
		t.Fatalf("decoded Box media config = %+v", media)
	}
	if len(bridge.Devices) != 1 {
		t.Fatalf("decoded devices = %d, want 1", len(bridge.Devices))
	}
	device := bridge.Devices[0]
	if device.DeviceID != "kitchen-speaker" || device.PairingID != "pairing-kitchen-2026-07" || device.RoomID != "kitchen" || device.TokenEnv != "SPEECHKIT_DEVICE_KITCHEN_TOKEN" {
		t.Fatalf("decoded device = %+v", device)
	}
	if len(device.AllowedClientCIDRs) != 1 || device.AllowedClientCIDRs[0] != "192.168.10.42/32" {
		t.Fatalf("decoded allowed CIDRs = %#v", device.AllowedClientCIDRs)
	}
	if len(device.LocalRules) != 1 || device.LocalRules[0].RuleID != "kitchen-light-off" || device.LocalRules[0].EntityID != "light.kitchen" {
		t.Fatalf("decoded local rules = %#v", device.LocalRules)
	}
}

func TestServerDeviceAgentEffectiveClaimSettingsDefaults(t *testing.T) {
	settings := (ServerDeviceAgentConfig{}).EffectiveClaimSettings()
	if settings.MaxRequestAgeSec != DefaultServerDeviceAgentMaxRequestAgeSec {
		t.Fatalf("MaxRequestAgeSec = %d", settings.MaxRequestAgeSec)
	}
	if settings.FutureSkewSec != DefaultServerDeviceAgentFutureSkewSec {
		t.Fatalf("FutureSkewSec = %d", settings.FutureSkewSec)
	}
	if settings.ClaimRetentionSec != DefaultServerDeviceAgentClaimRetentionSec {
		t.Fatalf("ClaimRetentionSec = %d", settings.ClaimRetentionSec)
	}
	if settings.MaxClaims != DefaultServerDeviceAgentMaxClaims {
		t.Fatalf("MaxClaims = %d", settings.MaxClaims)
	}
}

func TestValidateServerProductionAuthAcceptsValidDeviceAgentConfig(t *testing.T) {
	cfg := validServerDeviceAgentConfig(t)
	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth: %v", err)
	}
}

func TestValidateServerProductionAuthLeavesDisabledDeviceAgentInert(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			ListenAddr: "127.0.0.1:8080",
			AuthMode:   "none",
			DeviceAgent: ServerDeviceAgentConfig{
				Enabled: true,
			},
		},
	}
	cfg.Server.DeviceAgent.Enabled = false
	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("disabled bridge should impose no config requirements: %v", err)
	}
}

func TestValidateServerProductionAuthRejectsBoxMediaWithoutDeviceAgent(t *testing.T) {
	cfg := &Config{Server: ServerConfig{ListenAddr: "127.0.0.1:8080", AuthMode: "none"}}
	cfg.Server.DeviceAgent.BoxMedia.Enabled = true
	if err := ValidateServerProductionAuth(cfg); err == nil || !strings.Contains(err.Error(), "requires [server.device_agent].enabled=true") {
		t.Fatalf("Box media without device-agent error = %v", err)
	}
}

func TestValidateServerProductionAuthAcceptsValidBoxMediaConfig(t *testing.T) {
	cfg := validServerDeviceAgentConfig(t)
	enableValidBoxMediaConfig(t, cfg)
	// The selected Box binding may retain additional generic Device-Agent
	// source ranges, but at least one direct RFC1918 IPv4 prefix is mandatory.
	cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = append(
		cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs,
		"127.0.0.1/32",
	)
	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth: %v", err)
	}
}

func TestValidateServerProductionAuthRejectsIncompleteBoxMediaConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *Config)
		want   string
	}{
		{"local STT disabled", func(_ *testing.T, cfg *Config) { cfg.Local.Enabled = false }, "[local].enabled=true"},
		{"relative local model", func(_ *testing.T, cfg *Config) { cfg.Local.ModelPath = "ggml-small.bin" }, "model_path"},
		{"invalid local port", func(_ *testing.T, cfg *Config) { cfg.Local.Port = 0 }, "[local].port"},
		{"wildcard listener", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.ListenAddr = ":8444" }, "listen_addr"},
		{"loopback listener", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.ListenAddr = "127.0.0.1:8444" }, "RFC1918 IPv4"},
		{"link-local listener", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.ListenAddr = "169.254.10.20:8444" }, "RFC1918 IPv4"},
		{"CGNAT listener", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.ListenAddr = "100.64.10.20:8444" }, "RFC1918 IPv4"},
		{"IPv6 ULA listener", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.ListenAddr = "[fd00::10]:8444" }, "RFC1918 IPv4"},
		{"IPv6 loopback listener", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.ListenAddr = "[::1]:8444" }, "RFC1918 IPv4"},
		{"public listener", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.ListenAddr = "203.0.113.10:8444" }, "RFC1918 IPv4"},
		{"selected binding loopback only", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = []string{"127.0.0.1/32"}
		}, "selected device allowed_client_cidrs"},
		{"selected binding link-local only", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = []string{"169.254.10.0/24"}
		}, "selected device allowed_client_cidrs"},
		{"selected binding CGNAT only", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = []string{"100.64.10.0/24"}
		}, "selected device allowed_client_cidrs"},
		{"selected binding IPv6 loopback only", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = []string{"::1/128"}
		}, "selected device allowed_client_cidrs"},
		{"selected binding IPv6 ULA only", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = []string{"fd00::/64"}
		}, "selected device allowed_client_cidrs"},
		{"relative certificate", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.CertificateFile = "server.crt" }, "certificate_file"},
		{"reused key path", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.BoxMedia.PrivateKeyFile = cfg.Server.DeviceAgent.BoxMedia.CertificateFile
		}, "must be distinct"},
		{"bad pinned CA digest", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.BoxMedia.PinnedCASHA256 = strings.Repeat("A", 64)
		}, "pinned_ca_sha256"},
		{"unresolved media token", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.TokenEnv = "TEST_BOX_MEDIA_MISSING" }, "must resolve"},
		{"device token env reused", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.BoxMedia.TokenEnv = cfg.Server.DeviceAgent.Devices[0].TokenEnv
		}, "credential env"},
		{"device token value reused", func(t *testing.T, _ *Config) {
			t.Setenv("TEST_BOX_MEDIA_TOKEN", "device-token-one-0123456789abcdef")
		}, "device kitchen-speaker credential"},
		{"unknown device", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.DeviceID = "other-device" }, "existing paired device"},
		{"wrong pairing epoch", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.PairingID = "pairing-old" }, "current pairing epoch"},
		{"unknown command", func(_ *testing.T, cfg *Config) { cfg.Server.DeviceAgent.BoxMedia.CommandID = "other-command" }, "existing G0 rule"},
		{"different transcript", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.BoxMedia.Transcript = "turn on the kitchen light"
		}, "exactly match"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validServerDeviceAgentConfig(t)
			enableValidBoxMediaConfig(t, cfg)
			tc.mutate(t, cfg)
			err := ValidateServerProductionAuth(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateServerProductionAuth error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateServerProductionAuthRejectsIncompleteDeviceAgentConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"missing server instance", func(cfg *Config) { cfg.Server.DeviceAgent.ServerInstanceID = "" }, "server_instance_id"},
		{"missing claim store", func(cfg *Config) { cfg.Server.DeviceAgent.ClaimStorePath = "" }, "claim_store_path"},
		{"missing HA URL", func(cfg *Config) { cfg.Assist.HomeAssistant.URL = "" }, "home_assistant].url"},
		{"missing HA token env", func(cfg *Config) { cfg.Assist.HomeAssistant.TokenEnv = "" }, "home_assistant].token_env"},
		{"unresolved HA token", func(cfg *Config) { cfg.Assist.HomeAssistant.TokenEnv = "TEST_HOME_ASSISTANT_UNRESOLVED" }, "must resolve"},
		{"TTS disabled", func(cfg *Config) { cfg.TTS.Enabled = false }, "strategy=local-only"},
		{"cloud TTS strategy", func(cfg *Config) { cfg.TTS.Strategy = "cloud-first" }, "strategy=local-only"},
		{"missing devices", func(cfg *Config) { cfg.Server.DeviceAgent.Devices = nil }, "at least one paired device"},
		{"missing device id", func(cfg *Config) { cfg.Server.DeviceAgent.Devices[0].DeviceID = "" }, "device_id"},
		{"missing pairing id", func(cfg *Config) { cfg.Server.DeviceAgent.Devices[0].PairingID = "" }, "pairing_id"},
		{"pairing id equals device id", func(cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].PairingID = cfg.Server.DeviceAgent.Devices[0].DeviceID
		}, "distinct from device_id"},
		{"missing room", func(cfg *Config) { cfg.Server.DeviceAgent.Devices[0].RoomID = "" }, "room_id"},
		{"missing token env", func(cfg *Config) { cfg.Server.DeviceAgent.Devices[0].TokenEnv = "" }, "token_env"},
		{"unresolved token", func(cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].TokenEnv = "TEST_DEVICE_AGENT_UNRESOLVED_TOKEN"
		}, "did not resolve"},
		{"missing CIDRs", func(cfg *Config) { cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = nil }, "at least one explicit local CIDR"},
		{"missing local rules", func(cfg *Config) { cfg.Server.DeviceAgent.Devices[0].LocalRules = nil }, "local_rules"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validServerDeviceAgentConfig(t)
			tc.mutate(cfg)
			err := ValidateServerProductionAuth(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateServerProductionAuth error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateServerProductionAuthRejectsAmbiguousDeviceBindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *Config)
		want   string
	}{
		{"duplicate device id", func(t *testing.T, cfg *Config) {
			appendSecondDevice(t, cfg)
			cfg.Server.DeviceAgent.Devices[1].DeviceID = cfg.Server.DeviceAgent.Devices[0].DeviceID
		}, "device_id"},
		{"duplicate pairing id", func(t *testing.T, cfg *Config) {
			appendSecondDevice(t, cfg)
			cfg.Server.DeviceAgent.Devices[1].PairingID = cfg.Server.DeviceAgent.Devices[0].PairingID
		}, "pairing_id"},
		{"duplicate token env case insensitive", func(t *testing.T, cfg *Config) {
			appendSecondDevice(t, cfg)
			cfg.Server.DeviceAgent.Devices[1].TokenEnv = strings.ToLower(cfg.Server.DeviceAgent.Devices[0].TokenEnv)
		}, "token_env"},
		{"duplicate resolved token", func(t *testing.T, cfg *Config) {
			appendSecondDevice(t, cfg)
			t.Setenv("TEST_DEVICE_AGENT_TOKEN_TWO", "device-token-one-0123456789abcdef")
		}, "same credential"},
		{"HA token env reused", func(_ *testing.T, cfg *Config) {
			cfg.Server.DeviceAgent.Devices[0].TokenEnv = cfg.Assist.HomeAssistant.TokenEnv
		}, "credential env"},
		{"HA token value reused", func(t *testing.T, cfg *Config) {
			t.Setenv("TEST_DEVICE_AGENT_TOKEN_ONE", "home-assistant-token-0123456789abcdef")
		}, "Home Assistant credential"},
		{"general server bearer reused by device", func(t *testing.T, cfg *Config) {
			cfg.Server.AuthMode = "bearer"
			cfg.Server.BearerTokenEnv = "TEST_GENERAL_SERVER_TOKEN"
			t.Setenv("TEST_GENERAL_SERVER_TOKEN", "device-token-one-0123456789abcdef")
		}, "general server bearer"},
		{"general server bearer reused by HA", func(t *testing.T, cfg *Config) {
			cfg.Server.AuthMode = "bearer"
			cfg.Server.BearerTokenEnv = "TEST_GENERAL_SERVER_TOKEN"
			t.Setenv("TEST_GENERAL_SERVER_TOKEN", "home-assistant-token-0123456789abcdef")
		}, "distinct from Home Assistant"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validServerDeviceAgentConfig(t)
			tc.mutate(t, cfg)
			err := ValidateServerProductionAuth(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateServerProductionAuth error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateServerProductionAuthRejectsUnsafeDeviceAgentCIDRs(t *testing.T) {
	tests := []string{
		"",
		"not-a-cidr",
		"0.0.0.0/0",
		"10.0.0.0/7",
		"8.8.8.0/24",
		"::/0",
		"2001:4860::/32",
	}
	for _, cidr := range tests {
		name := cidr
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			cfg := validServerDeviceAgentConfig(t)
			cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = []string{cidr}
			err := ValidateServerProductionAuth(cfg)
			if err == nil || !strings.Contains(err.Error(), "allowed_client_cidrs") {
				t.Fatalf("ValidateServerProductionAuth error = %v, want CIDR rejection", err)
			}
		})
	}
}

func TestValidateServerProductionAuthAcceptsSupportedLocalDeviceAgentCIDRs(t *testing.T) {
	for _, cidr := range []string{
		"127.0.0.1/32",
		"10.20.0.0/16",
		"172.16.5.10/32",
		"192.168.50.0/24",
		"100.64.10.0/24",
		"169.254.20.1/32",
		"::1/128",
		"fd00:1234::/64",
		"fe80::/64",
	} {
		t.Run(cidr, func(t *testing.T) {
			cfg := validServerDeviceAgentConfig(t)
			cfg.Server.DeviceAgent.Devices[0].AllowedClientCIDRs = []string{cidr}
			if err := ValidateServerProductionAuth(cfg); err != nil {
				t.Fatalf("ValidateServerProductionAuth(%q): %v", cidr, err)
			}
		})
	}
}

func TestValidateServerProductionAuthRejectsUnsafeDeviceAgentHAURL(t *testing.T) {
	userInfoURL := (&url.URL{
		Scheme: "http",
		User:   url.User("embedded-user"),
		Host:   "192.168.1.5:8123",
	}).String()
	tests := []string{
		"ha.local:8123",
		"ftp://192.168.1.5",
		userInfoURL,
		"http://8.8.8.8:8123",
		"http://0.0.0.0:8123",
		"http://192.168.1.5:8123/?target=elsewhere",
		"http://192.168.1.5:8123/#fragment",
		"http://192.168.1.5:8123/a/../b",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			cfg := validServerDeviceAgentConfig(t)
			cfg.Assist.HomeAssistant.URL = rawURL
			err := ValidateServerProductionAuth(cfg)
			if err == nil || !strings.Contains(err.Error(), "home_assistant].url") {
				t.Fatalf("ValidateServerProductionAuth error = %v, want HA URL rejection", err)
			}
		})
	}
}

func TestValidateServerProductionAuthValidatesDeviceAgentClaimBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ServerDeviceAgentConfig)
		want   string
	}{
		{"negative age", func(cfg *ServerDeviceAgentConfig) { cfg.MaxRequestAgeSec = -1 }, "max_request_age_sec"},
		{"negative skew", func(cfg *ServerDeviceAgentConfig) { cfg.FutureSkewSec = -1 }, "future_skew_sec"},
		{"negative retention", func(cfg *ServerDeviceAgentConfig) { cfg.ClaimRetentionSec = -1 }, "claim_retention_sec"},
		{"negative claims", func(cfg *ServerDeviceAgentConfig) { cfg.MaxClaims = -1 }, "max_claims"},
		{"age over cap", func(cfg *ServerDeviceAgentConfig) { cfg.MaxRequestAgeSec = MaxServerDeviceAgentRequestAgeSec + 1 }, "max_request_age_sec"},
		{"skew over cap", func(cfg *ServerDeviceAgentConfig) { cfg.FutureSkewSec = MaxServerDeviceAgentFutureSkewSec + 1 }, "future_skew_sec"},
		{"retention over cap", func(cfg *ServerDeviceAgentConfig) { cfg.ClaimRetentionSec = MaxServerDeviceAgentClaimRetentionSec + 1 }, "claim_retention_sec"},
		{"claims over cap", func(cfg *ServerDeviceAgentConfig) { cfg.MaxClaims = MaxServerDeviceAgentClaims + 1 }, "max_claims"},
		{"retention equals window", func(cfg *ServerDeviceAgentConfig) {
			cfg.MaxRequestAgeSec = 60
			cfg.FutureSkewSec = 10
			cfg.ClaimRetentionSec = 70
		}, "greater than"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validServerDeviceAgentConfig(t)
			tc.mutate(&cfg.Server.DeviceAgent)
			err := ValidateServerProductionAuth(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateServerProductionAuth error = %v, want %q", err, tc.want)
			}
		})
	}
}

func validServerDeviceAgentConfig(t *testing.T) *Config {
	t.Helper()
	// Keep missing-token tests hermetic even on workstations with Doppler CLI
	// and managed build defaults available.
	t.Setenv("DOPPLER_PROJECT", "")
	t.Setenv("DOPPLER_CONFIG", "")
	t.Setenv("TEST_DEVICE_AGENT_TOKEN_ONE", "device-token-one-0123456789abcdef")
	t.Setenv("TEST_HOME_ASSISTANT_TOKEN", "home-assistant-token-0123456789abcdef")
	return &Config{
		TTS: TTSConfig{Enabled: true, Strategy: "local-only"},
		Assist: AssistConfig{HomeAssistant: AssistHomeAssistantConfig{
			URL:      "http://127.0.0.1:8123",
			TokenEnv: "TEST_HOME_ASSISTANT_TOKEN",
			Language: "en",
		}},
		Server: ServerConfig{
			ListenAddr: "127.0.0.1:8080",
			AuthMode:   "none",
			DeviceAgent: ServerDeviceAgentConfig{
				Enabled:          true,
				ServerInstanceID: "speechkit-home-01",
				ClaimStorePath:   "/var/lib/speechkit/device-agent-claims.db",
				Devices: []ServerDeviceAgentDeviceConfig{{
					DeviceID:           "kitchen-speaker",
					PairingID:          "pairing-kitchen-2026-07",
					RoomID:             "kitchen",
					TokenEnv:           "TEST_DEVICE_AGENT_TOKEN_ONE",
					AllowedClientCIDRs: []string{"192.168.10.42/32"},
					LocalRules: []ServerDeviceAgentLocalRuleConfig{{
						RuleID: "kitchen-light-off", TriggerText: "turn off the kitchen light", Locale: "en-US",
						Action: "turn_off", EntityID: "light.kitchen",
						NotBefore: "2026-07-01T00:00:00Z", ExpiresAt: "2026-07-31T00:00:00Z",
					}},
				}},
			},
		},
	}
}

func appendSecondDevice(t *testing.T, cfg *Config) {
	t.Helper()
	t.Setenv("TEST_DEVICE_AGENT_TOKEN_TWO", "device-token-two-0123456789abcdef")
	cfg.Server.DeviceAgent.Devices = append(cfg.Server.DeviceAgent.Devices, ServerDeviceAgentDeviceConfig{
		DeviceID:           "office-speaker",
		PairingID:          "pairing-office-2026-07",
		RoomID:             "office",
		TokenEnv:           "TEST_DEVICE_AGENT_TOKEN_TWO",
		AllowedClientCIDRs: []string{"192.168.10.43/32"},
		LocalRules: []ServerDeviceAgentLocalRuleConfig{{
			RuleID: "office-light-off", TriggerText: "turn off the office light", Locale: "en-US",
			Action: "turn_off", EntityID: "light.office",
			NotBefore: "2026-07-01T00:00:00Z", ExpiresAt: "2026-07-31T00:00:00Z",
		}},
	})
}

func enableValidBoxMediaConfig(t *testing.T, cfg *Config) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TEST_BOX_MEDIA_TOKEN", "box-media-token-0123456789abcdefghijkl")
	cfg.Local = LocalConfig{
		Enabled: true, ModelPath: filepath.Join(root, "ggml-small.bin"), Port: 9000, GPU: "cpu",
	}
	cfg.Server.DeviceAgent.BoxMedia = ServerDeviceAgentBoxMediaConfig{
		Enabled: true, ListenAddr: "192.168.10.10:8444",
		CertificateFile: filepath.Join(root, "box-media.crt"),
		PrivateKeyFile:  filepath.Join(root, "box-media.key"),
		PinnedCAFile:    filepath.Join(root, "box-media-ca.crt"),
		PinnedCASHA256:  strings.Repeat("a", 64),
		TokenEnv:        "TEST_BOX_MEDIA_TOKEN",
		DeviceID:        "kitchen-speaker",
		PairingID:       "pairing-kitchen-2026-07",
		RoomID:          "kitchen",
		Transcript:      "turn off the kitchen light",
		CommandID:       "kitchen-light-off",
		Locale:          "en-US",
	}
}
