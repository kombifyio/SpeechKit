import { useEffect, useRef, useState } from "react";

import type { OnboardingTarget } from "./setup-wizard-data";

type Profile = "on-prem" | "cloud-byok";

// Derive the voice-agent profile from the onboarding target chosen on the
// welcome screen. The user already declared local vs. cloud intent up front,
// so we do not ask again here — we apply the matching profile and let the
// user review the result.
function profileForTarget(target: OnboardingTarget): Profile {
  return target === "cloud" ? "cloud-byok" : "on-prem";
}

interface ProfileSummaryProps {
  profile: Profile;
}

function ProfileSummary({ profile }: ProfileSummaryProps) {
  const title =
    profile === "on-prem"
      ? "On-Prem (Local Cascaded)"
      : "BYOK Cloud (Gemini Live)";
  const badges =
    profile === "on-prem"
      ? ["Zero egress", "BSI C5 ready"]
      : ["Sub-1s latency", "EU region"];
  const body =
    profile === "on-prem"
      ? "whisper.cpp + local LLM + local TTS, no cloud. Higher latency, full data residency."
      : "Your own Google Cloud project, EU region pin, DPA + SCCs apply. Customer is data controller.";
  return (
    <div className="rounded-2xl border border-[#cabeff]/40 bg-[#cabeff]/8 px-5 py-4">
      <div className="flex flex-wrap items-center gap-2 mb-2">
        <span className="text-[#e4e1e9] font-semibold text-base">{title}</span>
        {badges.map((b) => (
          <span
            key={b}
            className="rounded border border-[#484555] px-2 py-0.5 text-[9px] font-semibold uppercase tracking-[0.12em] text-[#cabeff]"
          >
            {b}
          </span>
        ))}
      </div>
      <p className="text-[#b5b3c4] text-sm leading-relaxed">{body}</p>
    </div>
  );
}

type ApplyStatus = "applying" | "applied" | "error";

interface VoiceAgentProfileStepProps {
  target: OnboardingTarget;
  onBack: () => void;
  onNext: () => void;
}

export function VoiceAgentProfileStep({
  target,
  onBack,
  onNext,
}: VoiceAgentProfileStepProps) {
  const profile = profileForTarget(target);
  const [status, setStatus] = useState<ApplyStatus>("applying");
  const [error, setError] = useState<string | null>(null);
  // Guard against double-apply when React StrictMode mounts the effect twice
  // in dev — the apply-profile POST is not idempotent in test fixtures and
  // ApplyStatus oscillation is harmless but confusing in the UI.
  const appliedRef = useRef(false);
  const retryNonce = useRef(0);
  const [retrying, setRetrying] = useState(0);

  useEffect(() => {
    if (appliedRef.current && retrying === 0) return;
    appliedRef.current = true;
    setStatus("applying");
    setError(null);
    const nonce = ++retryNonce.current;
    void (async () => {
      try {
        const resp = await fetch("/api/v1/voice-agent/apply-profile", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ profile }),
        });
        if (nonce !== retryNonce.current) return;
        if (!resp.ok) {
          const txt = await resp.text();
          throw new Error(`Apply failed: ${resp.status} ${txt}`);
        }
        setStatus("applied");
      } catch (e: unknown) {
        if (nonce !== retryNonce.current) return;
        setStatus("error");
        setError(e instanceof Error ? e.message : String(e));
      }
    })();
  }, [profile, retrying]);

  const retry = () => {
    setRetrying((n) => n + 1);
  };

  const headline =
    target === "cloud"
      ? "You chose the Cloud path on the welcome screen — the BYOK Cloud profile is being applied."
      : "You chose the Local path on the welcome screen — the On-Prem cascaded profile is being applied.";

  return (
    <div className="flex flex-col text-left max-w-2xl w-full h-full overflow-y-auto py-8 z-10">
      <h2 className="text-3xl font-extrabold tracking-tight mb-2">
        Voice Agent profile
      </h2>
      <p className="text-[#b5b3c4] text-sm leading-relaxed mb-6">
        {headline}{" "}
        <a
          href="https://github.com/kombifyio/SpeechKit/blob/main/docs/compliance/voice-agent-decision-tree.md"
          target="_blank"
          rel="noreferrer"
          className="text-[#cabeff] hover:text-[#e4e1e9] underline underline-offset-2"
        >
          See the decision tree
        </a>{" "}
        for the detailed reasoning. You can switch profiles later from Settings.
      </p>

      <div className="space-y-3 mb-6">
        <ProfileSummary profile={profile} />
      </div>

      <div
        role="status"
        aria-live="polite"
        data-testid="voice-agent-profile-status"
        className="rounded-xl border px-4 py-3 text-xs mb-4 border-[#35343a] bg-[#1b1b20] text-[#b5b3c4]"
      >
        {status === "applying" && (
          <span data-testid="voice-agent-profile-applying">
            Applying {profile === "on-prem" ? "On-Prem" : "BYOK Cloud"} profile…
          </span>
        )}
        {status === "applied" && (
          <span
            className="text-[#9ddf9a]"
            data-testid="voice-agent-profile-applied"
          >
            Profile applied. You can continue.
          </span>
        )}
        {status === "error" && (
          <span
            className="text-red-200/85"
            data-testid="voice-agent-profile-error"
          >
            {error}
          </span>
        )}
      </div>

      <div className="shrink-0 flex items-center justify-between gap-3 border-t border-[#35343a]/50 bg-[#131318]/95 pt-4 backdrop-blur-xl">
        <button
          type="button"
          onClick={onBack}
          className="text-[#b5b3c4] hover:text-[#e4e1e9] text-sm font-medium transition-colors"
        >
          Back
        </button>
        <div className="flex items-center gap-3">
          {status === "error" && (
            <button
              type="button"
              onClick={retry}
              className="text-[#cabeff] hover:text-[#e4e1e9] text-sm font-semibold transition-colors"
            >
              Retry
            </button>
          )}
          <button
            type="button"
            onClick={onNext}
            disabled={status !== "applied"}
            className="signature-gradient text-[#2b0088] h-12 rounded-full px-8 font-bold transition-all ambient-glow active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Continue
          </button>
        </div>
      </div>
    </div>
  );
}
