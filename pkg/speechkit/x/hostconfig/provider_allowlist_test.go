package hostconfig_test

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/hostconfig"
)

// A provider allowlist limits the profiles a host may present to the named
// providers; one that names no catalog provider admits nothing rather than
// everything.
func TestPolicyFromRestrictsProfilesToAllowedProviders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed []string
		want    map[string]bool
	}{
		{name: "named providers", allowed: []string{"local", "foundry"}, want: map[string]bool{"local": true, "foundry": true}},
		{name: "no catalog provider", allowed: []string{"github_copilot"}, want: map[string]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := hostconfig.Parse([]byte(""))
			if err != nil {
				t.Fatal(err)
			}
			cfg.General = hostconfig.General{DictateEnabled: true, AssistEnabled: true, VoiceAgentEnabled: true}
			cfg.Providers.Allowed = tc.allowed
			visible := speechkit.FilterProviderProfiles(catalog.DefaultProviderProfiles(), hostconfig.PolicyFrom(cfg))
			seen := map[string]bool{}
			for _, profile := range visible {
				family := catalog.ProviderFamily(catalog.ProviderIDForProfile(profile))
				if !tc.want[family] {
					t.Errorf("profile %s of provider %s is visible under allowlist %v", profile.ID, family, tc.allowed)
				}
				seen[family] = true
			}
			for family := range tc.want {
				if !seen[family] {
					t.Errorf("no profile of allowed provider %s is visible", family)
				}
			}
		})
	}
}
