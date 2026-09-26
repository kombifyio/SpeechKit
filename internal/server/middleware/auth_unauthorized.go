//go:build linux

package middleware

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

func browserUnauthorizedResponse(paths map[string]struct{}, routes []PublicRoute, r *http.Request) bool {
	if r == nil {
		return false
	}
	if _, ok := paths[r.URL.Path]; !ok && !routeAllowed(routes, r.URL.Path, r.Method) {
		return false
	}
	return requestAcceptsHTML(r)
}

func requestAcceptsHTML(r *http.Request) bool {
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	return accept == "" || strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")
}

func writeAuthError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="speechkit"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "unauthenticated",
			"message": "missing or invalid credentials",
		},
	})
}

// adminAuthErrorStyleCSS and adminAuthErrorScriptJS hold the exact inline
// <style>/<script> bodies of the admin sign-in page. They are kept as named
// constants so their sha256 hashes can be pinned in the page's
// Content-Security-Policy — letting the page keep its inline assets without
// 'unsafe-inline'. The hash is computed over these constants and the page is
// assembled from the same constants, so the two can never drift.
const adminAuthErrorStyleCSS = `
    :root {
      color-scheme: dark;
      --sk-bg: #0d131c;
      --sk-surface-0: #0b1118;
      --sk-surface-1: #111923;
      --sk-surface-2: #17212f;
      --sk-panel-border: rgba(224, 235, 255, .12);
      --sk-border: #334055;
      --sk-text: #e7eefb;
      --sk-text-muted: #97a4bc;
      --sk-accent: #a8bfff;
      --sk-accent-soft: rgba(168, 191, 255, .16);
      --fail: #ff9a9a;
      font-family: "Segoe UI Variable", "Segoe UI", "Aptos", sans-serif;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background:
        radial-gradient(circle at top left, rgba(130, 161, 255, .14), transparent 32%),
        linear-gradient(180deg, #0f1622 0%, #0a1017 100%);
      color: var(--sk-text);
      font-size: 14px;
      -webkit-font-smoothing: antialiased;
    }
    main {
      width: min(440px, calc(100vw - 32px));
      overflow: hidden;
      border: 1px solid var(--sk-panel-border);
      border-radius: 8px;
      background: rgba(17, 25, 35, .9);
      box-shadow: 0 28px 80px rgba(0, 0, 0, .28);
    }
    .topbar {
      display: flex;
      gap: 12px;
      align-items: center;
      border-bottom: 1px solid rgba(224, 235, 255, .08);
      padding: 16px;
      background: linear-gradient(180deg, rgba(19, 27, 38, .94), rgba(19, 27, 38, .68));
    }
    .mark {
      display: grid;
      width: 42px;
      height: 42px;
      flex: 0 0 auto;
      place-items: center;
      border: 1px solid var(--sk-panel-border);
      border-radius: 8px;
      background: var(--sk-surface-2);
      color: var(--sk-accent);
      font-weight: 800;
    }
    .kicker {
      margin: 0;
      color: var(--sk-text-muted);
      font-size: 10px;
      font-weight: 700;
      letter-spacing: .14em;
      text-transform: uppercase;
    }
    h1 {
      margin: 0;
      font-size: 21px;
      line-height: 1.1;
    }
    .content {
      display: grid;
      gap: 16px;
      padding: 16px;
    }
    p {
      line-height: 1.55;
      margin: 0;
      color: var(--sk-text-muted);
    }
    form {
      display: grid;
      gap: 12px;
    }
    label {
      display: grid;
      gap: 7px;
      color: var(--sk-text-muted);
      font-size: 13px;
    }
    input {
      min-height: 40px;
      border: 1px solid var(--sk-border);
      border-radius: 6px;
      background: var(--sk-surface-0);
      color: var(--sk-text);
      padding: 8px 10px;
      font: inherit;
      outline: none;
    }
    input:focus {
      border-color: rgba(168, 191, 255, .48);
      box-shadow: 0 0 0 3px var(--sk-accent-soft);
    }
    button {
      min-height: 42px;
      border: 1px solid rgba(168, 191, 255, .34);
      border-radius: 6px;
      background: var(--sk-accent-soft);
      color: var(--sk-accent);
      font: inherit;
      font-weight: 700;
      cursor: pointer;
    }
    button:hover {
      border-color: var(--sk-accent);
    }
    .error {
      color: var(--fail);
      min-height: 1.4rem;
    }
    .note {
      border: 1px solid var(--sk-panel-border);
      border-radius: 6px;
      padding: 10px;
      background: var(--sk-surface-2);
    }
    code { font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace; }
  `

const adminAuthErrorScriptJS = `
    (function () {
      const form = document.getElementById("adminLogin");
      const error = document.getElementById("loginError");
      function adminSessionPath() {
        return window.location.pathname.indexOf("/api/") === 0 ? "/api/v1/admin/session" : "/v1/admin/session";
      }
      async function tryLogin(username, password) {
        const response = await fetch(adminSessionPath(), {
          method: "POST",
          headers: { "Authorization": "Basic " + btoa(username + ":" + password), "Accept": "application/json" },
          credentials: "same-origin"
        });
        if (!response.ok) throw new Error("Invalid admin username or password.");
        window.location.reload();
      }
      form.addEventListener("submit", function (event) {
        event.preventDefault();
        error.textContent = "";
        const username = document.getElementById("adminUsername").value.trim();
        const password = document.getElementById("adminPassword").value;
        tryLogin(username, password).catch(function (err) {
          error.textContent = err && err.message ? err.message : String(err);
        });
      });
    })();
  `

// cspSourceHash returns the CSP source-expression (e.g. 'sha256-<base64>') for
// an inline element body, so it can be allow-listed without 'unsafe-inline'.
func cspSourceHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// adminAuthErrorCSP locks the admin sign-in page to its own hashed inline
// style+script; connect-src 'self' lets the form POST the same-origin admin
// session endpoint. Computed once at init from the constants above.
var adminAuthErrorCSP = "default-src 'none'; style-src " + cspSourceHash(adminAuthErrorStyleCSS) +
	"; script-src " + cspSourceHash(adminAuthErrorScriptJS) +
	"; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

var adminAuthErrorHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SpeechKit Admin Sign-In Required</title>
  <style>` + adminAuthErrorStyleCSS + `</style>
</head>
<body>
  <main>
    <header class="topbar">
      <div class="mark" aria-hidden="true">SK</div>
      <div>
        <p class="kicker">kombify SpeechKit</p>
        <h1>SpeechKit Admin Sign-In Required</h1>
      </div>
    </header>
    <div class="content" data-ui-surface="speechkit-server-admin-login">
      <p>The server setup UI is protected after bootstrap. Sign in with the admin account created during setup.</p>
      <form id="adminLogin">
        <label for="adminUsername">Admin username
          <input id="adminUsername" autocomplete="username" required>
        </label>
        <label for="adminPassword">Admin password
          <input id="adminPassword" type="password" autocomplete="current-password" required>
        </label>
        <button type="submit">Sign In</button>
        <div class="error" id="loginError" role="status"></div>
      </form>
      <p class="note">API clients can continue to call <code>/api/v1/*</code> or <code>/v1/*</code> with their configured bearer token.</p>
    </div>
  </main>
  <script>` + adminAuthErrorScriptJS + `</script>
</body>
</html>`

func writeBrowserAuthError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Override the strict default CSP from SecurityHeaders with a per-page
	// policy that permits only this page's hashed inline assets.
	w.Header().Set("Content-Security-Policy", adminAuthErrorCSP)
	w.WriteHeader(http.StatusUnauthorized)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(adminAuthErrorHTML))
}
