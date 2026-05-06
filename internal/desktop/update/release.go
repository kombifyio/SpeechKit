package update

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	InstallModeVerified       = "verified_installable"
	InstallModeManualUnsigned = "manual_unsigned"
	InstallModeUnavailable    = "unavailable"
)

type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	ContentType        string `json:"content_type"`
	Size               int64  `json:"size"`
}

type ReleaseInfo struct {
	Version      string
	ReleaseURL   string
	DownloadURL  string
	DownloadName string
	DownloadSize int64
	InstallMode  string
}

func SelectWindowsInstallerAsset(assets []ReleaseAsset) (string, string, int64) {
	bestScore := -1
	var bestName string
	var bestURL string
	var bestSize int64

	for _, asset := range assets {
		score := InstallerAssetScore(asset.Name, asset.ContentType)
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

func HasUnsignedWindowsNotice(assets []ReleaseAsset) bool {
	for _, asset := range assets {
		if strings.EqualFold(filepath.Base(strings.TrimSpace(asset.Name)), "UNSIGNED-WINDOWS-RELEASE.txt") {
			return true
		}
	}
	return false
}

func NormalizeInstallMode(mode, downloadURL string) string {
	switch strings.TrimSpace(mode) {
	case InstallModeVerified, InstallModeManualUnsigned, InstallModeUnavailable:
		return strings.TrimSpace(mode)
	}
	if strings.TrimSpace(downloadURL) != "" {
		return InstallModeVerified
	}
	return InstallModeUnavailable
}

func InstallerAssetScore(name, contentType string) int {
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

func ResolveDir(cfgPath, exeDir, localAppData, tempDir string) string {
	if cfgPath != "" {
		return filepath.Join(filepath.Dir(cfgPath), "updates")
	}
	if exeDir := strings.TrimSpace(exeDir); exeDir != "" {
		return filepath.Join(exeDir, "updates")
	}
	if localAppData := strings.TrimSpace(localAppData); localAppData != "" {
		return filepath.Join(localAppData, "SpeechKit", "updates")
	}
	return filepath.Join(tempDir, "SpeechKit", "updates")
}

func InstallerAssetName(release ReleaseInfo) string {
	if release.DownloadName != "" {
		return filepath.Base(release.DownloadName)
	}
	if release.DownloadURL != "" {
		if name := filepath.Base(release.DownloadURL); name != "" && name != "." && name != "/" {
			return name
		}
	}
	return fmt.Sprintf("SpeechKit-Setup-v%s.exe", release.Version)
}

func IsInstallerAssetName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".exe" || ext == ".msi"
}
