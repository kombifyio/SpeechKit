package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

var (
	updateMu      sync.Mutex
	updateVersion string
	updateURL     string
	updateChecked time.Time
)

const (
	updateCheckInterval = 6 * time.Hour
	updateCheckTimeout  = 5 * time.Second
	releaseAPIURL       = "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest"
)

func cachedLatestRelease() (version, url string, ok bool) {
	updateMu.Lock()
	ver, u, checked := updateVersion, updateURL, updateChecked
	updateMu.Unlock()

	if !checked.IsZero() && time.Since(checked) < updateCheckInterval {
		return ver, u, true
	}

	// Trigger background refresh
	go refreshLatestRelease()
	return ver, u, ver != ""
}

func refreshLatestRelease() {
	updateMu.Lock()
	if !updateChecked.IsZero() && time.Since(updateChecked) < updateCheckInterval {
		updateMu.Unlock()
		return
	}
	updateMu.Unlock()

	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Get(releaseAPIURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}

	version := release.TagName
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}

	updateMu.Lock()
	updateVersion = version
	updateURL = release.HTMLURL
	updateChecked = time.Now()
	updateMu.Unlock()
}
