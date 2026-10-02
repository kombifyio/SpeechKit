package deviceagent

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

func newDeviceBinding(raw DeviceBindingOptions) (deviceBinding, error) {
	binding := deviceBinding{
		pairingID: strings.TrimSpace(raw.PairingID),
		deviceID:  strings.TrimSpace(raw.DeviceID),
		roomID:    strings.TrimSpace(raw.RoomID),
		token:     strings.TrimSpace(raw.Token),
	}
	switch {
	case !validBridgeID(binding.pairingID):
		return deviceBinding{}, errors.New("device-agent bridge: pairing id must be a bounded stable identifier")
	case !validBridgeID(binding.deviceID):
		return deviceBinding{}, errors.New("device-agent bridge: device id must be a bounded stable identifier")
	case !validBridgeID(binding.roomID):
		return deviceBinding{}, fmt.Errorf("device-agent bridge: room id must be a bounded stable identifier for device %q", binding.deviceID)
	case !validBridgeToken(binding.token):
		return deviceBinding{}, fmt.Errorf("device-agent bridge: pairing token for device %q must be a %d..512 byte bearer credential", binding.deviceID, minPairingTokenLen)
	case len(raw.AllowedClientCIDRs) == 0:
		return deviceBinding{}, fmt.Errorf("device-agent bridge: allowed client CIDRs are required for device %q", binding.deviceID)
	}
	for _, rawCIDR := range raw.AllowedClientCIDRs {
		cidr := strings.TrimSpace(rawCIDR)
		if !localBridgeCIDR(cidr) {
			return deviceBinding{}, fmt.Errorf("device-agent bridge: client CIDR %q must be an explicit local range", rawCIDR)
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return deviceBinding{}, fmt.Errorf("device-agent bridge: invalid client CIDR %q: %w", rawCIDR, err)
		}
		binding.allowed = append(binding.allowed, network)
	}
	return binding, nil
}

var bridgeLocalPrefixes = []netip.Prefix{
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

func localBridgeCIDR(raw string) bool {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || prefix.Addr().Is4In6() {
		return false
	}
	prefix = prefix.Masked()
	for _, allowed := range bridgeLocalPrefixes {
		if prefix.Addr().BitLen() == allowed.Addr().BitLen() && prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Addr()) {
			return true
		}
	}
	return false
}

func validBridgeID(raw string) bool {
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

func validBridgeToken(value string) bool {
	if len(value) < minPairingTokenLen || len(value) > 512 {
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

func (b *Bridge) wrap(next func(http.ResponseWriter, *http.Request, deviceBinding)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(wire.ServerInstanceHeader, b.serverInstanceID)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			b.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "device_agent_post_required", false, "no", "Use POST for this local device-agent endpoint.")
			return
		}
		binding, ok := b.authenticate(r)
		if !ok {
			b.writeError(w, http.StatusUnauthorized, "device_auth_required", "pairing_credential_invalid", false, "no", "Pair this device again with the local SpeechKit server.")
			return
		}
		if !bindingAllowsRemote(binding, r.RemoteAddr) {
			b.writeError(w, http.StatusForbidden, "device_source_denied", "source_cidr_not_allowed", false, "no", "Connect directly from the paired device network address.")
			return
		}
		next(w, r, binding)
	}
}

func (b *Bridge) authenticate(r *http.Request) (deviceBinding, bool) {
	deviceID := strings.TrimSpace(r.Header.Get("X-SpeechKit-Device-ID"))
	binding, ok := b.bindings[deviceID]
	if !ok {
		return deviceBinding{}, false
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
		return deviceBinding{}, false
	}
	token = strings.TrimSpace(token)
	if len(token) != len(binding.token) || subtle.ConstantTimeCompare([]byte(token), []byte(binding.token)) != 1 {
		return deviceBinding{}, false
	}
	return binding, true
}

func bindingAllowsRemote(binding deviceBinding, remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range binding.allowed {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
