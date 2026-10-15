package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// Model sources ([models] source, Models\Source). The value is one of the two
// words below or an internal mirror base URL.
const (
	// ModelsSourcePublic downloads model files from the catalog's public
	// hosts. It is the default.
	ModelsSourcePublic = "public"
	// ModelsSourceBundled uses only model files already on the device (the
	// managed models directory or what the installer shipped); every
	// download is refused.
	ModelsSourceBundled = "bundled"
)

// ModelSourceMode is how a process obtains model files.
type ModelSourceMode string

const (
	ModelSourceModePublic  ModelSourceMode = "public"
	ModelSourceModeBundled ModelSourceMode = "bundled"
	ModelSourceModeMirror  ModelSourceMode = "mirror"
)

// ModelsConfig is the [models] table.
type ModelsConfig struct {
	// Source is "public" (the default), "bundled" or the https base URL of an
	// internal mirror that serves every catalog artifact at the catalog's
	// path under it. An unusable value behaves as "bundled": a source that
	// cannot be understood never reaches a public host.
	Source string `toml:"source,omitempty"`
}

// ModelSource is the resolved Models\Source setting.
type ModelSource struct {
	Mode ModelSourceMode
	// MirrorBase is the mirror's base URL without a trailing slash; set only
	// in ModelSourceModeMirror.
	MirrorBase string
}

// ParseModelsSource normalizes a Models\Source value: "" and "public" mean
// the public hosts, "bundled" the files already on the device, anything else
// must be an https mirror base URL (host required; no user-info, query or
// fragment). The mirror host may be a private address; whether it can be
// reached is the network scope's decision at the socket.
func ParseModelsSource(v string) (string, error) {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", ModelsSourcePublic:
		return ModelsSourcePublic, nil
	case ModelsSourceBundled:
		return ModelsSourceBundled, nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", errors.New("not a URL")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("a mirror must use https")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", errors.New("a mirror base URL has no query or fragment")
	}
	if err := netsec.ValidateProviderURL(v, netsec.ValidationOptions{AllowPrivate: true}); err != nil {
		return "", fmt.Errorf("mirror base URL rejected: %w", err)
	}
	return strings.TrimRight(v, "/"), nil
}

// modelsSourceStrictness orders the sources for the policy: a mirror closes
// the public hosts, bundled closes every download.
func modelsSourceStrictness(v string) int {
	parsed, err := ParseModelsSource(v)
	switch {
	case err != nil, parsed == ModelsSourceBundled:
		return 2
	case parsed == ModelsSourcePublic:
		return 0
	default:
		return 1
	}
}

// validateModelsSource is the Models\Source validator; see ParseModelsSource.
func validateModelsSource(v string) (string, error) { return ParseModelsSource(v) }

// ModelSource resolves [models] source (after the machine policy). An
// unusable value fails closed to bundled.
func (c *Config) ModelSource() ModelSource {
	if c == nil {
		return ModelSource{Mode: ModelSourceModePublic}
	}
	v, err := ParseModelsSource(c.Models.Source)
	if err != nil {
		return ModelSource{Mode: ModelSourceModeBundled}
	}
	switch v {
	case ModelsSourcePublic:
		return ModelSource{Mode: ModelSourceModePublic}
	case ModelsSourceBundled:
		return ModelSource{Mode: ModelSourceModeBundled}
	default:
		return ModelSource{Mode: ModelSourceModeMirror, MirrorBase: v}
	}
}
