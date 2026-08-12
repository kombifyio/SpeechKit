package config

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const AllowInsecureNoAuthEnv = "SPEECHKIT_ALLOW_INSECURE_NO_AUTH"

const serverDeviceAgentMinimumTokenBytes = 32

// ValidateServerProductionAuth rejects accidental public no-auth server binds.
// auth_mode=none remains available for local development and explicit tests.
func ValidateServerProductionAuth(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if err := validateServerResourceLimits(cfg); err != nil {
		return err
	}
	if err := validateServerDeviceAgent(cfg); err != nil {
		return err
	}
	authMode := strings.ToLower(strings.TrimSpace(cfg.Server.AuthMode))
	if authMode == "" {
		authMode = "none"
	}
	if authMode != "none" && serverCORSAllowsWildcard(cfg.Server.CORSAllowedOrigins) {
		return fmt.Errorf("cors_allowed_origins=* is not allowed with authenticated server auth_mode=%s", authMode)
	}
	// Audit S-7: when AdminAuthEnabled is true the operator is
	// shipping the admin-session cookie surface (set-cookie reachable
	// via POST /v1/admin/session). Wildcard CORS combined with cookie
	// auth lets any origin issue authenticated requests once the
	// admin browser tab visits a hostile page in the same session.
	// Reject even when auth_mode=none, because the admin cookie is
	// independent of the request-auth mode.
	if cfg.Server.AdminAuthEnabled && serverCORSAllowsWildcard(cfg.Server.CORSAllowedOrigins) {
		return fmt.Errorf("cors_allowed_origins=* is not allowed when admin_auth_enabled=true; the admin session cookie surface requires an explicit origin allow-list")
	}
	if authMode != "none" {
		return validateServerAuthCredentials(cfg, authMode)
	}
	if parseServerBoolEnv(AllowInsecureNoAuthEnv) {
		return nil
	}
	if IsLoopbackListenAddr(cfg.Server.ListenAddr) {
		return nil
	}
	return fmt.Errorf("auth_mode=none is only allowed on loopback listen addresses; set %s=1 only for explicit local test runs", AllowInsecureNoAuthEnv)
}

var serverDeviceAgentLocalPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

var serverDeviceAgentBoxMediaRFC1918Prefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

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

func validateServerDeviceAgentBoxMedia(cfg *Config, reservedEnvs, reservedValues map[string]string) error {
	media := cfg.Server.DeviceAgent.BoxMedia
	if !media.Enabled {
		return nil
	}
	const path = "[server.device_agent.box_media]"

	if !cfg.Local.Enabled {
		return fmt.Errorf("%s requires [local].enabled=true for the host-local STT runtime", path)
	}
	modelPath := strings.TrimSpace(cfg.Local.ModelPath)
	if !filepath.IsAbs(modelPath) || filepath.Clean(modelPath) != modelPath {
		return fmt.Errorf("%s requires [local].model_path to be one canonical absolute path", path)
	}
	if cfg.Local.Port < 1 || cfg.Local.Port > 65535 {
		return fmt.Errorf("%s requires [local].port to be 1..65535", path)
	}

	host, portText, err := net.SplitHostPort(strings.TrimSpace(media.ListenAddr))
	if err != nil {
		return fmt.Errorf("%s.listen_addr must be one explicit local IP and port: %w", path, err)
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil || !serverDeviceAgentBoxMediaRFC1918Addr(address) {
		return fmt.Errorf("%s.listen_addr must use one explicit RFC1918 IPv4 address", path)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s.listen_addr port must be 1..65535", path)
	}

	certificateFile := strings.TrimSpace(media.CertificateFile)
	privateKeyFile := strings.TrimSpace(media.PrivateKeyFile)
	pinnedCAFile := strings.TrimSpace(media.PinnedCAFile)
	paths := map[string]string{
		"certificate_file": certificateFile,
		"private_key_file": privateKeyFile,
		"pinned_ca_file":   pinnedCAFile,
	}
	seenPaths := make(map[string]string, len(paths))
	for field, raw := range paths {
		if !filepath.IsAbs(raw) || filepath.Clean(raw) != raw {
			return fmt.Errorf("%s.%s must be one canonical absolute path", path, field)
		}
		canonical := strings.ToLower(filepath.Clean(raw))
		if previous, exists := seenPaths[canonical]; exists {
			return fmt.Errorf("%s.%s must be distinct from %s", path, field, previous)
		}
		seenPaths[canonical] = field
	}
	pinnedDigest := strings.TrimSpace(media.PinnedCASHA256)
	decodedDigest, err := hex.DecodeString(pinnedDigest)
	if err != nil || len(decodedDigest) != 32 || hex.EncodeToString(decodedDigest) != pinnedDigest {
		return fmt.Errorf("%s.pinned_ca_sha256 must be the lowercase SHA-256 of the pinned CA DER certificate", path)
	}

	tokenEnv := strings.TrimSpace(media.TokenEnv)
	if !validEnvName(tokenEnv) {
		return fmt.Errorf("%s.token_env must name one explicit environment variable", path)
	}
	tokenEnvKey := strings.ToUpper(tokenEnv)
	if scope, exists := reservedEnvs[tokenEnvKey]; exists {
		return fmt.Errorf("%s.token_env must be distinct from the %s credential env", path, scope)
	}
	mediaToken := strings.TrimSpace(ResolveSecret(tokenEnv))
	if !validServerDeviceAgentToken(mediaToken) {
		return fmt.Errorf("%s.token_env %q must resolve to a %d..512 byte bearer credential", path, tokenEnv, serverDeviceAgentMinimumTokenBytes)
	}
	if scope, exists := reservedValues[mediaToken]; exists {
		return fmt.Errorf("%s.token_env %q must not reuse the %s credential", path, tokenEnv, scope)
	}

	deviceID := strings.TrimSpace(media.DeviceID)
	pairingID := strings.TrimSpace(media.PairingID)
	roomID := strings.TrimSpace(media.RoomID)
	commandID := strings.TrimSpace(media.CommandID)
	transcript := strings.TrimSpace(media.Transcript)
	locale := strings.TrimSpace(media.Locale)
	if !validServerDeviceAgentID(deviceID) || !validServerDeviceAgentID(pairingID) || !validServerDeviceAgentID(roomID) || !validServerDeviceAgentID(commandID) {
		return fmt.Errorf("%s device_id, pairing_id, room_id, and command_id must be bounded stable identifiers", path)
	}
	if transcript == "" || len(transcript) > 512 || strings.ContainsAny(transcript, "\x00\r\n") {
		return fmt.Errorf("%s.transcript must be one bounded single-line utterance", path)
	}
	if !validServerDeviceAgentLanguage(locale) {
		return fmt.Errorf("%s.locale must be a bounded language tag", path)
	}

	for _, device := range cfg.Server.DeviceAgent.Devices {
		if strings.TrimSpace(device.DeviceID) != deviceID {
			continue
		}
		if strings.TrimSpace(device.PairingID) != pairingID || strings.TrimSpace(device.RoomID) != roomID {
			return fmt.Errorf("%s identity must match the selected paired device and current pairing epoch", path)
		}
		if !serverDeviceAgentBoxMediaHasRFC1918Prefix(device.AllowedClientCIDRs) {
			return fmt.Errorf("%s selected device allowed_client_cidrs must include at least one RFC1918 IPv4 prefix", path)
		}
		for _, rule := range device.LocalRules {
			if strings.TrimSpace(rule.RuleID) != commandID {
				continue
			}
			if strings.TrimSpace(rule.TriggerText) != transcript || strings.TrimSpace(rule.Locale) != locale {
				return fmt.Errorf("%s transcript and locale must exactly match the selected G0 rule", path)
			}
			return nil
		}
		return fmt.Errorf("%s.command_id must select one existing G0 rule for the paired device", path)
	}
	return fmt.Errorf("%s.device_id must select one existing paired device", path)
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

func validateServerDeviceAgentHomeAssistant(cfg AssistHomeAssistantConfig) error {
	rawURL := strings.TrimSpace(cfg.URL)
	if rawURL == "" {
		return fmt.Errorf("[assist.home_assistant].url is required when [server.device_agent] is enabled")
	}
	if strings.TrimSpace(cfg.TokenEnv) == "" {
		return fmt.Errorf("[assist.home_assistant].token_env is required when [server.device_agent] is enabled")
	}
	if !validEnvName(strings.TrimSpace(cfg.TokenEnv)) {
		return fmt.Errorf("[assist.home_assistant].token_env must be a valid environment variable name")
	}
	if cfg.AgentID != "" && !validServerDeviceAgentID(cfg.AgentID) {
		return fmt.Errorf("[assist.home_assistant].agent_id must be a bounded stable identifier")
	}
	if cfg.Language != "" && !validServerDeviceAgentLanguage(cfg.Language) {
		return fmt.Errorf("[assist.home_assistant].language must be a bounded language tag")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("[assist.home_assistant].url is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("[assist.home_assistant].url must use http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("[assist.home_assistant].url must include a host")
	}
	if parsed.User != nil {
		return fmt.Errorf("[assist.home_assistant].url must not contain user-info")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("[assist.home_assistant].url must not contain a query or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("[assist.home_assistant].url must be an origin without a path")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." {
			return fmt.Errorf("[assist.home_assistant].url must not contain '..' path segments")
		}
	}

	host := strings.TrimSpace(parsed.Hostname())
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil && !serverDeviceAgentLocalAddr(addr) {
		return fmt.Errorf("[assist.home_assistant].url literal host %q is public or wildcard; the device-agent bridge is local-only", host)
	}
	// DNS names are resolve-time validated by the bridge's restricted HTTP
	// client. Config validation deliberately makes no network call.
	return nil
}

func validateServerDeviceAgentCIDRs(path string, raw []string) error {
	if len(raw) == 0 {
		return fmt.Errorf("%s.allowed_client_cidrs must contain at least one explicit local CIDR", path)
	}
	seen := make(map[netip.Prefix]struct{}, len(raw))
	for _, value := range raw {
		cidr := strings.TrimSpace(value)
		if cidr == "" {
			return fmt.Errorf("%s.allowed_client_cidrs must not contain empty entries", path)
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return fmt.Errorf("%s.allowed_client_cidrs contains invalid CIDR %q: %w", path, cidr, err)
		}
		prefix = prefix.Masked()
		if !serverDeviceAgentLocalPrefix(prefix) {
			return fmt.Errorf("%s.allowed_client_cidrs contains public or wildcard CIDR %q; only explicit local ranges are allowed", path, cidr)
		}
		if _, exists := seen[prefix]; exists {
			return fmt.Errorf("%s.allowed_client_cidrs contains duplicate CIDR %q", path, cidr)
		}
		seen[prefix] = struct{}{}
	}
	return nil
}

func serverDeviceAgentLocalAddr(addr netip.Addr) bool {
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	for _, allowed := range serverDeviceAgentLocalPrefixes {
		if allowed.Contains(addr) {
			return true
		}
	}
	return false
}

func serverDeviceAgentBoxMediaRFC1918Addr(addr netip.Addr) bool {
	if !addr.Is4() {
		return false
	}
	for _, allowed := range serverDeviceAgentBoxMediaRFC1918Prefixes {
		if allowed.Contains(addr) {
			return true
		}
	}
	return false
}

func serverDeviceAgentBoxMediaHasRFC1918Prefix(rawCIDRs []string) bool {
	for _, rawCIDR := range rawCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rawCIDR))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		if !prefix.Addr().Is4() {
			continue
		}
		for _, allowed := range serverDeviceAgentBoxMediaRFC1918Prefixes {
			if prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Addr()) {
				return true
			}
		}
	}
	return false
}

func serverDeviceAgentLocalPrefix(prefix netip.Prefix) bool {
	prefix = prefix.Masked()
	if prefix.Addr().Is4In6() {
		return false
	}
	for _, allowed := range serverDeviceAgentLocalPrefixes {
		if prefix.Addr().BitLen() == allowed.Addr().BitLen() &&
			prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}

func validServerDeviceAgentID(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-', character == ':':
		default:
			return false
		}
	}
	return true
}

func validServerDeviceAgentToken(value string) bool {
	if len(value) < serverDeviceAgentMinimumTokenBytes || len(value) > 512 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '.', character == '_', character == '~':
		case character == '+', character == '/', character == '=':
		default:
			return false
		}
	}
	return true
}

func validServerHomeAssistantToken(value string) bool {
	if len(value) < serverDeviceAgentMinimumTokenBytes || len(value) > 4096 {
		return false
	}
	for _, character := range value {
		if character < '!' || character > '~' {
			return false
		}
	}
	return true
}

func validServerDeviceAgentLanguage(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-':
		default:
			return false
		}
	}
	return true
}

func validServerDeviceAgentLightEntity(raw string) bool {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "light.") || len(value) <= len("light.") || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_':
		default:
			return false
		}
	}
	return true
}

func validateServerResourceLimits(cfg *Config) error {
	if cfg.Server.ReadHeaderTimeoutSec < 0 {
		return fmt.Errorf("read_header_timeout_sec must be >= 0")
	}
	if cfg.Server.ReadTimeoutSec < 0 {
		return fmt.Errorf("read_timeout_sec must be >= 0")
	}
	if cfg.Server.IdleTimeoutSec < 0 {
		return fmt.Errorf("idle_timeout_sec must be >= 0")
	}
	if cfg.Server.MaxHeaderBytes < 0 {
		return fmt.Errorf("max_header_bytes must be >= 0")
	}
	if cfg.Server.MaxDecodedAudioSeconds < 0 {
		return fmt.Errorf("max_decoded_audio_seconds must be >= 0")
	}
	for _, raw := range cfg.Server.TrustedProxyCIDRs {
		cidr := strings.TrimSpace(raw)
		if cidr == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("trusted_proxy_cidrs contains invalid CIDR %q: %w", cidr, err)
		}
	}
	return nil
}

func validateServerAuthCredentials(cfg *Config, authMode string) error {
	bearerEnv := strings.TrimSpace(cfg.Server.BearerTokenEnv)
	if bearerEnv == "" {
		bearerEnv = "SPEECHKIT_SERVER_TOKEN"
	}
	edgeEnv := strings.TrimSpace(cfg.Server.EdgeAuthSecretEnv)
	if edgeEnv == "" {
		edgeEnv = "EDGE_AUTH_SECRET"
	}
	bearerSet := strings.TrimSpace(os.Getenv(bearerEnv)) != ""
	edgeSet := strings.TrimSpace(os.Getenv(edgeEnv)) != ""
	switch authMode {
	case "bearer":
		if !bearerSet {
			return fmt.Errorf("auth_mode=bearer requires %s to be set", bearerEnv)
		}
	case "edge_hmac":
		if !edgeSet {
			return fmt.Errorf("auth_mode=edge_hmac requires %s to be set", edgeEnv)
		}
	case "bearer_or_edge":
		if !bearerSet && !edgeSet {
			return fmt.Errorf("auth_mode=bearer_or_edge requires %s or %s to be set", bearerEnv, edgeEnv)
		}
	case "bearer_or_oidc":
		// The OIDC leg is what distinguishes this mode — bootstrap builds the
		// JWT validator unconditionally for it, so an empty jwks_url must be
		// rejected here (fail-closed with a clear message) instead of
		// crashing the server later. Bearer-only deployments use
		// auth_mode=bearer.
		if strings.TrimSpace(cfg.Server.OIDC.JWKSURL) == "" {
			return fmt.Errorf("auth_mode=bearer_or_oidc requires [server.oidc].jwks_url to be set (use auth_mode=bearer for bearer-only deployments; %s adds the bearer leg)", bearerEnv)
		}
	}
	return nil
}

func serverCORSAllowsWildcard(origins []string) bool {
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "*" {
			return true
		}
	}
	return false
}

func parseServerBoolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// IsLoopbackListenAddr reports whether the given HTTP listen address binds
// only to a loopback interface (127.0.0.1, ::1, localhost). Used by both
// startup validation (server_security.go) and the auth middleware to decide
// whether AuthModeNone is acceptable as a runtime fallback.
//
// Returns false for wildcard binds (":8080", "0.0.0.0:8080", "[::]:8080")
// and for any address that fails to parse — fail-closed: an unknown bind is
// treated as public.
func IsLoopbackListenAddr(raw string) bool {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		addr = ":8080"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			host = ""
		} else {
			return false
		}
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
