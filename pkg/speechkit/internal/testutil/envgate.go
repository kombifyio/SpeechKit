package testutil

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Environment variables that turn a missing external test configuration from
// a skip into a failure.
const (
	RequireConfigEnv       = "E2E_REQUIRE_CONFIG"
	RequireLiveTestEnv     = "SPEECHKIT_REQUIRE_LIVE_TEST_CONFIG"
	ciEnv                  = "CI"
	githubActionsEnv       = "GITHUB_ACTIONS"
	missingConfigMsgPrefix = "missing required test configuration"
)

// ExternalTestConfigRequired reports whether missing external test
// configuration must fail instead of skip (explicit opt-in or CI).
func ExternalTestConfigRequired() bool {
	return externalTestConfigRequired(os.Getenv)
}

// ExplicitExternalTestConfigRequired is like ExternalTestConfigRequired but
// ignores the implicit CI signal.
func ExplicitExternalTestConfigRequired() bool {
	return explicitExternalTestConfigRequired(os.Getenv)
}

// RequireEnvOrSkip returns the trimmed value of envName, or skips (or fails,
// when configuration is required) with guidance.
func RequireEnvOrSkip(t testing.TB, envName, guidance string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(envName))
	if value != "" {
		return value
	}
	SkipOrFailMissingConfig(t, envName, guidance)
	return ""
}

// RequireAnyEnvOrSkip returns the first non-empty value among envNames, or
// skips (or fails) with guidance.
func RequireAnyEnvOrSkip(t testing.TB, envNames []string, guidance string) string {
	t.Helper()
	for _, envName := range envNames {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value
		}
	}
	SkipOrFailMissingConfig(t, strings.Join(envNames, " or "), guidance)
	return ""
}

// SkipOrFailMissingConfig skips the test, or fails it when external test
// configuration is required.
func SkipOrFailMissingConfig(t testing.TB, missing, guidance string) {
	t.Helper()
	message := missingConfigMessage(missing, guidance)
	if ExternalTestConfigRequired() {
		t.Fatal(message)
	}
	t.Skip(message)
}

// SkipOrFailExplicitMissingConfig skips the test, or fails it only when
// configuration was explicitly required.
func SkipOrFailExplicitMissingConfig(t testing.TB, missing, guidance string) {
	t.Helper()
	message := missingConfigMessage(missing, guidance)
	if ExplicitExternalTestConfigRequired() {
		t.Fatal(message)
	}
	t.Skip(message)
}

func missingConfigMessage(missing, guidance string) string {
	message := fmt.Sprintf("%s: %s", missingConfigMsgPrefix, strings.TrimSpace(missing))
	if trimmed := strings.TrimSpace(guidance); trimmed != "" {
		message += ". " + trimmed
	}
	return message
}

func externalTestConfigRequired(getenv func(string) string) bool {
	return explicitExternalTestConfigRequired(getenv) || envFlag(getenv(ciEnv)) || envFlag(getenv(githubActionsEnv))
}

func explicitExternalTestConfigRequired(getenv func(string) string) bool {
	return envFlag(getenv(RequireConfigEnv)) || envFlag(getenv(RequireLiveTestEnv))
}

func envFlag(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
