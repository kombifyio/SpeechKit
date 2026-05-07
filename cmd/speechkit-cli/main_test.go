package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusCommandUsesServerAndToken(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/readyz" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test"})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--server", srv.URL, "--token", "secret", "status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if auth != "Bearer secret" {
		t.Fatalf("Authorization = %q", auth)
	}
	if !strings.Contains(stdout.String(), "status: ok") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestCatalogListPassesModeFilter(t *testing.T) {
	var gotMode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMode = r.URL.Query().Get("mode")
		if r.URL.Path != "/v1/catalog/profiles" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"profiles": []map[string]any{}})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--server", srv.URL, "--json", "catalog", "list", "--mode=dictation"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if gotMode != "dictation" {
		t.Fatalf("mode = %q, want dictation", gotMode)
	}
}

func TestStatusCommandAcceptsJSONFlagAfterCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test"})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--server", srv.URL, "status", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "ok"`) {
		t.Fatalf("stdout = %s, want JSON status", stdout.String())
	}
}

func TestInitCommandScaffoldsBrowserDictationTemplate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-demo")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"init",
		"--template", "browser-dictation-react",
		dir,
		"--var", "APP_NAME=agent-demo",
		"--var", "SPEECHKIT_SERVER_URL=http://localhost:8080",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	pkg, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	if !strings.Contains(string(pkg), `"name": "agent-demo"`) {
		t.Fatalf("package.json missing app name:\n%s", pkg)
	}
	env, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if !strings.Contains(string(env), "VITE_SPEECHKIT_SERVER_URL=http://localhost:8080") {
		t.Fatalf(".env.example missing server url:\n%s", env)
	}
}

func TestInitCommandListsTemplatesAsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "init", "--list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Name": "browser-dictation-react"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
