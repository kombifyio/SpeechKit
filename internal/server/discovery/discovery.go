// Package discovery announces the SpeechKit server on the local network via
// mDNS/DNS-SD so LAN devices (Kombify Box, desktop app, Android) can find a
// homelab instance under `_speechkit._tcp` without typing an address.
//
// Scope: announcement only, opt-in via [server.discovery] (default OFF). The
// TXT record carries the dial URL plus enabled modes; it never carries
// credentials — auth (bearer/OIDC) applies unchanged after discovery.
package discovery

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/mdns"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// ServiceType is the DNS-SD service type SpeechKit servers announce.
const ServiceType = "_speechkit._tcp"

// Announcer wraps the running mDNS responder.
type Announcer struct {
	server *mdns.Server
}

// Start begins announcing when cfg.Server.Discovery.Enabled is set; it
// returns (nil, nil) when discovery is off. version and modes land in the
// TXT record so clients can filter capable instances (the Box needs
// "voiceagent").
func Start(cfg *config.Config, version string, modes []string) (*Announcer, error) {
	if cfg == nil || !cfg.Server.Discovery.Enabled {
		return nil, nil
	}
	d := cfg.Server.Discovery

	port := listenPort(cfg.Server.ListenAddr)
	host, _ := os.Hostname()
	if host == "" {
		host = "speechkit"
	}

	instance := strings.TrimSpace(d.InstanceName)
	if instance == "" {
		instance = host
	}

	advertise := strings.TrimSpace(d.AdvertiseURL)
	if advertise == "" {
		advertise = strings.TrimSpace(cfg.Server.PublicBaseURL)
	}
	if advertise == "" {
		advertise = fmt.Sprintf("http://%s:%d", host, port)
	}

	txt := []string{
		"url=" + advertise,
		"modes=" + strings.Join(modes, ","),
		"version=" + version,
	}

	service, err := mdns.NewMDNSService(instance, ServiceType, "", "", port, nil, txt)
	if err != nil {
		return nil, fmt.Errorf("discovery: build mDNS service: %w", err)
	}
	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return nil, fmt.Errorf("discovery: start mDNS responder: %w", err)
	}
	slog.Info("mDNS discovery announcing", "service", ServiceType,
		"instance", instance, "port", port, "url", advertise)
	return &Announcer{server: server}, nil
}

// Shutdown stops the responder (safe on nil).
func (a *Announcer) Shutdown() {
	if a == nil || a.server == nil {
		return
	}
	if err := a.server.Shutdown(); err != nil {
		slog.Warn("mDNS discovery shutdown", "err", err)
	}
}

// listenPort extracts the TCP port from a listen address like ":8080" or
// "0.0.0.0:8080"; unparseable input falls back to 8080 (the server default).
func listenPort(addr string) int {
	_, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return 8080
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 8080
	}
	return port
}
