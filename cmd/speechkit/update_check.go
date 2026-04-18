package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

var (
	updateMu           sync.Mutex
	updateVersion      string
	updateURL          string
	updateDownloadURL  string
	updateDownloadName string
	updateDownloadSize int64
	updateChecked      time.Time
)

const (
	updateCheckInterval = 6 * time.Hour
	updateCheckTimeout  = 5 * time.Second
	releaseAPIURL       = "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest"
)

type latestReleaseInfo struct {
	Version      string
	ReleaseURL   string
	DownloadURL  string
	DownloadName string
	DownloadSize int64
}

func cachedLatestRelease() (latestReleaseInfo, bool) {
	updateMu.Lock()
	info := latestReleaseInfo{
		Version:      updateVersion,
		ReleaseURL:   updateURL,
		DownloadURL:  updateDownloadURL,
		DownloadName: updateDownloadName,
		DownloadSize: updateDownloadSize,
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
	updateMu.Lock()
	if !updateChecked.IsZero() && time.Since(updateChecked) < updateCheckInterval {
		updateMu.Unlock()
		return
	}
	updateMu.Unlock()

	client := netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: updateCheckTimeout})
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
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			ContentType        string `json:"content_type"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}

	version := normalizeReleaseVersion(release.TagName)
	assetName, downloadURL, downloadSize := selectWindowsInstallerAsset(release.Assets)

	updateMu.Lock()
	updateVersion = version
	updateURL = release.HTMLURL
	updateDownloadURL = downloadURL
	updateDownloadName = assetName
	updateDownloadSize = downloadSize
	updateChecked = testNow()
	updateMu.Unlock()
}

func normalizeReleaseVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func selectWindowsInstallerAsset(assets []struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	ContentType        string `json:"content_type"`
	Size               int64  `json:"size"`
}) (string, string, int64) {
	bestScore := -1
	var bestName string
	var bestURL string
	var bestSize int64

	for _, asset := range assets {
		score := installerAssetScore(asset.Name, asset.ContentType)
		if score <= bestScore {
			continue
		}
		bestScore = score
		bestName = asset.Name
		bestURL = asset.BrowserDownloadURL
		bestSize = asset.Size
	}

	if bestScore <= 0 {
		return "", "", 0
	}
	return bestName, bestURL, bestSize
}

func installerAssetScore(name, contentType string) int {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	if base == "" {
		return 0
	}

	score := 0
	switch filepath.Ext(base) {
	case ".exe":
		score += 100
	case ".msi":
		score += 90
	default:
		return 0
	}

	if strings.Contains(base, "speechkit") {
		score += 20
	}
	if strings.Contains(base, "setup") || strings.Contains(base, "installer") {
		score += 20
	}
	if strings.Contains(strings.ToLower(contentType), "application") {
		score += 5
	}

	return score
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
