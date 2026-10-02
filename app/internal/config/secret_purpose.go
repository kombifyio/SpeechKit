package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// kombify Cloud credential identities. They live here, not in the desktop's
// cloudauth package, because config is part of the public OSS export and must
// not import desktop-only packages; cloudauth defines its names from these.
const (
	// CloudSessionSecretName is the credential-store entry holding the
	// signed-in kombify Cloud session (refresh token included).
	CloudSessionSecretName = "SPEECHKIT_CLOUD_SESSION"
	// CloudBearerSecretName is the credential name the kombify Cloud server
	// target resolves its bearer from.
	CloudBearerSecretName = "KOMBIFY_CLOUD_TOKEN"
	// CloudServerURLEnv overrides the kombify Cloud server URL (staging or a
	// local Gateway); CloudDefaultServerURL is the production value.
	CloudServerURLEnv     = "SPEECHKIT_CLOUD_SERVER_URL"
	CloudDefaultServerURL = "https://api.kombify.io/v1/speechkit"
)

// SecretPurpose names what a credential reference ("*_env" setting) is used
// for. ResolveSecret is a generic lookup over the secret store, the process
// environment and Doppler; a purpose narrows which names may be looked up
// and where the value may be sent, so a name that arrives with a
// control-plane request can never select an unrelated credential (the
// kombify Cloud refresh token, a provider API key, AWS_SECRET_ACCESS_KEY).
type SecretPurpose int

const (
	// SecretPurposeServerBearer is the bearer token a device sends to its
	// SpeechKit server target ([server_connection].bearer_token_env).
	SecretPurposeServerBearer SecretPurpose = iota + 1
	// SecretPurposeServerBetaInstall is the edge-beta install id/secret pair
	// ([server_connection].beta_install_*_env).
	SecretPurposeServerBetaInstall
	// SecretPurposeHomeAssistant is the Home Assistant long-lived access
	// token ([assist.home_assistant].token_env).
	SecretPurposeHomeAssistant
	// SecretPurposeWakewordUpload is the bearer token the wake-word training
	// uploader sends with captured clips
	// ([wakeword.training_data].upload_token_env).
	SecretPurposeWakewordUpload
)

// DefaultWakewordUploadTokenEnv is the documented default of
// [wakeword.training_data].upload_token_env.
const DefaultWakewordUploadTokenEnv = "SPEECHKIT_TRAINING_TOKEN" //nolint:gosec // secret name, not a credential

// ErrSecretNameNotAllowed means a credential name is not acceptable for the
// purpose it was requested for, or not for the URL it would be sent to.
var ErrSecretNameNotAllowed = errors.New("credential name not allowed for this purpose")

var (
	serverBearerNamePattern = regexp.MustCompile(`^SPEECHKIT_SERVER_[A-Z0-9_]+$`)
	betaInstallNamePattern  = regexp.MustCompile(`^SPEECHKIT_BETA_INSTALL_[A-Z0-9_]+$`)
	// Home Assistant token names in the wild: SPEECHKIT_HOME_ASSISTANT_TOKEN
	// (default), HA_LONG_LIVED_TOKEN, KOMBIFY_HA_TOKEN.
	wakewordUploadNamePattern = regexp.MustCompile(`^SPEECHKIT_(?:WAKEWORD_UPLOAD|TRAINING)_(?:[A-Z0-9]+_)*TOKEN$`)
	homeAssistantNamePattern  = regexp.MustCompile(`^(?:[A-Z0-9]+_)*(?:HA|HASS|HOME_ASSISTANT|HOMEASSISTANT)_(?:[A-Z0-9]+_)*TOKEN$`)
)

// defaultServerBearerEnv is the documented default of
// [server_connection].bearer_token_env.
const defaultServerBearerEnv = "SPEECHKIT_SERVER_TOKEN"

// IsInternalSecretName reports whether name is a secret-store entry SpeechKit
// writes for its own use (sign-in sessions, Entra token records, app client
// secrets). Those are never sent anywhere through a configurable credential
// name, whatever the purpose.
func IsInternalSecretName(name string) bool {
	n := strings.ToUpper(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	switch n {
	case CloudSessionSecretName, strings.ToUpper(DefaultFoundryEntraClientSecretEnv):
		return true
	}
	return strings.HasSuffix(n, "_SESSION") ||
		strings.HasSuffix(n, "_ENTRA_RECORD") ||
		strings.HasSuffix(n, "_CLIENT_SECRET")
}

// IsKombifyCloudURL reports whether raw is an https URL on a kombify Cloud
// host: the production hosts, or the host of the sign-in's server URL (which
// SPEECHKIT_CLOUD_SERVER_URL can point at a staging Gateway). The host is
// compared exactly, never by substring.
func IsKombifyCloudURL(raw string) bool {
	if host, ok := httpsHost(raw); ok && (host == "api.kombify.io" || host == "speechkit.kombify.io") {
		return true
	}
	// A staging or local Gateway configured for the sign-in (environment,
	// not control plane) is matched on its exact scheme, host and port.
	return sameOrigin(raw, cloudServerURL())
}

// cloudServerURL is the kombify Cloud server URL the sign-in uses: the
// environment override, else production.
func cloudServerURL() string {
	if v := strings.TrimSpace(os.Getenv(CloudServerURLEnv)); v != "" {
		return v
	}
	return CloudDefaultServerURL
}

func sameOrigin(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil || ua.Host == "" || ua.User != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host)
}

func httpsHost(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host, host != ""
}

// CheckSecretBinding applies the rules every credential name obeys, however
// it was configured: internal store entries are never resolved, and the
// kombify Cloud bearer is only ever sent, as a server bearer, to kombify
// Cloud. targetURL is where the value would be sent.
func CheckSecretBinding(purpose SecretPurpose, name, targetURL string) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return nil
	}
	if IsInternalSecretName(n) {
		return fmt.Errorf("%w: %q is managed by SpeechKit itself", ErrSecretNameNotAllowed, n)
	}
	if strings.EqualFold(n, CloudBearerSecretName) {
		if purpose != SecretPurposeServerBearer || !IsKombifyCloudURL(targetURL) {
			return fmt.Errorf("%w: %q is only sent to kombify Cloud", ErrSecretNameNotAllowed, n)
		}
	}
	return nil
}

// ValidateRequestedSecretName checks a credential name that arrived with a
// control-plane request rather than from config.toml. On top of
// CheckSecretBinding it accepts only the names that belong to purpose; a
// name set by hand in config.toml is the user's choice and is not passed
// through here.
func ValidateRequestedSecretName(purpose SecretPurpose, name, targetURL string) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return nil
	}
	if err := CheckSecretBinding(purpose, n, targetURL); err != nil {
		return err
	}
	upper := strings.ToUpper(n)
	switch purpose {
	case SecretPurposeServerBearer:
		if upper == defaultServerBearerEnv || upper == CloudBearerSecretName || serverBearerNamePattern.MatchString(upper) {
			return nil
		}
		return fmt.Errorf("%w: server token names must be %s or start with SPEECHKIT_SERVER_ (got %q)", ErrSecretNameNotAllowed, defaultServerBearerEnv, n)
	case SecretPurposeServerBetaInstall:
		if upper == DefaultBetaInstallIDEnv || upper == DefaultBetaInstallSecretEnv || betaInstallNamePattern.MatchString(upper) {
			return nil
		}
		return fmt.Errorf("%w: beta install credential names must start with SPEECHKIT_BETA_INSTALL_ (got %q)", ErrSecretNameNotAllowed, n)
	case SecretPurposeHomeAssistant:
		if homeAssistantNamePattern.MatchString(upper) {
			return nil
		}
		return fmt.Errorf("%w: Home Assistant token names must look like SPEECHKIT_HOME_ASSISTANT_TOKEN or HA_..._TOKEN (got %q)", ErrSecretNameNotAllowed, n)
	case SecretPurposeWakewordUpload:
		if upper == DefaultWakewordUploadTokenEnv || wakewordUploadNamePattern.MatchString(upper) {
			return nil
		}
		return fmt.Errorf("%w: wake-word upload token names must be %s or SPEECHKIT_WAKEWORD_UPLOAD_..._TOKEN (got %q)", ErrSecretNameNotAllowed, DefaultWakewordUploadTokenEnv, n)
	default:
		return fmt.Errorf("%w: unknown purpose", ErrSecretNameNotAllowed)
	}
}

// ResolvePurposeSecret resolves a configured credential name for purpose
// after CheckSecretBinding. Callers that send the value somewhere use this
// instead of ResolveSecret.
func ResolvePurposeSecret(purpose SecretPurpose, name, targetURL string) (string, error) {
	if err := CheckSecretBinding(purpose, name, targetURL); err != nil {
		return "", err
	}
	return strings.TrimSpace(ResolveSecret(name)), nil
}
