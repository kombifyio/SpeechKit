package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

var (
	// ErrDuplicateProfileID is returned by NewCatalog and Catalog.With when two
	// profiles share an id.
	ErrDuplicateProfileID = errors.New("speechkit: duplicate provider profile id")
	// ErrInvalidProfile wraps the mode-contract violation that keeps a profile
	// out of a Catalog.
	ErrInvalidProfile = errors.New("speechkit: invalid provider profile")
)

// Catalog is an immutable, host-composable set of provider profiles.
//
// The built-in catalog (DefaultCatalog) is what the reference apps ship. A
// host that adds its own STT, TTS or realtime provider describes it as a
// ProviderProfile and extends the catalog with With; the result feeds the same
// matrix, defaults, policy filtering and mode validation the built-ins go
// through, so the provider shows up in setup UIs, readiness and routing like
// any shipped one. Every profile is normalised with ProviderProfileWithDefaults
// and checked against its mode contract on the way in — a Catalog never holds
// an invalid profile.
type Catalog struct {
	profiles []speechkit.ProviderProfile
	index    map[string]int
}

// NewCatalog builds a catalog from the given profiles. Profiles are
// normalised, validated against their mode contract, and must have unique ids.
func NewCatalog(profiles ...speechkit.ProviderProfile) (*Catalog, error) {
	c := &Catalog{index: map[string]int{}}
	if err := c.add(profiles); err != nil {
		return nil, err
	}
	return c, nil
}

// DefaultCatalog returns the built-in framework catalog. It is the same data
// as DefaultProviderProfiles, exposed as a Catalog so hosts extend it with
// With instead of concatenating slices by hand.
func DefaultCatalog() *Catalog {
	c, err := NewCatalog(DefaultProviderProfiles()...)
	if err != nil {
		// The built-in catalog is validated by the test suite; an error here
		// is a programming mistake in this package, not a runtime condition.
		panic(err)
	}
	return c
}

// With returns a new Catalog containing the receiver's profiles plus the
// given ones. The receiver is unchanged. Ids must not collide with existing
// profiles; a host that wants to replace a built-in filters Profiles() first
// and builds a fresh catalog with NewCatalog.
func (c *Catalog) With(profiles ...speechkit.ProviderProfile) (*Catalog, error) {
	next := &Catalog{index: make(map[string]int, len(c.profiles)+len(profiles))}
	next.profiles = append(next.profiles, c.profiles...)
	for id, i := range c.index {
		next.index[id] = i
	}
	if err := next.add(profiles); err != nil {
		return nil, err
	}
	return next, nil
}

func (c *Catalog) add(profiles []speechkit.ProviderProfile) error {
	for _, profile := range profiles {
		profile = ProviderProfileWithDefaults(profile)
		id := strings.TrimSpace(profile.ID)
		if id == "" {
			return fmt.Errorf("%w: empty id", ErrInvalidProfile)
		}
		if _, exists := c.index[id]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicateProfileID, id)
		}
		if err := speechkit.ValidateProfileForMode(profile, profile.Mode); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidProfile, err)
		}
		c.index[id] = len(c.profiles)
		c.profiles = append(c.profiles, profile)
	}
	return nil
}

// Profiles returns a copy of every profile in catalog order.
func (c *Catalog) Profiles() []speechkit.ProviderProfile {
	if c == nil {
		return nil
	}
	return append([]speechkit.ProviderProfile(nil), c.profiles...)
}

// ProfilesForMode returns the profiles that belong to mode, in catalog order.
func (c *Catalog) ProfilesForMode(mode speechkit.Mode) []speechkit.ProviderProfile {
	if c == nil {
		return nil
	}
	mode = speechkit.NormalizeMode(mode)
	var out []speechkit.ProviderProfile
	for _, profile := range c.profiles {
		if speechkit.NormalizeMode(profile.Mode) == mode {
			out = append(out, profile)
		}
	}
	return out
}

// Profile looks a profile up by id. Legacy ids are resolved through
// NormalizeProviderProfileID first.
func (c *Catalog) Profile(id string) (speechkit.ProviderProfile, bool) {
	if c == nil {
		return speechkit.ProviderProfile{}, false
	}
	i, ok := c.index[speechkit.NormalizeProviderProfileID(id)]
	if !ok {
		return speechkit.ProviderProfile{}, false
	}
	return c.profiles[i], true
}

// ProviderMatrix groups the catalog by provider with per-feature support, the
// same shape DefaultProviderMatrix returns for the built-ins.
func (c *Catalog) ProviderMatrix() []ProviderMatrixRow {
	if c == nil {
		return nil
	}
	return providerMatrixFor(c.profiles)
}

// ProviderDefaults returns the preferred profile per provider and mode, the
// same shape DefaultProviderDefaults returns for the built-ins.
func (c *Catalog) ProviderDefaults() []ProviderDefault {
	if c == nil {
		return nil
	}
	return providerDefaultsFromMatrix(c.ProviderMatrix())
}

// Filter applies a RuntimePolicy and returns the profiles a host may present.
func (c *Catalog) Filter(policy speechkit.RuntimePolicy) []speechkit.ProviderProfile {
	if c == nil {
		return nil
	}
	return speechkit.FilterProviderProfiles(c.profiles, policy)
}
