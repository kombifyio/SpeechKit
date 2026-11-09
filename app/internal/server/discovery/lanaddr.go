package discovery

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/config"
)

// virtualInterfacePrefixes name container, VM and overlay interfaces whose
// addresses no LAN device can dial.
var virtualInterfacePrefixes = []string{
	"docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "vethernet", "podman", "lxcbr", "lxdbr",
}

// dockerDefaultBridge is the default bridge network of a container engine.
var dockerDefaultBridge = &net.IPNet{IP: net.IPv4(172, 17, 0, 0), Mask: net.CIDRMask(16, 32)}

// ErrNoLANAddress means no interface carries a LAN-dialable IPv4 address.
var ErrNoLANAddress = errors.New("discovery: no LAN IPv4 address found")

// PrimaryLANIPv4 returns the IPv4 address of the default-route interface and
// that interface. It never returns a loopback, link-local or container bridge
// address. The route choice comes from a UDP "connect" to a documentation
// address, which sends no packet; host name resolution is not used (it yields
// 127.0.1.1 on common Linux installs). Without a usable default route the
// first up, multicast-capable interface with a private IPv4 address is used.
func PrimaryLANIPv4() (net.IP, *net.Interface, error) {
	if conn, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		local, _ := conn.LocalAddr().(*net.UDPAddr)
		_ = conn.Close()
		if local != nil {
			if iface := interfaceWithIP(local.IP); iface != nil && usableLANAddress(local.IP, iface) {
				return local.IP.To4(), iface, nil
			}
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, errors.Join(ErrNoLANAddress, err)
	}
	for i := range ifaces {
		iface := &ifaces[i]
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || !ipNet.IP.IsPrivate() || !usableLANAddress(ipNet.IP, iface) {
				continue
			}
			return ipNet.IP.To4(), iface, nil
		}
	}
	return nil, nil, ErrNoLANAddress
}

func usableLANAddress(ip net.IP, iface *net.Interface) bool {
	v4 := ip.To4()
	if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() || v4.IsUnspecified() || dockerDefaultBridge.Contains(v4) {
		return false
	}
	if iface == nil || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagMulticast == 0 {
		return false
	}
	name := strings.ToLower(iface.Name)
	for _, prefix := range virtualInterfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

func interfaceWithIP(ip net.IP) *net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.Equal(ip) {
				return &ifaces[i]
			}
		}
	}
	return nil
}

// LANAddress is the address LAN devices dial for this server.
type LANAddress struct {
	URL string
	IP  string
}

// ResolveLANAddress reports the LAN address of the serving process, in this
// order: [server.discovery] advertise_url; the address an announce-only
// process sharing the state directory wrote; the process's own primary LAN
// address, but only outside a container, where a bridge address would be
// mistaken for a LAN one. ok is false when none is known.
func ResolveLANAddress(cfg *config.Config) (LANAddress, bool) {
	if cfg == nil {
		return LANAddress{}, false
	}
	if raw := strings.TrimRight(strings.TrimSpace(cfg.Server.Discovery.AdvertiseURL), "/"); raw != "" {
		out := LANAddress{URL: raw}
		if u, err := url.Parse(raw); err == nil {
			if ip := net.ParseIP(u.Hostname()); ip != nil && ip.To4() != nil {
				out.IP = ip.String()
			}
		}
		return out, true
	}
	if a, ok := LoadAnnouncement(config.ServerStateDir(cfg)); ok {
		out := LANAddress{URL: strings.TrimRight(a.URL, "/")}
		if ip := net.ParseIP(a.IP); ip != nil && ip.To4() != nil {
			out.IP = ip.String()
		}
		return out, true
	}
	if inContainer() {
		return LANAddress{}, false
	}
	ip, _, err := PrimaryLANIPv4()
	if err != nil {
		return LANAddress{}, false
	}
	return LANAddress{
		URL: "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(AdvertisePort(cfg))),
		IP:  ip.String(),
	}, true
}

func inContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}
