package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

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
