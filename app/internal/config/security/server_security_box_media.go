package security

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/config"
)

var serverDeviceAgentBoxMediaRFC1918Prefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

func validateServerDeviceAgentBoxMedia(cfg *config.Config, reservedEnvs, reservedValues map[string]string) error {
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
	mediaToken := strings.TrimSpace(config.ResolveSecret(tokenEnv))
	if !validServerDeviceAgentToken(mediaToken) {
		return fmt.Errorf("%s.token_env %q must resolve to a %d..512 byte bearer credential", path, tokenEnv, serverDeviceAgentMinimumTokenBytes)
	}
	if scope, exists := reservedValues[mediaToken]; exists {
		return fmt.Errorf("%s.token_env %q must not reuse the %s credential", path, tokenEnv, scope)
	}

	deviceID := strings.TrimSpace(media.DeviceID)
	pairingID := strings.TrimSpace(media.PairingID)
	roomID := strings.TrimSpace(media.RoomID)
	locale := strings.TrimSpace(media.Locale)
	if !validServerDeviceAgentID(deviceID) || !validServerDeviceAgentID(pairingID) || !validServerDeviceAgentID(roomID) {
		return fmt.Errorf("%s device_id, pairing_id, and room_id must be bounded stable identifiers", path)
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
		return validateServerDeviceAgentBoxMediaMappings(path, media.Mappings, device.LocalRules, locale)
	}
	return fmt.Errorf("%s.device_id must select one existing paired device", path)
}

// validateServerDeviceAgentBoxMediaMappings proves the closed mapping set:
// bounded, unique by command and normalized transcript, every entry an exact
// re-statement of one G0 rule of the selected binding in the binding locale,
// all entries one light, and both turn_on and turn_off present.
func validateServerDeviceAgentBoxMediaMappings(path string, mappings []config.ServerDeviceAgentBoxMediaMappingConfig, rules []config.ServerDeviceAgentLocalRuleConfig, locale string) error {
	if len(mappings) == 0 || len(mappings) > config.MaxServerDeviceAgentBoxMediaMappings {
		return fmt.Errorf("%s.mappings must contain 1..%d exact entries", path, config.MaxServerDeviceAgentBoxMediaMappings)
	}
	rulesByID := make(map[string]config.ServerDeviceAgentLocalRuleConfig, len(rules))
	for _, rule := range rules {
		rulesByID[strings.TrimSpace(rule.RuleID)] = rule
	}
	seenCommands := make(map[string]int, len(mappings))
	seenTranscripts := make(map[string]int, len(mappings))
	actions := make(map[string]bool, 2)
	entityID := ""
	for index, mapping := range mappings {
		entry := fmt.Sprintf("%s.mappings[%d]", path, index)
		commandID := strings.TrimSpace(mapping.CommandID)
		transcript := strings.TrimSpace(mapping.Transcript)
		if !validServerDeviceAgentID(commandID) {
			return fmt.Errorf("%s.command_id must be a bounded stable identifier", entry)
		}
		if transcript == "" || len(transcript) > 512 || strings.ContainsAny(transcript, "\x00\r\n") {
			return fmt.Errorf("%s.transcript must be one bounded single-line utterance", entry)
		}
		if previous, exists := seenCommands[commandID]; exists {
			return fmt.Errorf("%s duplicates the command_id of mappings[%d]", entry, previous)
		}
		seenCommands[commandID] = index
		normalized := strings.ToLower(strings.Join(strings.Fields(transcript), " "))
		if previous, exists := seenTranscripts[normalized]; exists {
			return fmt.Errorf("%s is ambiguous: its normalized transcript repeats mappings[%d]", entry, previous)
		}
		seenTranscripts[normalized] = index
		rule, exists := rulesByID[commandID]
		if !exists {
			return fmt.Errorf("%s.command_id must select one existing G0 rule for the paired device", entry)
		}
		if strings.TrimSpace(rule.TriggerText) != transcript || strings.TrimSpace(rule.Locale) != locale {
			return fmt.Errorf("%s transcript and locale must exactly match the selected G0 rule", entry)
		}
		action := strings.TrimSpace(rule.Action)
		if action != "turn_on" && action != "turn_off" {
			return fmt.Errorf("%s must select a turn_on or turn_off G0 rule", entry)
		}
		actions[action] = true
		ruleEntity := strings.TrimSpace(rule.EntityID)
		if entityID == "" {
			entityID = ruleEntity
		} else if ruleEntity != entityID {
			return fmt.Errorf("%s must target the one selected light %q", entry, entityID)
		}
	}
	if !actions["turn_on"] || !actions["turn_off"] {
		return fmt.Errorf("%s.mappings must cover both turn_on and turn_off of the selected light", path)
	}
	return nil
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
