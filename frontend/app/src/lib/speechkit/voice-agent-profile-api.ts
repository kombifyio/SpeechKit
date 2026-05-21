// Voice-agent profile is derived from the onboarding target chosen on the
// welcome screen. There is no UI step that asks the user — Local maps to
// on-prem (whisper.cpp + local LLM + local TTS), Cloud and Server-Connect
// both map to cloud-byok (Gemini Live via the user's own Google project).
//
// The apply-profile POST is called from the welcome-step submit handlers
// once the install-mode has been persisted. Failure is logged but does not
// block onboarding — the user can still finish the wizard and re-configure
// from Settings later.

export type VoiceAgentProfile = "on-prem" | "cloud-byok";

// installMode is the string we already POST to /app/install-mode in the
// welcome handlers ("local" | "cloud" | "server"). Mapping is centralised
// here so callers only carry one concept.
export function voiceAgentProfileForInstallMode(
  installMode: "local" | "cloud" | "server",
): VoiceAgentProfile {
  return installMode === "local" ? "on-prem" : "cloud-byok";
}

export async function applyVoiceAgentProfile(
  profile: VoiceAgentProfile,
): Promise<void> {
  const resp = await fetch("/api/v1/voice-agent/apply-profile", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ profile }),
  });
  if (!resp.ok) {
    const txt = await resp.text();
    throw new Error(`apply-profile failed: ${resp.status} ${txt}`);
  }
}
