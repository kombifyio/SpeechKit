package discovery

import (
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/mdns"
)

// Record is one SpeechKit server found on the LAN. URL is the only dial
// address; credentials never belong here.
type Record struct {
	InstanceName string   `json:"instanceName"`
	URL          string   `json:"url"`
	Modes        []string `json:"modes,omitempty"`
	Version      string   `json:"version,omitempty"`
}

var credentialTXTKeys = map[string]struct{}{
	"token":    {},
	"auth":     {},
	"password": {},
	"secret":   {},
	"bearer":   {},
}

// ParseTXT reads `key=value` TXT lines as the announcer emits them.
// Missing url is not a dialable server.
func ParseTXT(instanceName string, lines []string) (Record, bool) {
	attrs := make(map[string]string, len(lines))
	for _, line := range lines {
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if _, drop := credentialTXTKeys[strings.ToLower(key)]; drop {
			continue
		}
		attrs[key] = line[eq+1:]
	}
	url := strings.TrimSpace(attrs["url"])
	if url == "" {
		return Record{}, false
	}
	var modes []string
	for _, part := range strings.Split(attrs["modes"], ",") {
		if m := strings.TrimSpace(part); m != "" {
			modes = append(modes, m)
		}
	}
	return Record{
		InstanceName: instanceName,
		URL:          url,
		Modes:        modes,
		Version:      strings.TrimSpace(attrs["version"]),
	}, true
}

func recordFromEntry(e *mdns.ServiceEntry) (Record, bool) {
	if e == nil {
		return Record{}, false
	}
	name := instanceName(e.Name)
	if rec, ok := ParseTXT(name, e.InfoFields); ok {
		return rec, true
	}
	host := ""
	if e.AddrV4 != nil && !e.AddrV4.IsUnspecified() {
		host = e.AddrV4.String()
	} else if e.AddrV6 != nil && !e.AddrV6.IsUnspecified() {
		host = e.AddrV6.String()
	}
	if host == "" || e.Port <= 0 {
		return Record{}, false
	}
	return Record{
		InstanceName: name,
		URL:          "http://" + net.JoinHostPort(host, strconv.Itoa(e.Port)),
	}, true
}

func instanceName(fqdn string) string {
	name := strings.TrimSuffix(fqdn, ".")
	name = strings.TrimSuffix(name, "."+ServiceType+".local")
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	if name == "" {
		return fqdn
	}
	return name
}
