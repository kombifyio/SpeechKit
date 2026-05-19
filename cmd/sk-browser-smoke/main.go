// Command sk-browser-smoke drives the public smoke page (`/`) of a
// running speechkit-server instance from a real headless Chrome
// process and asserts that every mode tile reports OK.
//
// Purpose: catch browser-only regressions that the API-level sk-e2e
// smoke cannot see — e.g. missing Authorization header injection,
// WebSocket Origin rejection, CORS preflight breakage, or the smoke
// page's own JS guard collapsing under server-side rewrites. These
// are exactly the bug classes that produced the v0.35.3 → v0.35.5
// hotfix train; an automated browser pass on every deploy makes
// that class impossible to ship.
//
// Usage:
//
//	sk-browser-smoke --origin https://speechkit.kombify.io
//	sk-browser-smoke --origin $URL --timeout 3m --keep-browser-open
//
// Exit codes:
//
//	0 — all six tiles (Server Settings, Health, Readiness,
//	    Dictation, Assist, Voice Agent) reported OK before the
//	    timeout expired
//	1 — at least one tile reported FAIL, or the overall status
//	    is anything other than "Passing", or the timeout expired
//	    while a tile was still RUNNING
//	2 — invalid CLI usage / browser-startup error / navigation
//	    failed
//
// Designed to run unattended in CI. The headless Chrome is the
// system-provided binary; on GitHub-hosted ubuntu-24.04 runners
// that is /usr/bin/google-chrome and needs no extra install step.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	defaultOrigin  = "https://speechkit.kombify.io"
	defaultTimeout = 3 * time.Minute

	// runSmokeButtonSelector matches the smoke-page "Start Smoke Test"
	// button by id, defined in internal/server/onboarding/assets/testui.html.
	runSmokeButtonSelector = `#runSmoke`

	// passingHeaderText is the literal status the smoke page paints
	// in its header chip when every tile resolved to OK.
	passingHeaderText = "Passing"

	// failedHeaderText short-circuits the wait when the page itself
	// has already concluded that one or more tiles failed.
	failedHeaderText = "Failed"

	// expectedTiles enumerates the tiles the smoke page renders in
	// the order the user reads them. Each tile name must surface in
	// .check h3 text after Start Smoke Test runs. The assertion is
	// "every name reports OK", so extending the smoke page later is
	// fine as long as the new tile also reports OK on a healthy server.
	expectedTiles = "Server Settings,Health,Readiness,Dictation,Assist,Voice Agent"
)

func main() {
	if exit := run(); exit != 0 {
		os.Exit(exit)
	}
}

func run() int {
	origin := flag.String("origin", defaultOrigin, "deployed server origin to probe")
	timeout := flag.Duration("timeout", defaultTimeout, "max wall time for the full smoke pass (incl. page load + every tile)")
	verbose := flag.Bool("v", false, "verbose: print every tile transition")
	keepOpen := flag.Bool("keep-browser-open", false, "do not close Chrome on exit; useful for local manual inspection")
	flag.Parse()

	base := strings.TrimRight(strings.TrimSpace(*origin), "/")
	if base == "" {
		fmt.Fprintln(os.Stderr, "sk-browser-smoke: --origin must not be empty")
		return 2
	}

	required := splitCSV(expectedTiles)
	if len(required) == 0 {
		fmt.Fprintln(os.Stderr, "sk-browser-smoke: internal error — empty expectedTiles")
		return 2
	}

	rootCtx, cancelRoot := context.WithTimeout(context.Background(), *timeout)
	defer cancelRoot()

	allocOpts := append([]chromedp.ExecAllocatorOption{},
		chromedp.DefaultExecAllocatorOptions[:]...,
	)
	allocOpts = append(allocOpts,
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("headless", "new"),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(rootCtx, allocOpts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	if !*keepOpen {
		defer cancelBrowser()
	}

	fmt.Printf("sk-browser-smoke: opening %s/ in headless Chrome (timeout %s)\n", base, timeout)

	if err := chromedp.Run(browserCtx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(runSmokeButtonSelector, chromedp.ByID),
	); err != nil {
		fmt.Fprintf(os.Stderr, "sk-browser-smoke: navigate to %s/ failed: %v\n", base, err)
		return 2
	}

	if err := chromedp.Run(browserCtx, chromedp.Click(runSmokeButtonSelector, chromedp.ByID)); err != nil {
		fmt.Fprintf(os.Stderr, "sk-browser-smoke: clicking #runSmoke failed: %v\n", err)
		return 2
	}

	fmt.Println("sk-browser-smoke: clicked Start Smoke Test, watching tiles…")

	if err := waitForAllTiles(browserCtx, required, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "sk-browser-smoke: %v\n", err)
		return 1
	}

	fmt.Println("sk-browser-smoke: all tiles reported OK, page header is Passing")
	return 0
}

// tileState is a snapshot of one .check block at a moment in time.
type tileState struct {
	Title  string `json:"title"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// pageState is the JS payload the smoke loop returns each tick.
type pageState struct {
	Header string      `json:"header"`
	Tiles  []tileState `json:"tiles"`
}

// readSmokePageStateJS is evaluated in the page on every poll tick.
// It returns the header chip text plus a normalised view of every
// .check block (title, current status, short detail prefix). The
// shape lives in JS rather than as separate selectors so we touch
// the DOM once per tick.
const readSmokePageStateJS = `(() => {
  const header = (document.querySelector('.overall, [class*="overall"]') || {}).textContent || "";
  const blocks = document.querySelectorAll('.check');
  const out = [];
  blocks.forEach((c) => {
    const lines = (c.innerText || "").split("\n");
    const title = (lines[0] || "").trim();
    const status = (lines[1] || "").trim();
    const detail = lines.slice(2).join(" ").slice(0, 200);
    out.push({ title, status, detail });
  });
  return { header: header.trim(), tiles: out };
})()`

func waitForAllTiles(ctx context.Context, required []string, verbose bool) error {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	lastStatus := map[string]string{}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for all tiles to report OK: %w (last state: %s)", ctx.Err(), formatLastStatus(lastStatus))
		case <-ticker.C:
		}

		var state pageState
		if err := chromedp.Run(ctx, chromedp.Evaluate(readSmokePageStateJS, &state)); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("timed out polling smoke page: %w", err)
			}
			return fmt.Errorf("evaluate smoke page state: %w", err)
		}

		seen := map[string]string{}
		for _, tile := range state.Tiles {
			seen[tile.Title] = strings.ToUpper(strings.TrimSpace(tile.Status))
		}
		if verbose {
			diffAndLog(lastStatus, seen)
		}
		lastStatus = seen

		// Hard failure short-circuit: any tile or the header reports FAIL.
		if strings.EqualFold(strings.TrimSpace(state.Header), failedHeaderText) {
			return failureReport("page header reports Failed", state)
		}
		for _, tile := range state.Tiles {
			if strings.EqualFold(strings.TrimSpace(tile.Status), "FAIL") {
				return failureReport(fmt.Sprintf("tile %q reports FAIL", tile.Title), state)
			}
		}

		// Success: every required tile is present AND OK.
		if allRequiredOK(required, seen) && strings.EqualFold(strings.TrimSpace(state.Header), passingHeaderText) {
			return nil
		}
	}
}

func allRequiredOK(required []string, seen map[string]string) bool {
	for _, name := range required {
		status, ok := seen[name]
		if !ok {
			return false
		}
		if status != "OK" {
			return false
		}
	}
	return true
}

func failureReport(headline string, state pageState) error {
	var b strings.Builder
	b.WriteString(headline)
	b.WriteString("\nheader: ")
	b.WriteString(state.Header)
	b.WriteString("\ntiles:")
	for _, t := range state.Tiles {
		b.WriteString("\n  - ")
		b.WriteString(t.Title)
		b.WriteString(": ")
		b.WriteString(t.Status)
		if t.Detail != "" {
			b.WriteString(" — ")
			b.WriteString(t.Detail)
		}
	}
	return errors.New(b.String())
}

func diffAndLog(prev, next map[string]string) {
	for title, status := range next {
		if prev[title] != status {
			fmt.Printf("  tile %-18s %s\n", title, status)
		}
	}
}

func formatLastStatus(m map[string]string) string {
	if len(m) == 0 {
		return "<empty>"
	}
	var b strings.Builder
	first := true
	for k, v := range m {
		if !first {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%s", k, v)
		first = false
	}
	return b.String()
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
