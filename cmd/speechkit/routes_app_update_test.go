package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/internal/router"
)

func init() {
	// Tests use httptest loopback servers; relax installer URL validation.
	appInstallerURLValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}
}

func TestAppVersionRouteHidesOlderLatestRelease(t *testing.T) {
	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	previousVersion := AppVersion
	AppVersion = "0.18.0"
	t.Cleanup(func() { AppVersion = previousVersion })

	updateMu.Lock()
	updateVersion = "0.17.0"
	updateURL = "https://example.com/releases/tag/v0.17.0"
	updateDownloadURL = "https://example.com/releases/download/v0.17.0/SpeechKit-Setup-v0.17.0.exe"
	updateDownloadName = "SpeechKit-Setup-v0.17.0.exe"
	updateDownloadSize = 42
	updateChecked = testNow()
	updateMu.Unlock()
	t.Cleanup(resetCachedLatestReleaseForTest)

	req := httptest.NewRequest(http.MethodGet, "/app/version", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got := payload["version"]; got != "0.18.0" {
		t.Fatalf("version = %#v, want %q", got, "0.18.0")
	}
	if _, ok := payload["latestVersion"]; ok {
		t.Fatalf("latestVersion should be omitted when latest release is older: %#v", payload)
	}
	if _, ok := payload["updateURL"]; ok {
		t.Fatalf("updateURL should be omitted when latest release is older: %#v", payload)
	}
	if _, ok := payload["downloadURL"]; ok {
		t.Fatalf("downloadURL should be omitted when latest release is older: %#v", payload)
	}
}

func TestAppUpdateRoutesDownloadInstallerAndOpenIt(t *testing.T) {
	payload := []byte("installer-binary")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	previousVersion := AppVersion
	AppVersion = "0.18.0"
	t.Cleanup(func() { AppVersion = previousVersion })

	updateMu.Lock()
	updateVersion = "0.19.1"
	updateURL = "https://example.com/releases/tag/v0.19.1"
	updateDownloadURL = server.URL + "/SpeechKit-Setup-v0.19.1.exe"
	updateDownloadName = "SpeechKit-Setup-v0.19.1.exe"
	updateDownloadSize = int64(len(payload))
	updateChecked = testNow()
	updateMu.Unlock()
	t.Cleanup(resetCachedLatestReleaseForTest)

	form := url.Values{"version": {"0.19.1"}}
	startReq := httptest.NewRequest(http.MethodPost, "/app/update/download", strings.NewReader(form.Encode()))
	startReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	startRec := httptest.NewRecorder()

	handler.ServeHTTP(startRec, startReq)

	if startRec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", startRec.Code, startRec.Body.String())
	}

	var started struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(startRec.Body).Decode(&started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.ID == "" {
		t.Fatal("expected download job id")
	}

	var jobs []struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		FilePath string `json:"filePath"`
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		jobsReq := httptest.NewRequest(http.MethodGet, "/app/update/jobs", http.NoBody)
		jobsRec := httptest.NewRecorder()
		handler.ServeHTTP(jobsRec, jobsReq)
		if jobsRec.Code != http.StatusOK {
			t.Fatalf("jobs status = %d, want %d", jobsRec.Code, http.StatusOK)
		}
		if err := json.NewDecoder(jobsRec.Body).Decode(&jobs); err != nil {
			t.Fatalf("decode jobs: %v", err)
		}
		if len(jobs) == 1 && jobs[0].Status == "done" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v, want 1 completed job", jobs)
	}
	if jobs[0].Status != "done" {
		t.Fatalf("job status = %q, want %q", jobs[0].Status, "done")
	}
	if jobs[0].FilePath == "" {
		t.Fatal("expected downloaded installer path")
	}
	data, err := os.ReadFile(jobs[0].FilePath)
	if err != nil {
		t.Fatalf("read downloaded installer: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("downloaded payload = %q, want %q", string(data), string(payload))
	}

	var openedPath string
	prevOpen := openInstallerFileInShell
	openInstallerFileInShell = func(path string) error {
		openedPath = path
		return nil
	}
	defer func() { openInstallerFileInShell = prevOpen }()

	openForm := url.Values{"job_id": {started.ID}}
	openReq := httptest.NewRequest(http.MethodPost, "/app/update/open", strings.NewReader(openForm.Encode()))
	openReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	openRec := httptest.NewRecorder()

	handler.ServeHTTP(openRec, openReq)

	if openRec.Code != http.StatusOK {
		t.Fatalf("open status = %d, body=%s", openRec.Code, openRec.Body.String())
	}
	if openedPath != jobs[0].FilePath {
		t.Fatalf("openedPath = %q, want %q", openedPath, jobs[0].FilePath)
	}
}

func resetCachedLatestReleaseForTest() {
	updateMu.Lock()
	defer updateMu.Unlock()
	updateVersion = ""
	updateURL = ""
	updateDownloadURL = ""
	updateDownloadName = ""
	updateDownloadSize = 0
	updateChecked = testZeroTime()
}
