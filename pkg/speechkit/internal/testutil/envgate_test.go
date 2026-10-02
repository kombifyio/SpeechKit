package testutil

import "testing"

func TestExternalTestConfigRequiredIncludesCIAndRequireFlags(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "none", env: map[string]string{}, want: false},
		{name: "ci true", env: map[string]string{ciEnv: "true"}, want: true},
		{name: "github actions", env: map[string]string{githubActionsEnv: "true"}, want: true},
		{name: "explicit e2e", env: map[string]string{RequireConfigEnv: "1"}, want: true},
		{name: "explicit speechkit live", env: map[string]string{RequireLiveTestEnv: "yes"}, want: true},
		{name: "false strings", env: map[string]string{ciEnv: "false", RequireConfigEnv: "0"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(name string) string {
				return tt.env[name]
			}
			if got := externalTestConfigRequired(getenv); got != tt.want {
				t.Fatalf("externalTestConfigRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExplicitExternalTestConfigRequiredIgnoresCI(t *testing.T) {
	getenv := func(name string) string {
		if name == ciEnv {
			return "true"
		}
		return ""
	}
	if explicitExternalTestConfigRequired(getenv) {
		t.Fatal("explicitExternalTestConfigRequired should ignore CI without an explicit require flag")
	}
}

func TestMissingConfigMessageIncludesGuidance(t *testing.T) {
	got := missingConfigMessage("OPENAI_API_KEY", "Inject credentials through Doppler.")
	want := "missing required test configuration: OPENAI_API_KEY. Inject credentials through Doppler."
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
