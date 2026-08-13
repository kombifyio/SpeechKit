/**
 * SpeechKit server `/assistant` page — the hosted/self-hosted web surface of
 * the standard Voice Assistant UI module. Everything renders through the
 * published framework packages: the `speechkit-voice-assistant` element plus
 * `createVoiceAgentUiController` (mic → ticket WebSocket → agent audio back).
 * The server's `[server.assistant_ui]` block supplies the default appearance;
 * a per-browser localStorage override lets each user pick their own.
 */

import "@kombifyio/speechkit-voice-ui/define";
import {
  isSemanticVoiceMark,
  resolveMarkSrc,
  semanticMarkRatio,
  type SemanticVoiceMark,
  type SpeechKitVoiceAssistantElement,
  type VoiceAssistantVariant
} from "@kombifyio/speechkit-voice-ui";
import {
  createVoiceAgentUiController,
  type VoiceAgentUiController
} from "@kombifyio/speechkit-voice-ui/voiceagent-adapter";

const MARK_ASSETS = {
  rosette: "/assistant/marks/rosette.png",
  k: "/assistant/marks/k.png"
} as const;

const APPEARANCE_STORAGE_KEY = "speechkit.assistant.appearance";
const TOKEN_STORAGE_KEY = "speechkit.assistant.token";

interface Appearance {
  variant: VoiceAssistantVariant;
  mark: SemanticVoiceMark;
  transcript: boolean;
}

const FALLBACK_APPEARANCE: Appearance = { variant: "aura", mark: "rosette", transcript: true };

function isVariant(value: unknown): value is VoiceAssistantVariant {
  return value === "aura" || value === "waveform";
}

function parseAppearance(value: unknown, base: Appearance): Appearance {
  if (typeof value !== "object" || value === null) return base;
  const record = value as Record<string, unknown>;
  return {
    variant: isVariant(record["variant"]) ? record["variant"] : base.variant,
    mark:
      typeof record["mark"] === "string" && isSemanticVoiceMark(record["mark"])
        ? (record["mark"] as SemanticVoiceMark)
        : base.mark,
    transcript:
      typeof record["transcript"] === "boolean" ? record["transcript"] : base.transcript
  };
}

function byId<T extends HTMLElement>(id: string): T {
  const el = document.getElementById(id);
  if (!el) throw new Error(`missing #${id}`);
  return el as T;
}

const assistant = byId<SpeechKitVoiceAssistantElement>("assistant");
const sessionToggle = byId<HTMLButtonElement>("session-toggle");
const appearanceToggle = byId<HTMLButtonElement>("appearance-toggle");
const appearancePanel = byId<HTMLElement>("appearance-panel");
const appearanceReset = byId<HTMLButtonElement>("appearance-reset");
const transcriptToggle = byId<HTMLInputElement>("transcript-toggle");
const connectionNote = byId<HTMLElement>("connection-note");
const tokenRow = byId<HTMLElement>("token-row");
const tokenInput = byId<HTMLInputElement>("token-input");

let serverDefault: Appearance = { ...FALLBACK_APPEARANCE };
let appearance: Appearance = { ...FALLBACK_APPEARANCE };
let controller: VoiceAgentUiController | null = null;
let sessionActive = false;

function storedOverride(): Partial<Appearance> | null {
  try {
    const raw = localStorage.getItem(APPEARANCE_STORAGE_KEY);
    return raw ? (JSON.parse(raw) as Partial<Appearance>) : null;
  } catch {
    return null;
  }
}

function applyAppearance(next: Appearance, persistOverride: boolean): void {
  appearance = next;
  assistant.setAttribute("variant", next.variant);
  const markSrc = resolveMarkSrc(next.mark, MARK_ASSETS);
  if (markSrc) assistant.setAttribute("mark-src", markSrc);
  else assistant.removeAttribute("mark-src");
  const ratio = semanticMarkRatio(next.mark);
  if (ratio) assistant.style.setProperty("--sk-assistant-mark-ratio", ratio);
  else assistant.style.removeProperty("--sk-assistant-mark-ratio");
  assistant.toggleAttribute("transcript", next.transcript);
  transcriptToggle.checked = next.transcript;
  for (const button of document.querySelectorAll<HTMLButtonElement>("#variant-chips button")) {
    button.dataset["active"] = String(button.dataset["variant"] === next.variant);
  }
  for (const button of document.querySelectorAll<HTMLButtonElement>("#mark-chips button")) {
    button.dataset["active"] = String(button.dataset["mark"] === next.mark);
  }
  if (persistOverride) {
    try {
      localStorage.setItem(APPEARANCE_STORAGE_KEY, JSON.stringify(next));
    } catch {
      // Storage unavailable (private mode) — the choice still applies live.
    }
  }
}

function smokeToken(): string {
  return document.body.dataset["smokeToken"]?.trim() ?? "";
}

function activeToken(): string {
  const smoke = smokeToken();
  if (smoke) return smoke;
  return tokenInput.value.trim();
}

function setNote(text: string): void {
  connectionNote.textContent = text;
}

function teardownController(): void {
  controller?.dispose();
  controller = null;
}

function ensureController(): VoiceAgentUiController {
  teardownController();
  const options: Parameters<typeof createVoiceAgentUiController>[0] = {
    serverUrl: window.location.origin,
    surface: "floating_panel",
    onDiagnostic: (message) => setNote(message)
  };
  const token = activeToken();
  if (token) options.token = token;
  const next = createVoiceAgentUiController(options);
  controller = next;
  assistant.controller = next;
  next.subscribe((state) => {
    sessionActive = state.status !== "idle" && state.status !== "cancelled" && state.status !== "denied";
    sessionToggle.textContent = sessionActive ? "Stop" : "Start";
  });
  return next;
}

sessionToggle.addEventListener("click", () => {
  if (sessionActive && controller) {
    void controller.stop();
    return;
  }
  void ensureController().start("voice_agent");
});

appearanceToggle.addEventListener("click", () => {
  const open = appearancePanel.hidden;
  appearancePanel.hidden = !open;
  appearanceToggle.setAttribute("aria-expanded", String(open));
});

for (const button of document.querySelectorAll<HTMLButtonElement>("#variant-chips button")) {
  button.addEventListener("click", () => {
    const variant = button.dataset["variant"];
    if (isVariant(variant)) applyAppearance({ ...appearance, variant }, true);
  });
}
for (const button of document.querySelectorAll<HTMLButtonElement>("#mark-chips button")) {
  button.addEventListener("click", () => {
    const mark = button.dataset["mark"];
    if (typeof mark === "string" && isSemanticVoiceMark(mark)) {
      applyAppearance({ ...appearance, mark }, true);
    }
  });
}
transcriptToggle.addEventListener("change", () => {
  applyAppearance({ ...appearance, transcript: transcriptToggle.checked }, true);
});
appearanceReset.addEventListener("click", () => {
  try {
    localStorage.removeItem(APPEARANCE_STORAGE_KEY);
  } catch {
    // Ignore storage failures; the reset still applies live.
  }
  applyAppearance({ ...serverDefault }, false);
});

tokenInput.addEventListener("change", () => {
  try {
    localStorage.setItem(TOKEN_STORAGE_KEY, tokenInput.value.trim());
  } catch {
    // Storage unavailable — the token still applies for this page load.
  }
});

async function bootstrap(): Promise<void> {
  try {
    tokenInput.value = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
  } catch {
    // Storage unavailable.
  }

  try {
    const response = await fetch("/v1/server/settings", { cache: "no-store" });
    if (!response.ok) throw new Error(`settings request failed: ${response.status}`);
    const payload = (await response.json()) as Record<string, unknown>;
    serverDefault = parseAppearance(
      (payload["assistant_ui"] as Record<string, unknown> | undefined) ?? {},
      {
        ...FALLBACK_APPEARANCE,
        transcript: parseTranscriptDefault(payload["assistant_ui"])
      }
    );
    const auth = payload["auth"] as Record<string, unknown> | undefined;
    const authMode = typeof auth?.["mode"] === "string" ? (auth["mode"] as string) : "";
    const needsToken = authMode !== "none" && smokeToken() === "";
    tokenRow.hidden = !needsToken;
    const version = typeof payload["version"] === "string" ? payload["version"] : "";
    setNote(`Connected to ${window.location.host}${version ? ` · ${version}` : ""}`);
    sessionToggle.disabled = false;
  } catch {
    setNote("Server settings unavailable — you can still try starting a session.");
    tokenRow.hidden = smokeToken() !== "";
    sessionToggle.disabled = false;
  }

  applyAppearance(parseAppearance(storedOverride(), serverDefault), false);
}

function parseTranscriptDefault(value: unknown): boolean {
  if (typeof value === "object" && value !== null) {
    const record = value as Record<string, unknown>;
    if (typeof record["transcript_default"] === "boolean") {
      return record["transcript_default"];
    }
  }
  return FALLBACK_APPEARANCE.transcript;
}

void bootstrap();
