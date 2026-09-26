package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const serverDeviceAgentMinimumTokenBytes = 32

func validateServerDeviceAgent(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if !cfg.Server.DeviceAgent.Enabled {
		if cfg.Server.DeviceAgent.BoxMedia.Enabled {
			return fmt.Errorf("[server.device_agent.box_media] requires [server.device_agent].enabled=true")
		}
		return nil
	}

	bridge := cfg.Server.DeviceAgent
	if !validServerDeviceAgentID(bridge.ServerInstanceID) {
		return fmt.Errorf("[server.device_agent].server_instance_id must be a bounded stable local identifier")
	}
	if strings.TrimSpace(bridge.ClaimStorePath) == "" {
		return fmt.Errorf("[server.device_agent].claim_store_path is required when the device-agent bridge is enabled")
	}
	if err := validateServerDeviceAgentClaimSettings(bridge); err != nil {
		return err
	}
	if err := validateServerDeviceAgentHomeAssistant(cfg.Assist.HomeAssistant); err != nil {
		return err
	}
	if !cfg.TTS.Enabled || strings.ToLower(strings.TrimSpace(cfg.TTS.Strategy)) != "local-only" {
		return fmt.Errorf("[server.device_agent] requires [tts].enabled=true and strategy=local-only")
	}
	haTokenEnv := strings.TrimSpace(cfg.Assist.HomeAssistant.TokenEnv)
	haToken := strings.TrimSpace(ResolveSecret(haTokenEnv))
	if !validServerHomeAssistantToken(haToken) {
		return fmt.Errorf("[assist.home_assistant].token_env %q must resolve to a bounded Home Assistant bearer credential", haTokenEnv)
	}
	reservedEnvs := map[string]string{strings.ToUpper(haTokenEnv): "Home Assistant"}
	reservedValues := map[string]string{haToken: "Home Assistant"}
	addReservedCredential := func(scope, envName, value string) error {
		envName = strings.TrimSpace(envName)
		value = strings.TrimSpace(value)
		if envName != "" {
			key := strings.ToUpper(envName)
			if previous, exists := reservedEnvs[key]; exists {
				return fmt.Errorf("%s credential env %q must be distinct from %s", scope, envName, previous)
			}
			reservedEnvs[key] = scope
		}
		if value != "" {
			if previous, exists := reservedValues[value]; exists {
				return fmt.Errorf("%s credential must be distinct from %s", scope, previous)
			}
			reservedValues[value] = scope
		}
		return nil
	}
	authMode := strings.ToLower(strings.TrimSpace(cfg.Server.AuthMode))
	if authMode == "bearer" || authMode == "bearer_or_edge" || authMode == "bearer_or_oidc" {
		bearerEnv := strings.TrimSpace(cfg.Server.BearerTokenEnv)
		if bearerEnv == "" {
			bearerEnv = "SPEECHKIT_SERVER_TOKEN"
		}
		if err := addReservedCredential("general server bearer", bearerEnv, os.Getenv(bearerEnv)); err != nil {
			return err
		}
	}
	if authMode == "edge_hmac" || authMode == "bearer_or_edge" {
		edgeEnv := strings.TrimSpace(cfg.Server.EdgeAuthSecretEnv)
		if edgeEnv == "" {
			edgeEnv = "EDGE_AUTH_SECRET"
		}
		if err := addReservedCredential("edge HMAC", edgeEnv, os.Getenv(edgeEnv)); err != nil {
			return err
		}
	}
	if smokeEnv := strings.TrimSpace(cfg.Server.SmokeTokenEnv); smokeEnv != "" {
		if err := addReservedCredential("smoke", smokeEnv, os.Getenv(smokeEnv)); err != nil {
			return err
		}
	}
	if len(bridge.Devices) == 0 {
		return fmt.Errorf("[server.device_agent].devices must contain at least one paired device")
	}

	deviceIDs := make(map[string]struct{}, len(bridge.Devices))
	pairingIDs := make(map[string]struct{}, len(bridge.Devices))
	tokenEnvs := make(map[string]struct{}, len(bridge.Devices))
	resolvedTokens := make(map[string]string, len(bridge.Devices))
	ruleIDs := make(map[string]struct{})
	for index, device := range bridge.Devices {
		path := fmt.Sprintf("[server.device_agent.devices][%d]", index)
		deviceID := strings.TrimSpace(device.DeviceID)
		if !validServerDeviceAgentID(deviceID) {
			return fmt.Errorf("%s.device_id must be a bounded stable local identifier", path)
		}
		if _, exists := deviceIDs[deviceID]; exists {
			return fmt.Errorf("%s.device_id %q is duplicated", path, deviceID)
		}
		deviceIDs[deviceID] = struct{}{}

		pairingID := strings.TrimSpace(device.PairingID)
		if !validServerDeviceAgentID(pairingID) {
			return fmt.Errorf("%s.pairing_id must be a bounded stable local identifier", path)
		}
		if pairingID == deviceID {
			return fmt.Errorf("%s.pairing_id must be distinct from device_id and identify a non-recycled credential epoch", path)
		}
		if _, exists := pairingIDs[pairingID]; exists {
			return fmt.Errorf("%s.pairing_id %q is duplicated; pairing epochs must never be recycled", path, pairingID)
		}
		pairingIDs[pairingID] = struct{}{}

		if !validServerDeviceAgentID(device.RoomID) {
			return fmt.Errorf("%s.room_id must be a bounded stable local identifier and is authoritative for the paired device", path)
		}

		tokenEnv := strings.TrimSpace(device.TokenEnv)
		if tokenEnv == "" {
			return fmt.Errorf("%s.token_env is required", path)
		}
		if !validEnvName(tokenEnv) {
			return fmt.Errorf("%s.token_env must be a valid environment variable name", path)
		}
		// Environment variable names are case-insensitive on Windows. Treat
		// them that way everywhere so a config validated on one platform does
		// not become ambiguous on the Linux server.
		tokenEnvKey := strings.ToUpper(tokenEnv)
		if scope, exists := reservedEnvs[tokenEnvKey]; exists {
			return fmt.Errorf("%s.token_env must be distinct from the %s credential env", path, scope)
		}
		if _, exists := tokenEnvs[tokenEnvKey]; exists {
			return fmt.Errorf("%s.token_env %q is duplicated", path, tokenEnv)
		}
		tokenEnvs[tokenEnvKey] = struct{}{}
		reservedEnvs[tokenEnvKey] = "device " + deviceID

		resolvedToken := strings.TrimSpace(ResolveSecret(tokenEnv))
		if resolvedToken == "" {
			return fmt.Errorf("%s.token_env %q did not resolve to a device credential", path, tokenEnv)
		}
		if !validServerDeviceAgentToken(resolvedToken) {
			return fmt.Errorf("%s.token_env %q must resolve to a %d..512 byte bearer credential", path, tokenEnv, serverDeviceAgentMinimumTokenBytes)
		}
		if previousEnv, exists := resolvedTokens[resolvedToken]; exists {
			return fmt.Errorf("%s.token_env %q resolves to the same credential as %q; every device requires an independent token", path, tokenEnv, previousEnv)
		}
		if scope, exists := reservedValues[resolvedToken]; exists {
			return fmt.Errorf("%s.token_env %q must not reuse the %s credential", path, tokenEnv, scope)
		}
		reservedValues[resolvedToken] = "device " + deviceID
		resolvedTokens[resolvedToken] = tokenEnv

		if err := validateServerDeviceAgentCIDRs(path, device.AllowedClientCIDRs); err != nil {
			return err
		}
		if len(device.LocalRules) == 0 {
			return fmt.Errorf("%s.local_rules must contain at least one explicit time-bounded Tier-1 light rule", path)
		}
		for ruleIndex, rule := range device.LocalRules {
			rulePath := fmt.Sprintf("%s.local_rules[%d]", path, ruleIndex)
			if err := validateServerDeviceAgentLocalRule(rulePath, rule); err != nil {
				return err
			}
			ruleID := strings.TrimSpace(rule.RuleID)
			if _, exists := ruleIDs[ruleID]; exists {
				return fmt.Errorf("%s.rule_id %q is duplicated; local rule ids must be globally unique", rulePath, ruleID)
			}
			ruleIDs[ruleID] = struct{}{}
		}
	}
	return validateServerDeviceAgentBoxMedia(cfg, reservedEnvs, reservedValues)
}

func validateServerDeviceAgentLocalRule(path string, rule ServerDeviceAgentLocalRuleConfig) error {
	if !validServerDeviceAgentID(rule.RuleID) {
		return fmt.Errorf("%s.rule_id must be a bounded stable identifier", path)
	}
	trigger := strings.TrimSpace(rule.TriggerText)
	if trigger == "" || len(trigger) > 512 || strings.ContainsAny(trigger, "\x00\r\n") {
		return fmt.Errorf("%s.trigger_text must be one bounded single-line utterance", path)
	}
	if !validServerDeviceAgentLanguage(rule.Locale) {
		return fmt.Errorf("%s.locale must be a bounded language tag", path)
	}
	switch strings.ToLower(strings.TrimSpace(rule.Action)) {
	case "turn_on", "turn_off":
	default:
		return fmt.Errorf("%s.action must be turn_on or turn_off", path)
	}
	if !validServerDeviceAgentLightEntity(rule.EntityID) {
		return fmt.Errorf("%s.entity_id must name one explicit light.* entity", path)
	}
	notBefore, err := time.Parse(time.RFC3339, strings.TrimSpace(rule.NotBefore))
	if err != nil {
		return fmt.Errorf("%s.not_before must be RFC3339: %w", path, err)
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(rule.ExpiresAt))
	if err != nil {
		return fmt.Errorf("%s.expires_at must be RFC3339: %w", path, err)
	}
	if !expiresAt.After(notBefore) {
		return fmt.Errorf("%s.expires_at must be after not_before", path)
	}
	if expiresAt.Sub(notBefore) > 31*24*time.Hour {
		return fmt.Errorf("%s authorization window must not exceed 31 days", path)
	}
	return nil
}

func validateServerDeviceAgentClaimSettings(cfg ServerDeviceAgentConfig) error {
	raw := []struct {
		name  string
		value int
	}{
		{"max_request_age_sec", cfg.MaxRequestAgeSec},
		{"future_skew_sec", cfg.FutureSkewSec},
		{"claim_retention_sec", cfg.ClaimRetentionSec},
		{"max_claims", cfg.MaxClaims},
	}
	for _, field := range raw {
		if field.value < 0 {
			return fmt.Errorf("[server.device_agent].%s must be >= 0 (zero selects the safe default)", field.name)
		}
	}

	settings := cfg.EffectiveClaimSettings()
	if settings.MaxRequestAgeSec <= 0 || settings.MaxRequestAgeSec > MaxServerDeviceAgentRequestAgeSec {
		return fmt.Errorf("[server.device_agent].max_request_age_sec must resolve to 1..%d", MaxServerDeviceAgentRequestAgeSec)
	}
	if settings.FutureSkewSec < 0 || settings.FutureSkewSec > MaxServerDeviceAgentFutureSkewSec {
		return fmt.Errorf("[server.device_agent].future_skew_sec must resolve to 0..%d", MaxServerDeviceAgentFutureSkewSec)
	}
	if settings.ClaimRetentionSec <= 0 || settings.ClaimRetentionSec > MaxServerDeviceAgentClaimRetentionSec {
		return fmt.Errorf("[server.device_agent].claim_retention_sec must resolve to 1..%d", MaxServerDeviceAgentClaimRetentionSec)
	}
	if settings.ClaimRetentionSec <= settings.MaxRequestAgeSec+settings.FutureSkewSec {
		return fmt.Errorf("[server.device_agent].claim_retention_sec must be greater than max_request_age_sec + future_skew_sec")
	}
	if settings.MaxClaims <= 0 || settings.MaxClaims > MaxServerDeviceAgentClaims {
		return fmt.Errorf("[server.device_agent].max_claims must resolve to 1..%d", MaxServerDeviceAgentClaims)
	}
	return nil
}
