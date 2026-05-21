import { defaultSettingsState } from "@/lib/speechkit";

export const onboardingHotkeys = {
  dictate: defaultSettingsState.dictateHotkey,
  assist: defaultSettingsState.assistHotkey,
  voiceAgent: defaultSettingsState.voiceAgentHotkey,
} as const;

export const onboardingHotkeyLabels = {
  dictate: "Ctrl+Win",
  assist: "Win+Alt",
  voiceAgent: "Ctrl+Shift",
} as const;

export type WizardStep =
  | "welcome"
  | "local_model"
  | "integrations"
  | "wake_word"
  | "done";

export type OnboardingTarget = "local" | "cloud";

// Curated catalog of phrases the wake-word wizard step offers. IDs here
// MUST match internal/wakeword/catalog.go DefaultCatalog() entries so the
// backend's keyword-label resolution (DetectionEvent.Keyword → phrase
// display name) lines up with the user's UI selection. The bundled
// keywords.txt (see scripts/prepare-wakeword-model.ps1) ships
// BPE-tokenised entries for every ID listed below.
export const onboardingWakeWordPhrases: {
  id: string;
  displayName: string;
  notes: string;
}[] = [
  {
    id: "hey_quby",
    displayName: "Hey Quby",
    notes:
      "SpeechKit brand default. Recognises three pronunciations (Quby / Cubi / Kubi) so it works in DE and EN.",
  },
  {
    id: "hey_computer",
    displayName: "Hey Computer",
    notes:
      "Star Trek classic — four syllables with very distinct phonemes. Best false-accept-rate of the curated set.",
  },
  {
    id: "hey_jarvis",
    displayName: "Hey Jarvis",
    notes:
      "Marvel-popular. Strong J/RV/S consonants make it easy to recognise even in noisy rooms.",
  },
  {
    id: "hey_mira",
    displayName: "Hey Mira",
    notes:
      "Short brand-style alternative. Three syllables, distinct vowels, equally natural in DE+EN.",
  },
  {
    id: "hey_kombify",
    displayName: "Hey Kombify",
    notes:
      "Kombify org brand. Four syllables with three distinct consonant onsets (K/B/F). English pronunciation only.",
  },
];

export type IntegrationMode = "dictate" | "assist" | "voice_agent";

export type OnboardingIntegration = {
  provider: string;
  label: string;
  category: "Cloud Router / Gateway" | "Direct Provider" | "Local Provider";
  modes: IntegrationMode[];
  credentialLabel?: string;
  setupUrl: string;
  setupLabel: string;
  headline: string;
  summary: string;
};

const integrationModeOrder: IntegrationMode[] = [
  "dictate",
  "assist",
  "voice_agent",
];

const integrationModeLabels: Record<IntegrationMode, string> = {
  dictate: "Dictation",
  assist: "Assist",
  voice_agent: "Voice Agent",
};

export const onboardingIntegrations: OnboardingIntegration[] = [
  {
    provider: "huggingface",
    label: "Hugging Face",
    category: "Cloud Router / Gateway",
    modes: ["dictate", "assist", "voice_agent"],
    credentialLabel: "Hugging Face token",
    setupUrl: "https://huggingface.co/settings/tokens",
    setupLabel: "Create Hugging Face token",
    headline: "One key for every mode.",
    summary: "All modes with one gateway key.",
  },
  {
    provider: "openrouter",
    label: "OpenRouter",
    category: "Cloud Router / Gateway",
    modes: ["dictate", "assist", "voice_agent"],
    credentialLabel: "OpenRouter API key",
    setupUrl: "https://openrouter.ai/settings/keys",
    setupLabel: "Create OpenRouter API key",
    headline: "One router for every mode.",
    summary: "Dictation, Assist, and Voice Agent through OpenRouter.",
  },
  {
    provider: "openai",
    label: "OpenAI",
    category: "Direct Provider",
    modes: ["dictate", "assist"],
    credentialLabel: "OpenAI API key",
    setupUrl: "https://platform.openai.com/api-keys",
    setupLabel: "Create OpenAI API key",
    headline: "Use your OpenAI key.",
    summary: "Use OpenAI for Dictation and Assist.",
  },
  {
    provider: "google",
    label: "Gemini / Google AI",
    category: "Direct Provider",
    modes: ["dictate", "assist", "voice_agent"],
    credentialLabel: "Gemini / Google AI API key",
    setupUrl: "https://aistudio.google.com/apikey",
    setupLabel: "Create Gemini API key",
    headline: "Gemini for Voice Agent.",
    summary: "Native Voice Agent plus Google profiles.",
  },
  {
    provider: "groq",
    label: "Groq",
    category: "Direct Provider",
    modes: ["dictate", "assist"],
    credentialLabel: "Groq API key",
    setupUrl: "https://console.groq.com/keys",
    setupLabel: "Create Groq API key",
    headline: "Groq for fast turns.",
    summary: "Fast Direct Provider for Dictation and Assist.",
  },
  {
    provider: "ollama",
    label: "Ollama",
    category: "Local Provider",
    modes: ["dictate", "assist", "voice_agent"],
    setupUrl: "https://ollama.com/download",
    setupLabel: "Download Ollama",
    headline: "Run models locally.",
    summary: "Run optional local models yourself.",
  },
];

export const onboardingIntegrationGroups = [
  {
    id: "gateways",
    headline: "Want one key for everything? Start with a gateway.",
    summary:
      "Hugging Face and OpenRouter unlock more model choice without managing every vendor.",
    providers: ["huggingface", "openrouter"],
    gridClass: "md:grid-cols-2",
  },
  {
    id: "direct",
    headline: "Already use a provider? Plug in your key.",
    summary:
      "OpenAI, Gemini, and Groq are best when your team already has an API account.",
    providers: ["openai", "google", "groq"],
    gridClass: "md:grid-cols-3",
  },
  {
    id: "local",
    headline: "Want to test models on your own machine? Use Ollama.",
    summary: "Keep experimenting locally while SpeechKit stays local-first by default.",
    providers: ["ollama"],
    gridClass: "md:grid-cols-2",
  },
];

export const integrationLogoSrc: Record<string, string> = {
  huggingface: "/integrations/huggingface.svg",
  openrouter: "/integrations/openrouter.svg",
  openai: "/integrations/openai.svg",
  google: "/integrations/gemini.svg",
  groq: "/integrations/groq.svg",
  ollama: "/integrations/ollama.svg",
};

export const logoFrameClass: Record<string, string> = {
  huggingface: "bg-[#ffd84d]",
  openrouter: "bg-[#0f1117]",
  openai: "bg-white",
  google: "bg-white",
  groq: "bg-white",
  ollama: "bg-white",
};

export function orderedIntegrationModeLabels(modes: IntegrationMode[]) {
  const enabledModes = new Set(modes);
  return integrationModeOrder
    .filter((mode) => enabledModes.has(mode))
    .map((mode) => integrationModeLabels[mode]);
}

export function setupDisplayUrl(setupUrl: string) {
  try {
    const parsed = new URL(setupUrl);
    return `${parsed.host}${parsed.pathname}`.replace(/\/$/, "");
  } catch {
    return setupUrl;
  }
}
