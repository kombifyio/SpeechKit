package cloudlink

import (
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/discovery"
)

// ReportFromConfig builds the check-in content from the effective server
// configuration: name, enabled modes, the primary model of each stage and the
// LAN address devices dial (discovery.ResolveLANAddress).
func ReportFromConfig(cfg *config.Config, version string, modes []string) Report {
	r := Report{Version: version, Modes: modes}
	if cfg == nil {
		return r
	}
	r.DisplayName = firstNonEmpty(cfg.Server.CloudLink.DisplayName, cfg.Server.Discovery.InstanceName)
	if r.DisplayName == "" {
		if host, err := os.Hostname(); err == nil {
			r.DisplayName = strings.TrimSpace(host)
		}
	}
	r.STT = cfg.ModelSelection.Dictate.PrimaryProfileID
	r.LLM = cfg.ModelSelection.Assist.PrimaryProfileID
	r.TTS = cfg.ModelSelection.TTS.PrimaryProfileID
	if lan, ok := discovery.ResolveLANAddress(cfg); ok {
		r.LANURL, r.LocalIP = lan.URL, lan.IP
	}
	return r
}
