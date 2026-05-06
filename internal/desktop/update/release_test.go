package update

import "testing"

func TestHasUnsignedWindowsNotice(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "SpeechKit-Setup.exe"},
		{Name: "UNSIGNED-WINDOWS-RELEASE.txt"},
	}

	if !HasUnsignedWindowsNotice(assets) {
		t.Fatal("unsigned Windows notice was not detected")
	}
}

func TestNormalizeInstallModeInfersVerifiedOnlyWhenDownloadExists(t *testing.T) {
	if got := NormalizeInstallMode("", "https://example.com/SpeechKit-Setup.exe"); got != InstallModeVerified {
		t.Fatalf("mode with download = %q, want %q", got, InstallModeVerified)
	}
	if got := NormalizeInstallMode("", ""); got != InstallModeUnavailable {
		t.Fatalf("mode without download = %q, want %q", got, InstallModeUnavailable)
	}
}

func TestSelectWindowsInstallerAssetPrefersSpeechKitSetupExe(t *testing.T) {
	name, url, size := SelectWindowsInstallerAsset([]ReleaseAsset{
		{Name: "notes.txt", BrowserDownloadURL: "https://example.com/notes.txt", Size: 1},
		{Name: "SpeechKit-Setup.exe", BrowserDownloadURL: "https://example.com/SpeechKit-Setup.exe", ContentType: "application/octet-stream", Size: 42},
		{Name: "Other.msi", BrowserDownloadURL: "https://example.com/Other.msi", ContentType: "application/octet-stream", Size: 24},
	})

	if name != "SpeechKit-Setup.exe" || url != "https://example.com/SpeechKit-Setup.exe" || size != 42 {
		t.Fatalf("selected asset = %q %q %d", name, url, size)
	}
}
