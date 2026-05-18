//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func verifyInstallerSignature(path string) error {
	policy, err := installerSignaturePolicyFromEnv()
	if err != nil {
		return err
	}
	cmd := buildInstallerSignatureCommand(path, policy)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("authenticode signature is not valid or not pinned to the expected signer: %s", msg)
	}
	return nil
}

type installerSignaturePolicy struct {
	ExpectedPublisher  string
	ExpectedThumbprint string
}

var (
	// Non-secret signer pins. Release builds may override these via -ldflags.
	installerSignatureDefaultPublisher  = "Kombify GmbH"
	installerSignatureDefaultThumbprint string
)

func installerSignaturePolicyFromEnv() (installerSignaturePolicy, error) {
	envPolicy := installerSignaturePolicy{
		ExpectedPublisher:  strings.TrimSpace(getEnvValue("SPEECHKIT_WINDOWS_SIGNING_PUBLISHER")),
		ExpectedThumbprint: normalizeCertThumbprint(getEnvValue("SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT")),
	}
	if envPolicy.configured() {
		// Environment variables take full precedence — enterprise override.
		return envPolicy, nil
	}

	policy := installerSignaturePolicy{
		ExpectedPublisher:  strings.TrimSpace(installerSignatureDefaultPublisher),
		ExpectedThumbprint: normalizeCertThumbprint(installerSignatureDefaultThumbprint),
	}

	// If a runtime thumbprint pin is configured (from [update] signature_pin_thumbprint
	// in the config file), it overrides the build-time default thumbprint. The
	// publisher check is preserved. This lets enterprise customers pin the cert
	// via their managed config without rebuilding the binary.
	if pin := normalizeCertThumbprint(signaturePinThumbprint); pin != "" {
		policy.ExpectedThumbprint = pin
	}

	if !policy.configured() {
		return installerSignaturePolicy{}, errors.New("installer signature policy is not configured: bundle a signing publisher/thumbprint or set SPEECHKIT_WINDOWS_SIGNING_PUBLISHER or SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT")
	}

	return policy, nil
}

func (p installerSignaturePolicy) configured() bool {
	return p.ExpectedPublisher != "" || p.ExpectedThumbprint != ""
}

var getEnvValue = func(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func buildInstallerSignatureCommand(path string, policy installerSignaturePolicy) *exec.Cmd {
	return exec.Command( // #nosec G204 -- PowerShell executable and command are fixed; installer path and policy are passed as arguments.
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		authenticodeVerificationScript(),
		path,
		policy.ExpectedPublisher,
		policy.ExpectedThumbprint,
	)
}

func authenticodeVerificationScript() string {
	return `
$sig = Get-AuthenticodeSignature -LiteralPath $args[0]
if ($sig.Status -ne 'Valid') { [Console]::Error.WriteLine($sig.StatusMessage); exit 1 }
if ($null -eq $sig.SignerCertificate) { [Console]::Error.WriteLine('missing signer certificate'); exit 1 }
$expectedPublisher = [string]$args[1]
$expectedThumbprint = ([string]$args[2]).ToUpperInvariant() -replace '[^0-9A-F]', ''
$subject = [string]$sig.SignerCertificate.Subject
$thumbprint = ([string]$sig.SignerCertificate.Thumbprint).ToUpperInvariant() -replace '[^0-9A-F]', ''
$publisherMatches = -not [string]::IsNullOrWhiteSpace($expectedPublisher) -and $subject.IndexOf($expectedPublisher, [System.StringComparison]::OrdinalIgnoreCase) -ge 0
$thumbprintMatches = -not [string]::IsNullOrWhiteSpace($expectedThumbprint) -and $thumbprint -eq $expectedThumbprint
if (-not ($publisherMatches -or $thumbprintMatches)) {
  [Console]::Error.WriteLine("signer does not match expected publisher '$expectedPublisher' or thumbprint '$expectedThumbprint'. Actual subject: $subject")
  exit 1
}
`
}

func normalizeCertThumbprint(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
