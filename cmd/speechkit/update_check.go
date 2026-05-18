package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	desktopupdate "github.com/kombifyio/SpeechKit/internal/desktop/update"
	"github.com/kombifyio/SpeechKit/internal/netsec"
)

var (
	updateMu           sync.Mutex
	updateVersion      string
	updateURL          string
	updateDownloadURL  string
	updateDownloadName string
	updateDownloadSize int64
	updateInstallMode  string
	updateChecked      time.Time
)

const (
	updateCheckTimeout = 5 * time.Second

	appUpdateInstallModeVerified       = desktopupdate.InstallModeVerified
	appUpdateInstallModeManualUnsigned = desktopupdate.InstallModeManualUnsigned
	appUpdateInstallModeUnavailable    = desktopupdate.InstallModeUnavailable
)

var (
	releaseAPIURLValidation = netsec.ValidationOptions{}

	// updateCheckInterval, releaseAPIURL, updateEnabled, and
	// signaturePinThumbprint are populated by configureUpdateChannel during
	// startup from internal/config.UpdateConfig.
	// When enabled = false, refreshLatestRelease() and cachedLatestRelease()
	// both become no-ops — no outbound HTTP is ever attempted (air-gap mode).
	// Defaults mirror the historical hardcoded values so unit tests that
	// run without explicit configuration behave as before.
	updateCheckInterval    = 6 * time.Hour
	releaseAPIURL          = "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest"
	updateEnabled          = true
	signaturePinThumbprint string // optional runtime Authenticode SHA-1 thumbprint from config
)

// configureUpdateChannel must be called once during startup with values
// resolved from internal/config.UpdateConfig. Passing enabled=false fully
// disables the auto-update path. Passing an empty manifestURL or invalid
// URL also disables it (with a warning log). pinThumbprint is an optional
// Authenticode SHA-1 thumbprint; when non-empty it is stored and used by
// installer signature verification as an additional cert-pin check.
func configureUpdateChannel(enabled bool, manifestURL string, checkIntervalHours int, pinThumbprint string) {
	updateEnabled = enabled
	if manifestURL != "" {
		releaseAPIURL = manifestURL
	}
	if checkIntervalHours > 0 {
		updateCheckInterval = time.Duration(checkIntervalHours) * time.Hour
	}
	signaturePinThumbprint = strings.TrimSpace(pinThumbprint)
	if enabled && releaseAPIURL != "" {
		if err := netsec.ValidateProviderURL(releaseAPIURL, releaseAPIURLValidation); err != nil {
			slog.Warn("update manifest URL rejected by netsec, disabling updates",
				"url", releaseAPIURL, "err", err)
			updateEnabled = false
		}
	}
}

// disableTelemetry applies the --no-telemetry CLI override to runtime state.
// It is a standalone function (rather than inline logic) so that tests can
// exercise the override path without invoking flag.Parse.
func disableTelemetry() {
	configureUpdateChannel(false, releaseAPIURL, 0, signaturePinThumbprint)
}

type latestReleaseInfo = desktopupdate.ReleaseInfo
type releaseAssetInfo = desktopupdate.ReleaseAsset

func cachedLatestRelease() (latestReleaseInfo, bool) {
	if !updateEnabled {
		return latestReleaseInfo{}, false
	}
	updateMu.Lock()
	info := latestReleaseInfo{
		Version:      updateVersion,
		ReleaseURL:   updateURL,
		DownloadURL:  updateDownloadURL,
		DownloadName: updateDownloadName,
		DownloadSize: updateDownloadSize,
		InstallMode:  desktopupdate.NormalizeInstallMode(updateInstallMode, updateDownloadURL),
	}
	checked := updateChecked
	updateMu.Unlock()

	if !checked.IsZero() && time.Since(checked) < updateCheckInterval {
		return info, true
	}

	// Trigger background refresh; must not inherit request context as the refresh
	// should outlive the HTTP request that triggered it.
	go refreshLatestRelease() //nolint:contextcheck // background refresh should not be bound to request context
	return info, info.Version != ""
}

func refreshLatestRelease() {
	if !updateEnabled || releaseAPIURL == "" {
		return
	}
	updateMu.Lock()
	if !updateChecked.IsZero() && time.Since(updateChecked) < updateCheckInterval {
		updateMu.Unlock()
		return
	}
	updateMu.Unlock()

	client := netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: updateCheckTimeout, DialValidation: &releaseAPIURLValidation})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, releaseAPIURL, http.NoBody)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		return
	}

	var release struct {
		TagName string             `json:"tag_name"`
		HTMLURL string             `json:"html_url"`
		Assets  []releaseAssetInfo `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}

	version := normalizeReleaseVersion(release.TagName)
	assetName, downloadURL, downloadSize := selectWindowsInstallerAsset(release.Assets)
	installMode := appUpdateInstallModeUnavailable
	if desktopupdate.HasUnsignedWindowsNotice(release.Assets) {
		assetName = ""
		downloadURL = ""
		downloadSize = 0
		installMode = appUpdateInstallModeManualUnsigned
	} else if downloadURL != "" {
		installMode = appUpdateInstallModeVerified
	}

	updateMu.Lock()
	updateVersion = version
	updateURL = release.HTMLURL
	updateDownloadURL = downloadURL
	updateDownloadName = assetName
	updateDownloadSize = downloadSize
	updateInstallMode = installMode
	updateChecked = testNow()
	updateMu.Unlock()
}

func normalizeReleaseVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func selectWindowsInstallerAsset(assets []releaseAssetInfo) (string, string, int64) {
	return desktopupdate.SelectWindowsInstallerAsset(assets)
}

func isNewerReleaseVersion(latest, current string) bool {
	return compareSemanticVersions(latest, current) > 0
}

func compareSemanticVersions(left, right string) int {
	leftParts, leftOK := parseSemanticVersion(left)
	rightParts, rightOK := parseSemanticVersion(right)
	if !leftOK || !rightOK {
		return 0
	}

	limit := len(leftParts)
	if len(rightParts) > limit {
		limit = len(rightParts)
	}
	for idx := range limit {
		var l int
		var r int
		if idx < len(leftParts) {
			l = leftParts[idx]
		}
		if idx < len(rightParts) {
			r = rightParts[idx]
		}
		switch {
		case l > r:
			return 1
		case l < r:
			return -1
		}
	}
	return 0
}

func parseSemanticVersion(raw string) ([]int, bool) {
	version := strings.TrimSpace(strings.TrimPrefix(raw, "v"))
	if version == "" {
		return nil, false
	}

	if cut := strings.IndexAny(version, "-+"); cut >= 0 {
		version = version[:cut]
	}
	parts := strings.Split(version, ".")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

func testNow() time.Time {
	return time.Now()
}

func testZeroTime() time.Time {
	return time.Time{}
}
