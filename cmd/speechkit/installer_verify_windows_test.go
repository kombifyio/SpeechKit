//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestInstallerSignaturePolicyUsesBundledPublisherWithoutUserEnv(t *testing.T) {
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_PUBLISHER", "")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT", "")

	policy, err := installerSignaturePolicyFromEnv()
	if err != nil {
		t.Fatalf("bundled publisher should be accepted without user env: %v", err)
	}
	if policy.ExpectedPublisher != "Kombify GmbH" {
		t.Fatalf("ExpectedPublisher = %q", policy.ExpectedPublisher)
	}
}

func TestInstallerSignaturePolicyUsesBundledThumbprintWithoutUserEnv(t *testing.T) {
	withInstallerSignatureDefaults(t, "", " aa bb cc ")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_PUBLISHER", "")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT", "")

	policy, err := installerSignaturePolicyFromEnv()
	if err != nil {
		t.Fatalf("bundled thumbprint should be accepted without user env: %v", err)
	}
	if policy.ExpectedThumbprint != "AABBCC" {
		t.Fatalf("ExpectedThumbprint = %q", policy.ExpectedThumbprint)
	}
}

func TestInstallerSignaturePolicyFailsClosedWithoutBundledOrEnvPolicy(t *testing.T) {
	withInstallerSignatureDefaults(t, "", "")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_PUBLISHER", "")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT", "")

	if _, err := installerSignaturePolicyFromEnv(); err == nil {
		t.Fatal("expected missing pinned signer policy to fail closed")
	}
}

func TestInstallerSignaturePolicyAcceptsPublisherOrThumbprint(t *testing.T) {
	withInstallerSignatureDefaults(t, "Bundled Publisher", "FFEEDD")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_PUBLISHER", "Env Publisher")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT", "")

	policy, err := installerSignaturePolicyFromEnv()
	if err != nil {
		t.Fatalf("publisher policy should be accepted: %v", err)
	}
	if policy.ExpectedPublisher != "Env Publisher" {
		t.Fatalf("ExpectedPublisher = %q", policy.ExpectedPublisher)
	}
	if policy.ExpectedThumbprint != "" {
		t.Fatalf("ExpectedThumbprint = %q", policy.ExpectedThumbprint)
	}

	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_PUBLISHER", "")
	t.Setenv("SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT", " aa bb cc ")
	policy, err = installerSignaturePolicyFromEnv()
	if err != nil {
		t.Fatalf("thumbprint policy should be accepted: %v", err)
	}
	if policy.ExpectedThumbprint != "AABBCC" {
		t.Fatalf("ExpectedThumbprint = %q", policy.ExpectedThumbprint)
	}
	if policy.ExpectedPublisher != "" {
		t.Fatalf("ExpectedPublisher = %q", policy.ExpectedPublisher)
	}
}

func TestBuildInstallerSignatureCommandChecksPinnedSigner(t *testing.T) {
	policy := installerSignaturePolicy{
		ExpectedPublisher:  "Kombify GmbH",
		ExpectedThumbprint: "AABBCC",
	}
	cmd := buildInstallerSignatureCommand("C:\\Temp\\SpeechKit-Setup.exe", policy)
	script := strings.Join(cmd.Args, " ")

	for _, want := range []string{
		"Get-AuthenticodeSignature",
		"SignerCertificate.Subject",
		"Kombify GmbH",
		"Thumbprint",
		"AABBCC",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("signature verification command should contain %q; args=%v", want, cmd.Args)
		}
	}
}

func withInstallerSignatureDefaults(t *testing.T, publisher, thumbprint string) {
	t.Helper()

	previousPublisher := installerSignatureDefaultPublisher
	previousThumbprint := installerSignatureDefaultThumbprint
	installerSignatureDefaultPublisher = publisher
	installerSignatureDefaultThumbprint = thumbprint
	t.Cleanup(func() {
		installerSignatureDefaultPublisher = previousPublisher
		installerSignatureDefaultThumbprint = previousThumbprint
	})
}
