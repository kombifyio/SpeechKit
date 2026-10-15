package config

import "testing"

// The user hive is writable by the user, so it may only close downloads, and
// never over a machine value.
func TestModelsSourceUserHiveMayOnlyChooseBundled(t *testing.T) {
	const mirror = "https://mirror.corp.example/models"
	cases := []struct {
		name          string
		machine, user string
		want          ModelSourceMode
	}{
		{"user asks for bundled, nothing pinned", "", ModelsSourceBundled, ModelSourceModeBundled},
		{"user cannot open the public hosts over bundled", ModelsSourceBundled, ModelsSourcePublic, ModelSourceModeBundled},
		{"user cannot swap in their own mirror", "", mirror, ModelSourceModePublic},
		{"machine mirror stays", mirror, ModelsSourceBundled, ModelSourceModeMirror},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			AttachPolicy(cfg, mergePolicyHives(PolicyValues{ModelsSource: tc.machine}, PolicyValues{}, PolicyValues{ModelsSource: tc.user}))
			if got := cfg.ModelSource().Mode; got != tc.want {
				t.Fatalf("model source = %q, want %q", got, tc.want)
			}
		})
	}
}

// A source that cannot be understood never reaches a public host: only an
// https mirror is a mirror, and anything else unusable behaves as bundled.
func TestModelsSourceFailsClosed(t *testing.T) {
	for _, v := range []string{"http://mirror.corp.example/models", "ftp://mirror.corp.example", "https://user:pw@mirror.corp.example", "https://mirror.corp.example/m?x=1", "mirror.corp.example", "\x7f"} {
		if _, err := ParseModelsSource(v); err == nil {
			t.Errorf("ParseModelsSource(%q) accepted", v)
		}
		cfg := defaults()
		cfg.Models.Source = v
		if got := cfg.ModelSource().Mode; got != ModelSourceModeBundled {
			t.Errorf("source %q resolves to %q, want bundled", v, got)
		}
	}
	cfg := defaults()
	cfg.Models.Source = " https://mirror.corp.example/models/ "
	if got := cfg.ModelSource(); got.Mode != ModelSourceModeMirror || got.MirrorBase != "https://mirror.corp.example/models" {
		t.Errorf("mirror = %+v", got)
	}
}

// Closing downloads while the product runs applies at once; opening them again
// waits for the next start.
func TestModelsSourceRefreshOnlyTightens(t *testing.T) {
	const mirror = "https://mirror.corp.example/models"
	cfg := defaults()
	AttachPolicy(cfg, mergePolicyHives(PolicyValues{ModelsSource: mirror}, PolicyValues{}, PolicyValues{}))

	RefreshPolicy(cfg, singleSourcePolicy(PolicyValues{ModelsSource: ModelsSourceBundled}, PolicySourceMachinePolicies))
	if got := cfg.ModelSource().Mode; got != ModelSourceModeBundled {
		t.Fatalf("after tightening to bundled: %q", got)
	}
	r := RefreshPolicy(cfg, singleSourcePolicy(PolicyValues{ModelsSource: mirror}, PolicySourceMachinePolicies))
	if got := cfg.ModelSource().Mode; got != ModelSourceModeBundled || len(r.Deferred) == 0 {
		t.Fatalf("a wider value applied at once: %q, deferred %v", got, r.Deferred)
	}
}
