//go:build linux

package core

import (
	"io"
	"net/http"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func serverPublicPaths() []string {
	return []string{"/", "/healthz", "/readyz", "/setup", "/setup/"}
}

func serverPublicRoutes() []middleware.PublicRoute {
	return []middleware.PublicRoute{
		{Path: "/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
		{Path: "/api/v1/server/settings", Methods: []string{http.MethodGet, http.MethodHead}},
	}
}

func serverBootstrapAuthRoutes() []middleware.PublicRoute {
	return []middleware.PublicRoute{
		{Path: "/v1/server/settings", Methods: []string{http.MethodPatch}},
		{Path: "/api/v1/server/settings", Methods: []string{http.MethodPatch}},
	}
}

func registerTestUI(app *App) {
	if app == nil || app.Mux == nil {
		return
	}
	smokeHandler := testUIHandler{}
	setupHandler := setupUIHandler{}
	app.Mux.Handle("/", smokeHandler)
	app.Mux.Handle("/setup", setupHandler)
	app.Mux.Handle("/setup/", setupHandler)
}

type testUIHandler struct{}

func (testUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeHTML(w, r, testUIHTML)
}

type setupUIHandler struct{}

func (setupUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/setup" && r.URL.Path != "/setup/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeHTML(w, r, setupOnlyUIHTML)
}

func writeHTML(w http.ResponseWriter, r *http.Request, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, html)
}

const testUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SpeechKit Server Smoke</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #0f1115;
      --surface: #171a20;
      --line: #343a45;
      --text: #f3f5f8;
      --muted: #a7b0bd;
      --ok: #73d28d;
      --warn: #e6bd68;
      --fail: #ff7878;
      --run: #8bbcff;
      font-family: "Segoe UI", "Aptos", sans-serif;
    }

    * { box-sizing: border-box; }

    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
    }

    button, textarea {
      font: inherit;
    }

    .shell {
      width: min(960px, calc(100vw - 32px));
      margin: 0 auto;
      padding: 28px 0 40px;
    }

    header {
      display: flex;
      justify-content: space-between;
      align-items: flex-end;
      gap: 18px;
      border-bottom: 1px solid var(--line);
      padding-bottom: 18px;
    }

    h1 {
      margin: 0;
      font-size: 32px;
      line-height: 1.1;
      letter-spacing: 0;
    }

    h2 {
      margin: 0;
      font-size: 18px;
      line-height: 1.2;
    }

    .subtitle {
      margin: 8px 0 0;
      color: var(--muted);
      max-width: 680px;
      line-height: 1.45;
    }

    .overall {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 9px 12px;
      color: var(--muted);
      background: #12151a;
      white-space: nowrap;
    }

    .overall.ok { color: var(--ok); border-color: rgba(115, 210, 141, .55); }
    .overall.warn { color: var(--warn); border-color: rgba(230, 189, 104, .55); }
    .overall.fail { color: var(--fail); border-color: rgba(255, 120, 120, .55); }
    .overall.run { color: var(--run); border-color: rgba(139, 188, 255, .55); }

    .summary {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 10px;
      margin-top: 18px;
    }

    .tile, .runtime-panel, .control, .check {
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--surface);
    }

    .tile {
      min-height: 116px;
      padding: 13px 14px;
      display: grid;
      align-content: start;
      gap: 4px;
      border-left-width: 3px;
    }

    .tile span, .setting span {
      color: var(--muted);
      font-size: 11px;
      text-transform: uppercase;
    }

    .tile strong, .setting strong {
      display: block;
      margin-top: 3px;
      color: var(--text);
      overflow-wrap: anywhere;
    }

    .tile small, .setting small {
      display: block;
      margin-top: 6px;
      color: var(--muted);
      line-height: 1.35;
      overflow-wrap: anywhere;
    }

    .tile.ok { border-left-color: var(--ok); }
    .tile.warn { border-left-color: var(--warn); }
    .tile.fail { border-left-color: var(--fail); }
    .tile.run { border-left-color: var(--run); }

    .runtime-panel {
      margin-top: 12px;
      padding: 14px;
    }

    .runtime-heading {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
    }

    .settings-grid {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px;
      margin-top: 14px;
    }

    .setting {
      min-height: 86px;
      padding: 11px 12px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #12151a;
    }

    .control {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 12px;
      align-items: end;
      margin-top: 18px;
      padding: 16px;
    }

    label {
      display: grid;
      gap: 7px;
      color: var(--muted);
      font-size: 13px;
    }

    textarea {
      min-height: 76px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #0b0d11;
      color: var(--text);
      padding: 10px 11px;
      outline: none;
      line-height: 1.4;
      resize: vertical;
    }

    textarea:focus {
      border-color: var(--run);
      box-shadow: 0 0 0 3px rgba(139, 188, 255, .13);
    }

    button {
      min-height: 42px;
      border: 1px solid rgba(115, 210, 141, .62);
      border-radius: 6px;
      background: #203126;
      color: var(--text);
      padding: 0 16px;
      cursor: pointer;
    }

    button:hover {
      border-color: var(--ok);
    }

    button:disabled {
      cursor: not-allowed;
      opacity: .58;
    }

    button.secondary {
      min-height: 34px;
      border-color: var(--line);
      background: #151a22;
      color: var(--muted);
    }

    .checks {
      display: grid;
      gap: 10px;
      margin-top: 14px;
    }

    .check {
      display: grid;
      grid-template-columns: 160px 120px minmax(0, 1fr);
      gap: 12px;
      align-items: start;
      padding: 13px 14px;
    }

    .name {
      font-weight: 650;
    }

    .status {
      display: inline-flex;
      justify-content: center;
      min-width: 86px;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 5px 8px;
      color: var(--muted);
      background: #11141a;
      font-size: 12px;
      text-transform: uppercase;
    }

    .status.ok { color: var(--ok); border-color: rgba(115, 210, 141, .55); }
    .status.warn { color: var(--warn); border-color: rgba(230, 189, 104, .55); }
    .status.fail { color: var(--fail); border-color: rgba(255, 120, 120, .55); }
    .status.run { color: var(--run); border-color: rgba(139, 188, 255, .55); }

    pre {
      margin: 0;
      min-height: 40px;
      max-height: 210px;
      overflow: auto;
      white-space: pre-wrap;
      word-break: break-word;
      color: #d9e0ea;
      font: 12px/1.45 "Cascadia Mono", "Consolas", monospace;
    }

    @media (max-width: 760px) {
      header, .control, .check, .summary, .settings-grid {
        grid-template-columns: 1fr;
      }

      header {
        display: grid;
        align-items: start;
      }

      button {
        width: 100%;
      }
    }
  </style>
</head>
<body>
  <main class="shell">
    <header>
      <div>
        <h1>SpeechKit Server Smoke</h1>
        <p class="subtitle">Checks the running server instance against the current configuration through /healthz, /readyz, and /api/v1.</p>
      </div>
      <div class="overall" id="overallStatus">Ready</div>
    </header>

    <section class="summary" aria-label="Runtime Status">
      <div class="tile" id="tileStatus">
        <span>Server</span>
        <strong id="runtimeStatus">waiting</strong>
        <small id="runtimeVersion">Version unknown</small>
      </div>
      <div class="tile" id="tileSTT">
        <span>STT</span>
        <strong id="sttProvider">waiting</strong>
        <small id="sttDetail">-</small>
      </div>
      <div class="tile" id="tileLLM">
        <span>LLM</span>
        <strong id="llmProvider">waiting</strong>
        <small id="llmDetail">-</small>
      </div>
      <div class="tile" id="tileVoice">
        <span>Voice Agent</span>
        <strong id="voiceProvider">waiting</strong>
        <small id="voiceDetail">-</small>
      </div>
    </section>

    <section class="runtime-panel">
      <div class="runtime-heading">
        <h2>Current Configuration</h2>
        <button id="refreshSettings" class="secondary" type="button">Refresh</button>
      </div>
      <div class="settings-grid">
        <div class="setting">
          <span>Runtime</span>
          <strong id="runtimeMode">waiting</strong>
          <small id="runtimeDetail">-</small>
        </div>
        <div class="setting">
          <span>Models</span>
          <strong id="modelSummary">waiting</strong>
          <small id="modelDetail">-</small>
        </div>
        <div class="setting">
          <span>Data</span>
          <strong id="dataSummary">waiting</strong>
          <small id="dataDetail">-</small>
        </div>
      </div>
    </section>

    <section class="control">
      <button id="runSmoke" type="button">Start Smoke Test</button>
    </section>

    <section class="checks" aria-live="polite">
      <article class="check">
        <div class="name">Server Settings</div>
        <div class="status" id="settingsStatus">waiting</div>
        <pre id="settingsOutput"></pre>
      </article>
      <article class="check">
        <div class="name">Health</div>
        <div class="status" id="healthStatus">waiting</div>
        <pre id="healthOutput"></pre>
      </article>
      <article class="check">
        <div class="name">Readiness</div>
        <div class="status" id="readyStatus">waiting</div>
        <pre id="readyOutput"></pre>
      </article>
      <article class="check">
        <div class="name">Dictation</div>
        <div class="status" id="dictationStatus">waiting</div>
        <pre id="dictationOutput"></pre>
      </article>
      <article class="check">
        <div class="name">Assist</div>
        <div class="status" id="assistStatus">waiting</div>
        <pre id="assistOutput"></pre>
      </article>
      <article class="check">
        <div class="name">Voice Agent</div>
        <div class="status" id="voiceagentStatus">waiting</div>
        <pre id="voiceagentOutput"></pre>
      </article>
    </section>
  </main>

  <script>
    const checks = ["settings", "health", "ready", "dictation", "assist", "voiceagent"];

    function byId(id) {
      return document.getElementById(id);
    }

    function setOverall(text, state) {
      const el = byId("overallStatus");
      el.textContent = text;
      el.className = "overall " + (state || "");
    }

    function setCheck(name, state, detail) {
      const status = byId(name + "Status");
      status.textContent = state;
      status.className = "status " + stateClass(state);
      byId(name + "Output").textContent = detail || "";
    }

    function stateClass(state) {
      if (state === "ok") return "ok";
      if (state === "degraded") return "warn";
      if (state === "starting") return "run";
      if (state === "unavailable") return "fail";
      if (state === "running") return "run";
      if (state === "fail") return "fail";
      return "";
    }

    function setTile(tileId, state, value, detail) {
      const tile = byId(tileId);
      tile.className = "tile " + stateClass(state);
      const strong = tile.querySelector("strong");
      const small = tile.querySelector("small");
      strong.textContent = value || "-";
      small.textContent = detail || "-";
    }

    function component(settings, name) {
      return settings && settings.components ? settings.components[name] : null;
    }

    function componentText(settings, name) {
      const item = component(settings, name);
      if (!item) return "-";
      return (item.status || "unknown") + (item.detail ? ": " + item.detail : "");
    }

    function providerList(values) {
      return Array.isArray(values) && values.length ? values.join(", ") : "none";
    }

    function updateRuntimePanel(settings) {
      settings = settings || {};
      const stt = settings.stt || {};
      const sttSelf = stt.self_hosted || {};
      const llm = settings.llm || {};
      const localLLM = llm.local || {};
      const voice = settings.voice_agent || {};
      const cascaded = voice.cascaded || {};
      const runtime = settings.runtime || {};
      const personas = settings.personas || {};
      const tts = settings.tts || {};
      const llmComponent = component(settings, "llm.local");
      const sttComponent = component(settings, "stt.vps");
      const voiceComponent = component(settings, "mode.voiceagent");
      const storeComponent = component(settings, "store");

      byId("runtimeStatus").textContent = settings.status || "unknown";
      byId("runtimeVersion").textContent = settings.version || "Version unknown";
      byId("tileStatus").className = "tile " + stateClass(settings.status || "");

      setTile("tileSTT", sttComponent ? sttComponent.status : "", providerList(stt.providers), componentText(settings, "stt.vps"));
      setTile("tileLLM", llmComponent ? llmComponent.status : "", localLLM.enabled ? "local llama.cpp" : "none", componentText(settings, "llm.local"));
      setTile("tileVoice", voiceComponent ? voiceComponent.status : "", voice.provider || "gemini", componentText(settings, "mode.voiceagent"));

      byId("runtimeMode").textContent = runtime.self_hosted_defaults ? "Self-hosted defaults" : "Custom";
      byId("runtimeDetail").textContent = "Modes: " + providerList(settings.modes) + " | Model dir: " + (runtime.model_dir || "-");
      byId("modelSummary").textContent = "STT " + (sttSelf.model || "-") + " / LLM " + (localLLM.assist || localLLM.voice_agent || "-");
      byId("modelDetail").textContent = "STT endpoint: " + (sttSelf.url || "-") + " | LLM endpoint: " + (localLLM.base_url || "-");
      byId("dataSummary").textContent = "Store " + (storeComponent ? storeComponent.status : "unknown") + " / TTS " + (tts.enabled ? "enabled" : "optional");
      byId("dataDetail").textContent = "Personas: " + (personas.seeded || 0) + ", roles: " + (personas.roles || 0) + " | Cascaded STT/LLM/TTS: "
        + Boolean(cascaded.stt_ready) + "/" + Boolean(cascaded.agent_ready) + "/" + Boolean(cascaded.tts_ready);
    }

    function format(value) {
      if (typeof value === "string") return value;
      return JSON.stringify(value, null, 2);
    }

    async function request(path, opts) {
      opts = opts || {};
      const response = await fetch(path, opts);
      const text = await response.text();
      let body = text;
      if (text) {
        try {
          body = JSON.parse(text);
        } catch (_) {
          body = text;
        }
      }
      return {
        ok: response.ok,
        status: response.status,
        statusText: response.statusText,
        body: body,
        text: text
      };
    }

    async function runCheck(name, fn) {
      setCheck(name, "running", "");
      try {
        const result = await fn();
        setCheck(name, result.state, result.detail);
        return result.state;
      } catch (err) {
        setCheck(name, "fail", err && err.message ? err.message : String(err));
        return "fail";
      }
    }

    function requireStatus(result, expected, label) {
      if (result.status !== expected) {
        throw new Error(label + " returned HTTP " + result.status + "\n" + format(result.body));
      }
    }

    function hasErrorEnvelope(result) {
      return result.body && result.body.error && result.body.error.code;
    }

    async function checkHealth() {
      const result = await request("/healthz", { method: "GET" });
      requireStatus(result, 200, "/healthz");
      if (!result.body || result.body.status !== "ok") {
        throw new Error("/healthz did not report ok\n" + format(result.body));
      }
      return { state: "ok", detail: format(result.body) };
    }

    async function checkSettings() {
      const result = await request("/v1/server/settings", { method: "GET" });
      requireStatus(result, 200, "/v1/server/settings");
      updateRuntimePanel(result.body);
      return { state: result.body && result.body.status ? result.body.status : "ok", detail: format(result.body) };
    }

    async function checkReady() {
      const result = await request("/readyz", { method: "GET" });
      if (result.status === 200) {
        return { state: "ok", detail: format(result.body) };
      }
      if (result.status === 503) {
        return { state: "degraded", detail: format(result.body) };
      }
      throw new Error("/readyz returned HTTP " + result.status + "\n" + format(result.body));
    }

    async function checkDictation() {
      const form = new FormData();
      form.append("audio", new Blob([synthWAV()], { type: "audio/wav" }), "speechkit-smoke.wav");
      form.append("language", "en");
      const result = await request("/api/v1/dictation/transcribe", {
        method: "POST",
        body: form
      });
      if (result.status === 200) {
        if (!result.body || result.body.duration_ms <= 0) {
          throw new Error("Dictation response is missing duration_ms\n" + format(result.body));
        }
        return { state: "ok", detail: format(result.body) };
      }
      if (result.status === 503 && hasErrorEnvelope(result)) {
        return { state: "degraded", detail: format(result.body) };
      }
      throw new Error("Dictation returned HTTP " + result.status + "\n" + format(result.body));
    }

    async function checkAssist() {
      const checks = [
        {
          name: "direct",
          payload: { text: "what time is it", locale: "en", tts: false },
          allowDegraded: true
        },
        {
          name: "copy_last",
          payload: { text: "copy last", locale: "en", tts: false },
          wantAction: "execute"
        },
        {
          name: "insert_last",
          payload: { text: "insert last", locale: "en", tts: false },
          wantAction: "execute"
        },
        {
          name: "summarize",
          payload: {
            text: "summarize this",
            locale: "en",
            selection: "Deploy smoke source text. It exercises the Assist summarize codeword after release.",
            tts: false
          },
          wantAction: "execute"
        }
      ];
      const results = [];
      for (const check of checks) {
        const result = await request("/api/v1/assist/process", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(check.payload)
        });
        if (result.status === 200) {
          if (!result.body || !result.body.action) {
            throw new Error("Assist " + check.name + " response is missing action\n" + format(result.body));
          }
          if (check.wantAction && result.body.action !== check.wantAction) {
            throw new Error("Assist " + check.name + " action is " + result.body.action + ", want " + check.wantAction + "\n" + format(result.body));
          }
          results.push({ name: check.name, state: "ok", body: result.body });
          continue;
        }
        if (result.status === 503 && check.allowDegraded && hasErrorEnvelope(result)) {
          results.push({ name: check.name, state: "degraded", body: result.body });
          continue;
        }
        throw new Error("Assist " + check.name + " returned HTTP " + result.status + "\n" + format(result.body));
      }
      return { state: "ok", detail: format(results) };
    }

    async function checkVoiceAgent() {
      const session = await request("/api/v1/voiceagent/sessions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}"
      });
      requireStatus(session, 201, "Voice Agent session create");
      if (!session.body || !session.body.session_id || !session.body.ws_url || !session.body.ticket) {
        throw new Error("Voice Agent session response is incomplete\n" + format(session.body));
      }

      let wsResult;
      try {
        wsResult = await verifyVoiceAgentSocket(session.body.ws_url);
      } finally {
        await request("/api/v1/voiceagent/sessions/" + encodeURIComponent(session.body.session_id), {
          method: "DELETE"
        }).catch(function () {});
      }
      return {
        state: "ok",
        detail: format({
          session_id: session.body.session_id,
          expires_at: session.body.expires_at,
          websocket: wsResult
        })
      };
    }

    function verifyVoiceAgentSocket(wsURL) {
      return new Promise(function (resolve, reject) {
        let done = false;
        const frames = [];
        const ws = new WebSocket(wsURL);
        const timer = window.setTimeout(function () {
          fail("Voice Agent WebSocket timed out before pong/listening.");
        }, 12000);

        function finish(payload) {
          if (done) return;
          done = true;
          window.clearTimeout(timer);
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "stop" }));
            ws.close(1000, "speechkit smoke complete");
          }
          resolve(payload);
        }

        function fail(message) {
          if (done) return;
          done = true;
          window.clearTimeout(timer);
          if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
            ws.close(1000, "speechkit smoke failed");
          }
          reject(new Error(message + "\n" + format({ frames: frames })));
        }

        ws.onopen = function () {
          const start = { type: "start" };
          frames.push({ sent: start });
          ws.send(JSON.stringify(start));
        };

        ws.onmessage = function (event) {
          if (typeof event.data !== "string") {
            frames.push({ received: "binary", bytes: event.data.byteLength || 0 });
            return;
          }
          let message;
          try {
            message = JSON.parse(event.data);
          } catch (_) {
            message = { raw: event.data };
          }
          frames.push({ received: message });
          if (message.type === "error") {
            fail("Voice Agent returned " + message.code + ": " + message.message);
            return;
          }
          if (message.type === "state" && message.state === "listening") {
            const ping = { type: "ping" };
            frames.push({ sent: ping });
            ws.send(JSON.stringify(ping));
            return;
          }
          if (message.type === "pong") {
            finish({ state: "connected", frames: frames });
          }
        };

        ws.onerror = function () {
          fail("Voice Agent WebSocket connection failed.");
        };

        ws.onclose = function (event) {
          if (!done) {
            fail("Voice Agent WebSocket closed early: " + event.code + " " + event.reason);
          }
        };
      });
    }

    function synthWAV() {
      const rate = 16000;
      const ms = 250;
      const frames = Math.floor(rate * ms / 1000);
      const dataSize = frames * 2;
      const buffer = new ArrayBuffer(44 + dataSize);
      const view = new DataView(buffer);

      writeString(view, 0, "RIFF");
      view.setUint32(4, 36 + dataSize, true);
      writeString(view, 8, "WAVE");
      writeString(view, 12, "fmt ");
      view.setUint32(16, 16, true);
      view.setUint16(20, 1, true);
      view.setUint16(22, 1, true);
      view.setUint32(24, rate, true);
      view.setUint32(28, rate * 2, true);
      view.setUint16(32, 2, true);
      view.setUint16(34, 16, true);
      writeString(view, 36, "data");
      view.setUint32(40, dataSize, true);

      for (let i = 0; i < frames; i++) {
        const t = i / rate;
        const sample = Math.sin(2 * Math.PI * 440 * t) * 10000;
        view.setInt16(44 + i * 2, sample, true);
      }
      return buffer;
    }

    function writeString(view, offset, value) {
      for (let i = 0; i < value.length; i++) {
        view.setUint8(offset + i, value.charCodeAt(i));
      }
    }

    async function runSmoke() {
      byId("runSmoke").disabled = true;
      setOverall("Running", "run");
      checks.forEach(function (name) {
        setCheck(name, "waiting", "");
      });

      const states = [];
      states.push(await runCheck("settings", checkSettings));
      states.push(await runCheck("health", checkHealth));
      states.push(await runCheck("ready", checkReady));
      states.push(await runCheck("dictation", checkDictation));
      states.push(await runCheck("assist", checkAssist));
      states.push(await runCheck("voiceagent", checkVoiceAgent));

      if (states.includes("fail")) {
        setOverall("Failed", "fail");
      } else if (states.includes("degraded")) {
        setOverall("Degraded", "warn");
      } else {
        setOverall("Passing", "ok");
      }
      byId("runSmoke").disabled = false;
    }

    byId("runSmoke").addEventListener("click", function () {
      runSmoke().catch(function (err) {
        setOverall("Failed", "fail");
        byId("runSmoke").disabled = false;
        setCheck("voiceagent", "fail", err && err.message ? err.message : String(err));
      });
    });

    byId("refreshSettings").addEventListener("click", function () {
      runCheck("settings", checkSettings).catch(function (err) {
        setCheck("settings", "fail", err && err.message ? err.message : String(err));
      });
    });

    runCheck("settings", checkSettings).catch(function () {});
  </script>
</body>
</html>`

const setupOnlyUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>SpeechKit Server Setup</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #0f1115;
      --surface: #171a20;
      --line: #343a45;
      --text: #f3f5f8;
      --muted: #a7b0bd;
      --ok: #73d28d;
      --warn: #e6bd68;
      --fail: #ff7878;
      --run: #8bbcff;
      font-family: "Segoe UI", "Aptos", sans-serif;
    }

    * { box-sizing: border-box; }

    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
    }

    button, input, select {
      font: inherit;
    }

    .shell {
      width: min(960px, calc(100vw - 32px));
      margin: 0 auto;
      padding: 28px 0 40px;
    }

    header {
      display: flex;
      justify-content: space-between;
      align-items: flex-end;
      gap: 18px;
      border-bottom: 1px solid var(--line);
      padding-bottom: 18px;
    }

    h1 {
      margin: 0;
      font-size: 32px;
      line-height: 1.1;
      letter-spacing: 0;
    }

    .subtitle {
      margin: 8px 0 0;
      color: var(--muted);
      max-width: 680px;
      line-height: 1.45;
    }

    .state {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 9px 12px;
      color: var(--muted);
      background: #12151a;
      white-space: nowrap;
    }

    .state.ok { color: var(--ok); border-color: rgba(115, 210, 141, .55); }
    .state.warn { color: var(--warn); border-color: rgba(230, 189, 104, .55); }
    .state.fail { color: var(--fail); border-color: rgba(255, 120, 120, .55); }
    .state.run { color: var(--run); border-color: rgba(139, 188, 255, .55); }

    .panel {
      margin-top: 18px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--surface);
      padding: 16px;
    }

    .stepper {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 8px;
    }

    .step {
      min-height: 42px;
      border-color: var(--line);
      background: #12151a;
      color: var(--muted);
      text-align: left;
    }

    .step.active {
      color: var(--text);
      border-color: rgba(139, 188, 255, .7);
      background: #172133;
    }

    .step.done {
      color: var(--ok);
      border-color: rgba(115, 210, 141, .5);
    }

    .step-panel {
      display: grid;
      gap: 14px;
    }

    .step-panel.hidden {
      display: none;
    }

    .intro-copy, .review-copy {
      color: var(--muted);
      line-height: 1.45;
      margin: 0;
    }

    .callout {
      padding: 12px;
      border: 1px solid rgba(139, 188, 255, .35);
      border-radius: 6px;
      background: #111827;
      color: var(--text);
      overflow-wrap: anywhere;
    }

    .setup-grid {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px;
      margin-top: 14px;
    }

    .metric {
      min-height: 76px;
      padding: 11px 12px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #12151a;
    }

    .metric span {
      color: var(--muted);
      font-size: 11px;
      text-transform: uppercase;
    }

    .metric strong {
      display: block;
      margin-top: 3px;
      overflow-wrap: anywhere;
    }

    .metric small {
      display: block;
      margin-top: 6px;
      color: var(--muted);
      line-height: 1.35;
      overflow-wrap: anywhere;
    }

    .model-form {
      display: grid;
      gap: 16px;
      margin-top: 16px;
      padding-top: 16px;
      border-top: 1px solid var(--line);
    }

    .provider-matrix {
      grid-column: 1 / -1;
      display: grid;
      gap: 8px;
    }

    .provider-row {
      display: grid;
      grid-template-columns: 120px minmax(150px, .75fr) minmax(190px, 1fr) minmax(150px, .75fr);
      gap: 10px;
      align-items: end;
      padding: 10px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #12151a;
    }

    .mode-name {
      padding-bottom: 10px;
      font-weight: 650;
    }

    .mode-options {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 10px;
      margin-top: 8px;
    }

    .mode-option-card {
      display: grid;
      gap: 10px;
      padding: 10px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #12151a;
    }

    .mode-option-card h3 {
      margin: 0;
      font-size: 14px;
      line-height: 1.2;
    }

    .tool-grid {
      display: grid;
      gap: 6px;
    }

    .credential-grid {
      grid-column: 1 / -1;
      display: grid;
      grid-template-columns: repeat(5, minmax(0, 1fr));
      gap: 10px;
    }

    .credential-card {
      display: grid;
      gap: 8px;
      padding: 10px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #12151a;
    }

    .server-auth-card {
      grid-column: 1 / -1;
    }

    .auth-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      flex-wrap: wrap;
    }

    .token-output {
      display: grid;
      gap: 8px;
      border: 1px solid rgba(115, 210, 141, .35);
      border-radius: 6px;
      padding: 10px;
      background: #0f1b13;
    }

    .token-output.hidden {
      display: none;
    }

    .token-output code {
      display: block;
      overflow-wrap: anywhere;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #0b0d11;
      padding: 8px 10px;
      color: var(--ok);
      line-height: 1.45;
    }

    .token-output small {
      color: var(--muted);
      line-height: 1.35;
    }

    .badge {
      display: inline-flex;
      align-items: center;
      min-height: 24px;
      width: fit-content;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 2px 8px;
      color: var(--muted);
      font-size: 12px;
    }

    .badge.ok {
      color: var(--ok);
      border-color: rgba(115, 210, 141, .55);
    }

    .badge.warn {
      color: var(--warn);
      border-color: rgba(230, 189, 104, .55);
    }

    label {
      display: grid;
      gap: 7px;
      color: var(--muted);
      font-size: 13px;
    }

    input, select, textarea {
      min-height: 38px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #0b0d11;
      color: var(--text);
      padding: 8px 10px;
      outline: none;
      line-height: 1.4;
    }

    textarea {
      min-height: 110px;
      resize: vertical;
    }

    input:focus, select:focus, textarea:focus {
      border-color: var(--run);
      box-shadow: 0 0 0 3px rgba(139, 188, 255, .13);
    }

    .inline {
      min-height: 38px;
      display: flex;
      gap: 8px;
      align-items: center;
      color: var(--text);
    }

    .inline input {
      width: 18px;
      min-height: 18px;
    }

    .form-actions {
      grid-column: 1 / -1;
      display: flex;
      gap: 10px;
      align-items: center;
      justify-content: flex-end;
    }

    .save-status {
      min-height: 20px;
      color: var(--muted);
      font-size: 13px;
      overflow-wrap: anywhere;
    }

    button {
      min-height: 42px;
      border: 1px solid rgba(115, 210, 141, .62);
      border-radius: 6px;
      background: #203126;
      color: var(--text);
      padding: 0 16px;
      cursor: pointer;
    }

    button:hover {
      border-color: var(--ok);
    }

    button:disabled {
      cursor: not-allowed;
      opacity: .58;
    }

    button.secondary {
      min-height: 34px;
      border-color: var(--line);
      background: #151a22;
      color: var(--muted);
    }

    @media (max-width: 760px) {
      header, .stepper, .setup-grid, .provider-row, .mode-options, .credential-grid, .model-form {
        grid-template-columns: 1fr;
      }

      header {
        display: grid;
        align-items: start;
      }

      button {
        width: 100%;
      }
    }
  </style>
</head>
<body>
  <main class="shell">
    <header>
      <div>
        <h1 id="setupHeading">SpeechKit Server Setup</h1>
        <p class="subtitle" id="setupSubtitle">Guided onboarding for provider, model, and credential settings on this server instance.</p>
      </div>
      <div class="state" id="setupStatus">Loading</div>
    </header>

    <section class="panel" id="setupWizard" data-setup-mode="onboarding">
      <div class="stepper" aria-label="Setup steps">
        <button class="step active" data-step-target="welcome" type="button">1. Welcome</button>
        <button class="step" data-step-target="models" type="button">2. Models</button>
        <button class="step" data-step-target="credentials" type="button">3. Keys</button>
        <button class="step" data-step-target="review" type="button">4. Review</button>
      </div>

      <form class="model-form" id="onboardingPanel">
        <section class="step-panel" data-step-panel="welcome">
          <p class="intro-copy">SpeechKit can run fully self-hosted out of the box. The default stack uses Whisper Large v3 Turbo for dictation and a local llama.cpp model for Assist and the Voice Agent pipeline.</p>
          <div class="callout">Default local LLM: <strong>ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M</strong></div>
          <div class="setup-grid">
            <div class="metric">
              <span>Runtime</span>
              <strong id="runtimeMode">waiting</strong>
              <small id="runtimeDetail">-</small>
            </div>
            <div class="metric">
              <span>Models</span>
              <strong id="modelSummary">waiting</strong>
              <small id="modelDetail">-</small>
            </div>
            <div class="metric">
              <span>Data</span>
              <strong id="dataSummary">waiting</strong>
              <small id="dataDetail">-</small>
            </div>
          </div>
        </section>

        <section class="step-panel hidden" id="settingsPanel" data-step-panel="models">
        <p class="settings-copy">Choose provider types and concrete models for Dictation, Assist, and Voice Agent.</p>
        <div class="provider-matrix" id="providerMatrix">
          <span class="badge" id="onboardingState">Onboarding pending</span>
          <div class="provider-row">
            <div class="mode-name">Dictation</div>
            <label for="dictationKind">Provider type
              <select id="dictationKind" data-mode="dictation">
                <option value="local_built_in">Local Built-in</option>
                <option value="local_provider">Local Provider</option>
                <option value="cloud_provider">Cloud Provider</option>
                <option value="direct_provider">Direct Provider</option>
              </select>
            </label>
            <label for="dictationProfile">Provider
              <select id="dictationProfile"></select>
            </label>
            <label for="dictationModel">Model
              <input id="dictationModel" autocomplete="off" value="whisper-1">
            </label>
          </div>
          <div class="provider-row">
            <div class="mode-name">Assist</div>
            <label for="assistKind">Provider type
              <select id="assistKind" data-mode="assist">
                <option value="local_built_in">Local Built-in</option>
                <option value="local_provider">Local Provider</option>
                <option value="cloud_provider">Cloud Provider</option>
                <option value="direct_provider">Direct Provider</option>
              </select>
            </label>
            <label for="assistProfile">Provider
              <select id="assistProfile"></select>
            </label>
            <label for="assistModel">Model
              <input id="assistModel" autocomplete="off" value="ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M">
            </label>
          </div>
          <div class="provider-row">
            <div class="mode-name">Voice Agent</div>
            <label for="voiceAgentKind">Provider type
              <select id="voiceAgentKind" data-mode="voice_agent">
                <option value="local_built_in">Local Built-in</option>
                <option value="local_provider">Local Provider</option>
                <option value="cloud_provider">Cloud Provider</option>
                <option value="direct_provider">Direct Provider</option>
              </select>
            </label>
            <label for="voiceAgentProfile">Provider
              <select id="voiceAgentProfile"></select>
            </label>
            <label for="voiceAgentModel">Model
              <input id="voiceAgentModel" autocomplete="off" value="ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M">
            </label>
          </div>
          <div class="mode-options" id="modeOptions">
            <div class="mode-option-card">
              <h3>Dictation Dictionary</h3>
              <label for="dictationDictionary">Custom words
                <textarea id="dictationDictionary" autocomplete="off" placeholder="Kombify&#10;kombi fire => Kombify"></textarea>
              </label>
            </div>
            <div class="mode-option-card">
              <h3>Assist Tools</h3>
              <div class="tool-grid" id="assistToolList"></div>
            </div>
            <div class="mode-option-card">
              <h3>Voice Agent Prompt Template</h3>
              <label for="voiceAgentAgentProfile">Agent profile
                <select id="voiceAgentAgentProfile"></select>
              </label>
              <label for="voiceAgentPromptTemplate">System prompt
                <textarea id="voiceAgentPromptTemplate" autocomplete="off" placeholder="You are a concise voice assistant."></textarea>
              </label>
            </div>
          </div>
        </div>
        </section>

        <section class="step-panel hidden" data-step-panel="credentials">
        <div class="credential-card server-auth-card">
          <div class="auth-row">
            <label class="inline"><input id="serverTokenManaged" type="checkbox" checked> Generate server API token</label>
            <span class="badge warn" id="serverTokenState">Token pending</span>
          </div>
          <p class="intro-copy">Keep this on to generate a bearer token for Windows clients and API callers. Turn it off when authentication is handled outside SpeechKit or the API should stay available with the current server auth mode.</p>
          <label for="serverTokenEnv">Bearer token env
            <input id="serverTokenEnv" autocomplete="off" value="SPEECHKIT_SERVER_TOKEN">
          </label>
          <div class="token-output hidden" id="serverTokenOutput">
            <strong>Generated server API token</strong>
            <code id="generatedServerToken">-</code>
            <button id="copyGeneratedServerToken" class="secondary" type="button">Copy Token</button>
            <small id="generatedServerTokenHint">Store this token in the Windows client environment and send it as Authorization: Bearer.</small>
          </div>
        </div>
        <div class="credential-grid">
          <div class="credential-card">
            <label class="inline"><input id="openAIEnabled" type="checkbox"> OpenAI</label>
            <label for="openAIEnv">Env
              <input id="openAIEnv" autocomplete="off" value="OPENAI_API_KEY">
            </label>
            <label for="openAIKey">API Key
              <input id="openAIKey" autocomplete="off" type="password" placeholder="leave empty to keep the current value">
            </label>
          </div>
          <div class="credential-card">
            <label class="inline"><input id="groqEnabled" type="checkbox"> Groq</label>
            <label for="groqEnv">Env
              <input id="groqEnv" autocomplete="off" value="GROQ_API_KEY">
            </label>
            <label for="groqKey">API Key
              <input id="groqKey" autocomplete="off" type="password" placeholder="leave empty to keep the current value">
            </label>
          </div>
          <div class="credential-card">
            <label class="inline"><input id="googleEnabled" type="checkbox"> Google</label>
            <label for="googleEnv">Env
              <input id="googleEnv" autocomplete="off" value="GOOGLE_AI_API_KEY">
            </label>
            <label for="googleKey">API Key
              <input id="googleKey" autocomplete="off" type="password" placeholder="leave empty to keep the current value">
            </label>
          </div>
          <div class="credential-card">
            <label class="inline"><input id="hfEnabled" type="checkbox"> Hugging Face</label>
            <label for="hfEnv">Env
              <input id="hfEnv" autocomplete="off" value="HF_TOKEN">
            </label>
            <label for="hfKey">API Key
              <input id="hfKey" autocomplete="off" type="password" placeholder="leave empty to keep the current value">
            </label>
          </div>
          <div class="credential-card">
            <label class="inline"><input id="openRouterEnabled" type="checkbox"> OpenRouter</label>
            <label for="openRouterEnv">Env
              <input id="openRouterEnv" autocomplete="off" value="OPENROUTER_API_KEY">
            </label>
            <label for="openRouterKey">API Key
              <input id="openRouterKey" autocomplete="off" type="password" placeholder="leave empty to keep the current value">
            </label>
          </div>
        </div>
        </section>

        <section class="step-panel hidden" data-step-panel="review">
          <p class="review-copy">Review the active choices. Saving marks onboarding complete for this deploy version and writes the desired model settings.</p>
          <div class="setup-grid">
            <div class="metric">
              <span>Dictation</span>
              <strong id="reviewDictation">-</strong>
              <small id="reviewDictationModel">-</small>
            </div>
            <div class="metric">
              <span>Assist</span>
              <strong id="reviewAssist">-</strong>
              <small id="reviewAssistModel">-</small>
            </div>
            <div class="metric">
              <span>Voice Agent</span>
              <strong id="reviewVoice">-</strong>
              <small id="reviewVoiceModel">-</small>
            </div>
            <div class="metric">
              <span>API Auth</span>
              <strong id="reviewServerAuth">-</strong>
              <small id="reviewServerAuthDetail">-</small>
            </div>
          </div>
        </section>

        <div class="form-actions">
          <span class="save-status" id="settingsSaveStatus"></span>
          <button id="runDefaultInstall" class="secondary" type="button">Use Local Defaults</button>
          <button id="setupBack" class="secondary" type="button">Back</button>
          <button id="setupNext" type="button">Continue</button>
          <button id="saveModelSettings" type="button">Save Settings</button>
        </div>
      </form>
    </section>
  </main>

  <script>
    const defaultLocalLLMModel = "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M";
    const providerKinds = ["local_built_in", "local_provider", "cloud_provider", "direct_provider"];
    const setupSteps = ["welcome", "models", "credentials", "review"];
    let currentStep = 0;
    let lastSettings = null;
    let setupMode = "onboarding";
    let generatedServerToken = "";
    let forceServerTokenGeneration = false;

    function byId(id) {
      return document.getElementById(id);
    }

    function setSetupStatus(text, state) {
      const el = byId("setupStatus");
      el.textContent = text;
      el.className = "state " + (state || "");
    }

    function component(settings, name) {
      return settings && settings.components ? settings.components[name] : null;
    }

    function providerList(values) {
      return Array.isArray(values) && values.length ? values.join(", ") : "none";
    }

    function selectedText(id) {
      const select = byId(id);
      return select && select.selectedOptions.length ? select.selectedOptions[0].textContent : "-";
    }

    function voiceAgentProfiles() {
      const voice = lastSettings && lastSettings.voice_agent ? lastSettings.voice_agent : {};
      const profiles = Array.isArray(voice.agent_profiles) ? voice.agent_profiles : [];
      return profiles.length ? profiles : [
        { id: "default", display_name: "Default Voice Agent" },
        { id: "brainstorming_companion", display_name: "Brainstorming Companion", voice: "Aoede" },
        { id: "humor_companion", display_name: "Humor Companion", voice: "Puck" },
        { id: "support_companion", display_name: "Support Companion", voice: "Charon" }
      ];
    }

    function populateVoiceAgentProfiles(selectedID) {
      const select = byId("voiceAgentAgentProfile");
      if (!select) return;
      const profiles = voiceAgentProfiles();
      const current = selectedID || select.value || "default";
      select.innerHTML = "";
      profiles.forEach(function (profile) {
        const option = document.createElement("option");
        option.value = profile.id;
        option.textContent = profile.display_name || profile.displayName || profile.id;
        if (profile.voice) {
          option.textContent += " (" + profile.voice + ")";
        }
        select.appendChild(option);
      });
      select.value = profiles.some(function (profile) { return profile.id === current; }) ? current : "default";
    }

    function assistTools() {
      const catalog = lastSettings && lastSettings.catalog ? lastSettings.catalog : {};
      return Array.isArray(catalog.assist_tools) ? catalog.assist_tools : [];
    }

    function defaultAssistToolIDs() {
      return assistTools().filter(function (tool) {
        return tool.default_enabled;
      }).map(function (tool) {
        return tool.id;
      });
    }

    function renderAssistTools(selectedIDs) {
      const selected = new Set(Array.isArray(selectedIDs) ? selectedIDs : defaultAssistToolIDs());
      const list = byId("assistToolList");
      list.innerHTML = "";
      assistTools().forEach(function (tool) {
        const label = document.createElement("label");
        label.className = "inline";
        const input = document.createElement("input");
        input.type = "checkbox";
        input.value = tool.id;
        input.checked = selected.has(tool.id);
        input.dataset.assistTool = "true";
        label.appendChild(input);
        label.appendChild(document.createTextNode(tool.label || tool.id));
        list.appendChild(label);
      });
    }

    function selectedAssistToolIDs() {
      return Array.from(document.querySelectorAll("[data-assist-tool]")).filter(function (input) {
        return input.checked;
      }).map(function (input) {
        return input.value;
      });
    }

    function serverBearerTokenSet() {
      const auth = lastSettings && lastSettings.auth ? lastSettings.auth : {};
      return Boolean(auth.bearer_token_set);
    }

    function updateServerAuthUI() {
      const managed = byId("serverTokenManaged").checked;
      const tokenSet = serverBearerTokenSet();
      const envName = byId("serverTokenEnv").value.trim() || "SPEECHKIT_SERVER_TOKEN";
      const state = byId("serverTokenState");
      if (!managed) {
        state.textContent = "Self-managed";
        state.className = "badge";
      } else if (forceServerTokenGeneration || !tokenSet) {
        state.textContent = "Token will be generated";
        state.className = "badge warn";
      } else {
        state.textContent = "Token configured";
        state.className = "badge ok";
      }
      byId("reviewServerAuth").textContent = managed ? "Managed bearer token" : "Self-managed";
      byId("reviewServerAuthDetail").textContent = managed
        ? "Env: " + envName + (forceServerTokenGeneration || !tokenSet ? " / new token on save" : " / existing token")
        : "Setup will not generate a server API token.";
    }

    function applyServerAuthToForm(settings) {
      settings = settings || {};
      const editable = settings.editable || {};
      const desired = editable.desired || {};
      const desiredAuth = desired.server_auth || {};
      const runtime = settings.runtime || {};
      const runtimeAuth = settings.auth || {};
      const mode = runtime.settings_persisted && desiredAuth.mode ? desiredAuth.mode : "managed_bearer";
      byId("serverTokenManaged").checked = mode !== "self_managed";
      byId("serverTokenEnv").value = desiredAuth.bearer_token_env || runtimeAuth.bearer_token_env || "SPEECHKIT_SERVER_TOKEN";
      forceServerTokenGeneration = byId("serverTokenManaged").checked && !Boolean(runtimeAuth.bearer_token_set);
      updateServerAuthUI();
    }

    function serverAuthPayload() {
      const managed = byId("serverTokenManaged").checked;
      const payload = {
        mode: managed ? "managed_bearer" : "self_managed",
        bearer_token_env: byId("serverTokenEnv").value.trim() || "SPEECHKIT_SERVER_TOKEN"
      };
      if (managed && (forceServerTokenGeneration || !serverBearerTokenSet())) {
        payload.generate_token = true;
      }
      return payload;
    }

    function renderGeneratedServerToken(generated) {
      generated = generated || {};
      if (!generated.token) {
        if (!generatedServerToken) {
          byId("serverTokenOutput").classList.add("hidden");
        }
        return;
      }
      generatedServerToken = generated.token;
      forceServerTokenGeneration = false;
      byId("generatedServerToken").textContent = generated.token;
      byId("generatedServerTokenHint").textContent = "Store this in " + (generated.env || "SPEECHKIT_SERVER_TOKEN") + " on the Windows client. API calls must send Authorization: Bearer <token>.";
      byId("serverTokenOutput").classList.remove("hidden");
      updateServerAuthUI();
    }

    function renderSettingsPanels() {
      document.querySelectorAll("[data-step-panel]").forEach(function (panel) {
        panel.classList.toggle("hidden", panel.dataset.stepPanel !== "models" && panel.dataset.stepPanel !== "credentials");
      });
      document.querySelectorAll("[data-step-target]").forEach(function (step) {
        step.classList.remove("active", "done");
      });
      byId("setupBack").style.display = "none";
      byId("setupNext").style.display = "none";
      byId("saveModelSettings").style.display = "";
      updateReview();
    }

    function renderSetupMode(settings) {
      const onboarding = settings && settings.onboarding ? settings.onboarding : {};
      const requiresOnboarding = onboarding.enabled !== false && Boolean(onboarding.required);
      setupMode = requiresOnboarding ? "onboarding" : "settings";
      byId("setupWizard").dataset.setupMode = setupMode;
      byId("setupHeading").textContent = requiresOnboarding ? "SpeechKit Server Setup" : "SpeechKit Server Settings";
      byId("setupSubtitle").textContent = requiresOnboarding
        ? "Guided onboarding for provider, model, and credential settings on this server instance."
        : "Manage provider, model, and credential settings for this server instance.";
      document.querySelector(".stepper").style.display = requiresOnboarding ? "" : "none";
      byId("onboardingState").textContent = requiresOnboarding ? "Onboarding required" : "Settings mode";
      byId("onboardingState").className = "badge " + (requiresOnboarding ? "warn" : "ok");
      if (requiresOnboarding) {
        setStep(currentStep);
      } else {
        renderSettingsPanels();
      }
    }

    function setStep(index) {
      if (setupMode === "settings") {
        renderSettingsPanels();
        return;
      }
      currentStep = Math.max(0, Math.min(setupSteps.length - 1, index));
      const active = setupSteps[currentStep];
      document.querySelectorAll("[data-step-panel]").forEach(function (panel) {
        panel.classList.toggle("hidden", panel.dataset.stepPanel !== active);
      });
      document.querySelectorAll("[data-step-target]").forEach(function (step, idx) {
        step.classList.toggle("active", step.dataset.stepTarget === active);
        step.classList.toggle("done", idx < currentStep);
      });
      byId("setupBack").disabled = currentStep === 0;
      byId("setupBack").style.display = "";
      byId("setupNext").style.display = currentStep === setupSteps.length - 1 ? "none" : "";
      byId("saveModelSettings").style.display = currentStep === setupSteps.length - 1 ? "" : "none";
      updateReview();
    }

    function updateReview() {
      const pairs = [
        ["reviewDictation", "reviewDictationModel", "dictationProfile", "dictationModel"],
        ["reviewAssist", "reviewAssistModel", "assistProfile", "assistModel"],
        ["reviewVoice", "reviewVoiceModel", "voiceAgentProfile", "voiceAgentModel"]
      ];
      pairs.forEach(function (pair) {
        byId(pair[0]).textContent = selectedText(pair[2]);
        byId(pair[1]).textContent = byId(pair[3]).value || "-";
      });
      const agentProfile = selectedText("voiceAgentAgentProfile");
      if (agentProfile !== "-") {
        byId("reviewVoiceModel").textContent = (byId("reviewVoiceModel").textContent || "-") + " / Agent " + agentProfile;
      }
      updateServerAuthUI();
    }

    function profilesFor(mode, kind) {
      const catalog = lastSettings && lastSettings.catalog && lastSettings.catalog.modes ? lastSettings.catalog.modes : {};
      return (catalog[mode] || []).filter(function (profile) {
        return !kind || profile.providerKind === kind;
      });
    }

    function profileById(mode, id) {
      const catalog = lastSettings && lastSettings.catalog && lastSettings.catalog.modes ? lastSettings.catalog.modes : {};
      return (catalog[mode] || []).find(function (profile) {
        return profile.id === id;
      });
    }

    function preferredProfile(mode, kind) {
      const profiles = profilesFor(mode, kind);
      return profiles.find(function (profile) { return profile.default; })
        || profiles.find(function (profile) { return profile.recommended; })
        || profiles[0]
        || null;
    }

    function recommendedVariant(profile) {
      if (!profile || !Array.isArray(profile.variants)) return null;
      return profile.variants.find(function (variant) { return variant.recommended; }) || profile.variants[0] || null;
    }

    function populateProfiles(mode, kindId, profileId) {
      const kind = byId(kindId).value;
      const select = byId(profileId);
      const current = select.value;
      select.innerHTML = "";
      profilesFor(mode, kind).forEach(function (profile) {
        const option = document.createElement("option");
        option.value = profile.id;
        option.textContent = profile.name;
        select.appendChild(option);
      });
      const fallback = preferredProfile(mode, kind);
      select.value = current && profileById(mode, current) ? current : (fallback ? fallback.id : "");
      syncModelForProfile(mode, profileId);
    }

    function syncModelForProfile(mode, profileId) {
      const profile = profileById(mode, byId(profileId).value);
      const variant = recommendedVariant(profile);
      const modelInput = mode === "dictation" ? byId("dictationModel") : mode === "assist" ? byId("assistModel") : byId("voiceAgentModel");
      if (!modelInput.value.trim()) {
        if (profile && profile.providerKind === "local_built_in" && mode !== "dictation") {
          modelInput.value = defaultLocalLLMModel;
        } else {
          modelInput.value = variant ? variant.modelId : (profile ? profile.modelId : "");
        }
      }
      updateReview();
    }

    function applyModeOptionsToForm(settings) {
      settings = settings || {};
      const editable = settings.editable || {};
      const desired = editable.desired || {};
      const dictation = desired.dictation || {};
      const assist = desired.assist || {};
      const voice = desired.voice_agent || {};
      byId("dictationDictionary").value = typeof dictation.dictionary === "string" ? dictation.dictionary : "";
      renderAssistTools(Array.isArray(assist.enabled_tools) ? assist.enabled_tools : defaultAssistToolIDs());
      populateVoiceAgentProfiles(voice.agent_profile_id || (settings.voice_agent || {}).agent_profile_id || "default");
      byId("voiceAgentPromptTemplate").value = typeof voice.prompt_template === "string" ? voice.prompt_template : "";
    }

    function applySettingsToForm(settings, options) {
      options = options || {};
      lastSettings = settings || {};
      populateProfiles("dictation", "dictationKind", "dictationProfile");
      populateProfiles("assist", "assistKind", "assistProfile");
      populateProfiles("voice_agent", "voiceAgentKind", "voiceAgentProfile");

      const editable = lastSettings.editable || {};
      const desired = editable.desired || {};
      const modes = desired.modes || {};
      applyModeForm("dictation", modes.dictation, "dictationKind", "dictationProfile", "dictationModel");
      applyModeForm("assist", modes.assist, "assistKind", "assistProfile", "assistModel");
      applyModeForm("voice_agent", modes.voice_agent, "voiceAgentKind", "voiceAgentProfile", "voiceAgentModel");

      const credentials = desired.credentials || {};
      applyCredentialForm(credentials.openai, "openAIEnabled", "openAIEnv");
      applyCredentialForm(credentials.groq, "groqEnabled", "groqEnv");
      applyCredentialForm(credentials.google, "googleEnabled", "googleEnv");
      applyCredentialForm(credentials.huggingface, "hfEnabled", "hfEnv");
      applyCredentialForm(credentials.openrouter, "openRouterEnabled", "openRouterEnv");
      applyServerAuthToForm(lastSettings);
      applyModeOptionsToForm(lastSettings);

      const runtime = lastSettings.runtime || {};
      const onboarding = lastSettings.onboarding || {};
      byId("saveModelSettings").disabled = !runtime.settings_write;
      const requiresOnboarding = onboarding.enabled !== false && Boolean(onboarding.required);
      byId("onboardingState").textContent = requiresOnboarding ? "Onboarding required" : "Settings mode";
      byId("onboardingState").className = "badge " + (requiresOnboarding ? "warn" : "ok");
      if (!options.preserveStatus || !runtime.settings_write) {
        byId("settingsSaveStatus").textContent = runtime.settings_write ? "" : "Saving is disabled.";
      }
      updateRuntimePanel(lastSettings);
      renderSetupMode(lastSettings);
      updateReview();
    }

    function applyModeForm(mode, setting, kindId, profileId, modelId) {
      setting = setting || {};
      const kind = setting.provider_kind || (preferredProfile(mode, "local_built_in") ? "local_built_in" : byId(kindId).value);
      byId(kindId).value = providerKinds.includes(kind) ? kind : "local_built_in";
      populateProfiles(mode, kindId, profileId);
      if (setting.profile_id && profileById(mode, setting.profile_id)) {
        byId(profileId).value = setting.profile_id;
      }
      byId(modelId).value = setting.model || byId(modelId).value || "";
    }

    function applyCredentialForm(setting, enabledId, envId) {
      setting = setting || {};
      if (typeof setting.enabled === "boolean") {
        byId(enabledId).checked = setting.enabled;
      }
      if (setting.env) {
        byId(envId).value = setting.env;
      }
    }

    function setDefaultLocalSettings() {
      byId("dictationKind").value = "local_built_in";
      byId("assistKind").value = "local_built_in";
      byId("voiceAgentKind").value = "local_built_in";
      populateProfiles("dictation", "dictationKind", "dictationProfile");
      populateProfiles("assist", "assistKind", "assistProfile");
      populateProfiles("voice_agent", "voiceAgentKind", "voiceAgentProfile");
      byId("dictationProfile").value = "stt.local.whispercpp";
      byId("assistProfile").value = "assist.builtin.gemma4-e4b";
      byId("voiceAgentProfile").value = "realtime.builtin.pipeline";
      byId("dictationModel").value = "whisper-1";
      byId("assistModel").value = defaultLocalLLMModel;
      byId("voiceAgentModel").value = defaultLocalLLMModel;
      renderAssistTools(defaultAssistToolIDs());
      populateVoiceAgentProfiles("default");
      byId("settingsSaveStatus").textContent = "Local defaults selected.";
      updateReview();
    }

    function credentialPayload(enabledId, envId, valueId) {
      const payload = {
        enabled: byId(enabledId).checked,
        env: byId(envId).value.trim()
      };
      const value = byId(valueId).value.trim();
      if (value) {
        payload.value = value;
      }
      return payload;
    }

    function buildSettingsPayload() {
      return {
        onboarding_complete: true,
        onboarding_version: lastSettings && lastSettings.version ? lastSettings.version : "",
        server_auth: serverAuthPayload(),
        modes: {
          dictation: {
            provider_kind: byId("dictationKind").value,
            profile_id: byId("dictationProfile").value,
            model: byId("dictationModel").value.trim()
          },
          assist: {
            provider_kind: byId("assistKind").value,
            profile_id: byId("assistProfile").value,
            model: byId("assistModel").value.trim()
          },
          voice_agent: {
            provider_kind: byId("voiceAgentKind").value,
            profile_id: byId("voiceAgentProfile").value,
            model: byId("voiceAgentModel").value.trim()
          }
        },
        dictation: {
          dictionary: byId("dictationDictionary").value.trim()
        },
        assist: {
          enabled_tools: selectedAssistToolIDs()
        },
        voice_agent: {
          agent_profile_id: byId("voiceAgentAgentProfile").value || "default",
          prompt_template: byId("voiceAgentPromptTemplate").value.trim()
        },
        credentials: {
          openai: credentialPayload("openAIEnabled", "openAIEnv", "openAIKey"),
          groq: credentialPayload("groqEnabled", "groqEnv", "groqKey"),
          google: credentialPayload("googleEnabled", "googleEnv", "googleKey"),
          huggingface: credentialPayload("hfEnabled", "hfEnv", "hfKey"),
          openrouter: credentialPayload("openRouterEnabled", "openRouterEnv", "openRouterKey")
        }
      };
    }

    async function saveServerSettings(event) {
      if (event && event.preventDefault) {
        event.preventDefault();
      }
      byId("saveModelSettings").disabled = true;
      byId("settingsSaveStatus").textContent = "Saving...";
      try {
        const result = await request("/v1/server/settings", {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(buildSettingsPayload())
        });
        if (!result.ok) {
          throw new Error(format(result.body));
        }
        ["openAIKey", "groqKey", "googleKey", "hfKey", "openRouterKey"].forEach(function (id) {
          byId(id).value = "";
        });
        const generated = result.body && result.body.generated_token ? result.body.generated_token : null;
        const message = result.body && result.body.message ? result.body.message : "Saved.";
        await loadServerSettings({ preserveStatus: true });
        renderGeneratedServerToken(generated);
        byId("settingsSaveStatus").textContent = message;
        setSetupStatus("Saved", "ok");
      } catch (err) {
        byId("settingsSaveStatus").textContent = err && err.message ? err.message : String(err);
      } finally {
        const runtime = lastSettings && lastSettings.runtime ? lastSettings.runtime : {};
        byId("saveModelSettings").disabled = !runtime.settings_write;
      }
    }

    function updateRuntimePanel(settings) {
      settings = settings || {};
      const stt = settings.stt || {};
      const sttSelf = stt.self_hosted || {};
      const llm = settings.llm || {};
      const localLLM = llm.local || {};
      const runtime = settings.runtime || {};
      const personas = settings.personas || {};
      const tts = settings.tts || {};
      const storeComponent = component(settings, "store");

      byId("runtimeMode").textContent = runtime.self_hosted_defaults ? "Self-hosted defaults" : "Custom";
      byId("runtimeDetail").textContent = "Modes: " + providerList(settings.modes) + " | Model dir: " + (runtime.model_dir || "-");
      byId("modelSummary").textContent = "STT " + (sttSelf.model || "-") + " / LLM " + (localLLM.assist || localLLM.voice_agent || "-");
      byId("modelDetail").textContent = "STT endpoint: " + (sttSelf.url || "-") + " | LLM endpoint: " + (localLLM.base_url || "-");
      byId("dataSummary").textContent = "Store " + (storeComponent ? storeComponent.status : "unknown") + " / TTS " + (tts.enabled ? "enabled" : "optional");
      byId("dataDetail").textContent = "Personas: " + (personas.seeded || 0) + ", roles: " + (personas.roles || 0);
    }

    function format(value) {
      if (typeof value === "string") return value;
      return JSON.stringify(value, null, 2);
    }

    async function request(path, opts) {
      opts = opts || {};
      if (generatedServerToken && opts.method && opts.method !== "GET" && path.indexOf("/server/settings") !== -1) {
        const headers = new Headers(opts.headers || {});
        if (!headers.has("Authorization")) {
          headers.set("Authorization", "Bearer " + generatedServerToken);
        }
        opts = Object.assign({}, opts, { headers: headers });
      }
      const response = await fetch(path, opts);
      const text = await response.text();
      let body = text;
      if (text) {
        try {
          body = JSON.parse(text);
        } catch (_) {
          body = text;
        }
      }
      return {
        ok: response.ok,
        status: response.status,
        statusText: response.statusText,
        body: body,
        text: text
      };
    }

    async function loadServerSettings(options) {
      options = options || {};
      setSetupStatus("Loading", "run");
      try {
        const result = await request("/v1/server/settings", { method: "GET" });
        if (!result.ok) {
          throw new Error(format(result.body));
        }
        applySettingsToForm(result.body, options);
        setSetupStatus(result.body && result.body.status ? result.body.status : "ok", result.body && result.body.status === "ok" ? "ok" : "warn");
      } catch (err) {
        setSetupStatus("Failed", "fail");
        byId("settingsSaveStatus").textContent = err && err.message ? err.message : String(err);
      }
    }

    byId("onboardingPanel").addEventListener("submit", saveServerSettings);
    byId("saveModelSettings").addEventListener("click", saveServerSettings);
    byId("runDefaultInstall").addEventListener("click", setDefaultLocalSettings);
    byId("setupBack").addEventListener("click", function () {
      setStep(currentStep - 1);
    });
    byId("setupNext").addEventListener("click", function () {
      setStep(currentStep + 1);
    });
    document.querySelectorAll("[data-step-target]").forEach(function (step, index) {
      step.addEventListener("click", function () {
        setStep(index);
      });
    });
    byId("dictationKind").addEventListener("change", function () {
      byId("dictationModel").value = "";
      populateProfiles("dictation", "dictationKind", "dictationProfile");
      updateReview();
    });
    byId("assistKind").addEventListener("change", function () {
      byId("assistModel").value = "";
      populateProfiles("assist", "assistKind", "assistProfile");
      updateReview();
    });
    byId("voiceAgentKind").addEventListener("change", function () {
      byId("voiceAgentModel").value = "";
      populateProfiles("voice_agent", "voiceAgentKind", "voiceAgentProfile");
      updateReview();
    });
    byId("dictationProfile").addEventListener("change", function () {
      byId("dictationModel").value = "";
      syncModelForProfile("dictation", "dictationProfile");
      updateReview();
    });
    byId("assistProfile").addEventListener("change", function () {
      byId("assistModel").value = "";
      syncModelForProfile("assist", "assistProfile");
      updateReview();
    });
    byId("voiceAgentProfile").addEventListener("change", function () {
      byId("voiceAgentModel").value = "";
      syncModelForProfile("voice_agent", "voiceAgentProfile");
      updateReview();
    });
    byId("voiceAgentAgentProfile").addEventListener("change", updateReview);
    byId("serverTokenManaged").addEventListener("change", function () {
      forceServerTokenGeneration = byId("serverTokenManaged").checked && !serverBearerTokenSet();
      updateServerAuthUI();
    });
    byId("serverTokenEnv").addEventListener("input", updateServerAuthUI);
    byId("copyGeneratedServerToken").addEventListener("click", async function () {
      if (!generatedServerToken) return;
      try {
        await navigator.clipboard.writeText(generatedServerToken);
        byId("settingsSaveStatus").textContent = "Token copied.";
      } catch (_) {
        byId("settingsSaveStatus").textContent = "Copy failed; select the token manually.";
      }
    });

    ["dictationModel", "assistModel", "voiceAgentModel"].forEach(function (id) {
      byId(id).addEventListener("input", updateReview);
    });
    setStep(0);
    loadServerSettings();
  </script>
</body>
</html>`
