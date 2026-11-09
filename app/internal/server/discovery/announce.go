package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"

	"github.com/kombifyio/SpeechKit/app/internal/config"
)

// RunAnnounceOnly is the announce-only mode (`speechkit-server
// --discovery-only`). It binds nothing but the mDNS responder (UDP 5353,
// shared with any system responder), announces `_speechkit._tcp` for the
// server that listens elsewhere, and returns when ctx ends. It makes no
// other network call.
//
// The port is [server.discovery] advertise_port, else the listen_addr port.
// With an empty advertise_url the URL is http://<primary LAN IPv4>:<port>;
// the A record names only that address (an IPv4 literal in advertise_url
// takes its place). Selecting the mode is the opt-in, so
// [server.discovery] enabled is not consulted.
//
// The instance id is read, never created, from the state directory the
// server shares (config.ServerStateDir): an announcer without that volume
// announces no `id=`. Every refreshInterval the address and the id are read
// again and the responder is rebuilt when either changed (DHCP renewal, a
// server that created its id after the announcer started), and the
// heartbeat file that ProbeAnnounceOnly checks is touched.
func RunAnnounceOnly(ctx context.Context, cfg *config.Config, version string, modes []string) error {
	if cfg == nil {
		return errors.New("discovery-only: no configuration")
	}
	stateDir := config.ServerStateDir(cfg)
	current, err := resolveAnnouncement(cfg, stateDir)
	if err != nil {
		return err
	}
	server, err := current.start(version, modes)
	if err != nil {
		return err
	}
	defer func() { shutdownResponder(server) }()
	current.publish(stateDir)
	current.log("discovery-only: announcing")
	touchHeartbeat()

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			removeHeartbeat()
			return nil
		case <-ticker.C:
		}
		next, err := resolveAnnouncement(cfg, stateDir)
		if err != nil {
			slog.Warn("discovery-only: keeping the previous announcement", "err", err)
			touchHeartbeat()
			continue
		}
		if next.same(current) {
			touchHeartbeat()
			continue
		}
		shutdownResponder(server)
		server, err = next.start(version, modes)
		if err != nil {
			return err
		}
		current = next
		current.publish(stateDir)
		current.log("discovery-only: announcement changed")
		touchHeartbeat()
	}
}

// refreshInterval is how often the announce-only mode re-reads its address
// and id; the heartbeat goes stale after three missed refreshes.
const refreshInterval = 30 * time.Second

type announcement struct {
	instance  string
	advertise string
	ip        net.IP
	iface     *net.Interface
	port      int
	id        string
}

func resolveAnnouncement(cfg *config.Config, stateDir string) (announcement, error) {
	d := cfg.Server.Discovery
	a := announcement{port: AdvertisePort(cfg)}

	ip, iface, lanErr := PrimaryLANIPv4()
	advertise := strings.TrimRight(strings.TrimSpace(d.AdvertiseURL), "/")
	if advertise != "" {
		u, err := url.Parse(advertise)
		if err != nil || u.Host == "" {
			return a, fmt.Errorf("discovery-only: [server.discovery] advertise_url %q is not an absolute URL", advertise)
		}
		if literal := net.ParseIP(u.Hostname()); literal != nil && literal.To4() != nil {
			ip = literal.To4()
			if found := interfaceWithIP(ip); found != nil {
				iface = found
			}
			lanErr = nil
		}
	}
	if lanErr != nil || ip == nil {
		return a, fmt.Errorf("discovery-only: %w; run this mode in the host network namespace "+
			"(docker --network host) or set [server.discovery] advertise_url = \"http://<lan-ip>:%d\"", ErrNoLANAddress, a.port)
	}
	if advertise == "" {
		advertise = "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(a.port))
	}
	a.advertise, a.ip, a.iface = advertise, ip, iface

	a.instance = strings.TrimSpace(d.InstanceName)
	if a.instance == "" {
		if host, _ := os.Hostname(); strings.TrimSpace(host) != "" {
			a.instance = strings.TrimSpace(host)
		} else {
			a.instance = "speechkit"
		}
	}
	if id, err := LoadInstanceID(stateDir); err == nil {
		a.id = id
	}
	return a, nil
}

func (a announcement) same(b announcement) bool {
	return a.instance == b.instance && a.advertise == b.advertise && a.ip.Equal(b.ip) &&
		a.port == b.port && a.id == b.id && a.ifaceName() == b.ifaceName()
}

func (a announcement) ifaceName() string {
	if a.iface == nil {
		return ""
	}
	return a.iface.Name
}

func (a announcement) start(version string, modes []string) (*mdns.Server, error) {
	// The SRV target is a name of our own with an A record for the chosen
	// address only; the host name of a container or a 127.0.1.1 mapping
	// never reaches the LAN.
	hostName := dnsLabel(a.instance) + ".local."
	service, err := mdns.NewMDNSService(a.instance, ServiceType, "", hostName, a.port, []net.IP{a.ip},
		txtLines(a.advertise, version, modes, a.id))
	if err != nil {
		return nil, fmt.Errorf("discovery-only: build mDNS service: %w", err)
	}
	server, err := mdns.NewServer(&mdns.Config{Zone: service, Iface: a.iface})
	if err != nil {
		return nil, fmt.Errorf("discovery-only: start mDNS responder on UDP 5353: %w", err)
	}
	return server, nil
}

// publish hands the announced address to a server sharing the state
// directory; a read-only mount simply skips it.
func (a announcement) publish(stateDir string) {
	if err := writeAnnouncement(stateDir, Announcement{URL: a.advertise, IP: a.ip.String(), Port: a.port}); err != nil {
		slog.Debug("discovery-only: announcement file not written", "dir", stateDir, "err", err)
	}
}

func (a announcement) log(msg string) {
	slog.Info(msg, "service", ServiceType, "instance", a.instance, "url", a.advertise,
		"ip", a.ip.String(), "port", a.port, "interface", a.ifaceName(), "id", a.id)
}

func shutdownResponder(server *mdns.Server) {
	if server == nil {
		return
	}
	if err := server.Shutdown(); err != nil {
		slog.Warn("mDNS discovery shutdown", "err", err)
	}
}

// heartbeatPath is the announce-only liveness file, in the temporary
// directory so a read-only state volume does not matter.
func heartbeatPath() string { return filepath.Join(os.TempDir(), "speechkit-discovery-only.alive") }

func touchHeartbeat() {
	if err := os.WriteFile(heartbeatPath(), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		slog.Debug("discovery-only: heartbeat not written", "err", err)
	}
}

func removeHeartbeat() { _ = os.Remove(heartbeatPath()) }

// ProbeAnnounceOnly is the health probe of the announce-only mode
// (`speechkit-server --discovery-only --probe`): nil while a responder in
// this container refreshed its heartbeat within three refresh intervals. It
// makes no network call.
func ProbeAnnounceOnly() error {
	info, err := os.Stat(heartbeatPath())
	if err != nil {
		return fmt.Errorf("discovery-only probe: no running announcer (%w)", err)
	}
	if age := time.Since(info.ModTime()); age > 3*refreshInterval {
		return fmt.Errorf("discovery-only probe: announcer heartbeat is %s old", age.Truncate(time.Second))
	}
	return nil
}

// dnsLabel turns an instance name into one DNS label: lower-case letters,
// digits and hyphens, at most 63 characters.
func dnsLabel(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == ' ' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}
	label := strings.Trim(b.String(), "-")
	if len(label) > 63 {
		label = strings.Trim(label[:63], "-")
	}
	if label == "" {
		return "speechkit"
	}
	return label
}

// AnnounceOnlyOptions are the command-line values of the announce-only mode;
// empty values keep what cfg already says.
type AnnounceOnlyOptions struct {
	Listen   string
	Port     int
	Modes    string
	Instance string
	URL      string
}

// ApplyAnnounceOnlyOptions writes the announce-only flags into cfg.
func ApplyAnnounceOnlyOptions(cfg *config.Config, o AnnounceOnlyOptions) error {
	if v := strings.TrimSpace(o.Listen); v != "" {
		cfg.Server.ListenAddr = v
	}
	if o.Port != 0 {
		if o.Port < 1 || o.Port > 65535 {
			return fmt.Errorf("--discovery-port %d is not a TCP port", o.Port)
		}
		cfg.Server.Discovery.AdvertisePort = o.Port
	}
	if v := strings.TrimSpace(o.Modes); v != "" {
		var modes []string
		for _, part := range strings.Split(v, ",") {
			if m := strings.ToLower(strings.TrimSpace(part)); m != "" {
				modes = append(modes, m)
			}
		}
		cfg.Server.Modes = modes
	}
	if v := strings.TrimSpace(o.Instance); v != "" {
		cfg.Server.Discovery.InstanceName = v
	}
	if v := strings.TrimSpace(o.URL); v != "" {
		cfg.Server.Discovery.AdvertiseURL = v
	}
	return nil
}
