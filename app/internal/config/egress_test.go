package config_test

import (
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// A running policy refresh that tightens the network scope re-applies the
// process-wide egress policy at once, as a settings save would.
func TestPolicyRefreshTighteningReappliesEgressPolicy(t *testing.T) {
	t.Cleanup(func() { netsec.Configure(netsec.Policy{}) })
	cfg := &config.Config{}
	cfg.Privacy.NetworkScope = config.NetworkScopeOpen
	config.ApplyEgressPolicy(cfg)
	shared := config.NewShared(filepath.Join(t.TempDir(), "config.toml"), cfg)
	t.Cleanup(config.WatchEgressPolicy(shared))

	shared.RefreshPolicy(config.PolicyValues{PrivacyNetworkScope: config.NetworkScopeDeviceOnly})

	if netsec.CurrentPolicy().AllowsPublicEgress() {
		t.Fatal("the tightened scope did not reach the egress policy")
	}
}
