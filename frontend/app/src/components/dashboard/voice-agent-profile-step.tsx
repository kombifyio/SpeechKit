import { useState } from "react";

type Profile = "on-prem" | "cloud-byok";

interface ProfileCardProps {
  profile: Profile;
  title: string;
  badges: string[];
  body: string;
  selected: boolean;
  applying: boolean;
  onSelect: () => void;
}

function ProfileCard({
  title,
  badges,
  body,
  selected,
  applying,
  onSelect,
}: ProfileCardProps) {
  return (
    <button
      type="button"
      className={[
        "w-full text-left rounded-2xl border px-5 py-4 transition-all",
        selected
          ? "border-[#cabeff]/40 bg-[#cabeff]/8"
          : "border-[#35343a] bg-[#1b1b20] hover:border-[#484555]",
        applying ? "cursor-not-allowed opacity-60" : "",
      ].join(" ")}
      onClick={onSelect}
      disabled={applying}
      aria-pressed={selected}
    >
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
    </button>
  );
}

interface VoiceAgentProfileStepProps {
  onBack: () => void;
  onNext: () => void;
}

export function VoiceAgentProfileStep({
  onBack,
  onNext,
}: VoiceAgentProfileStepProps) {
  const [selected, setSelected] = useState<Profile | null>(null);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const apply = async () => {
    if (!selected) return;
    setApplying(true);
    setError(null);
    try {
      const resp = await fetch("/api/v1/voice-agent/apply-profile", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ profile: selected }),
      });
      if (!resp.ok) {
        const txt = await resp.text();
        throw new Error(`Apply failed: ${resp.status} ${txt}`);
      }
      onNext();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setApplying(false);
    }
  };

  const applyLabel = applying
    ? "Applying…"
    : selected === "on-prem"
      ? "Apply On-Prem"
      : selected === "cloud-byok"
        ? "Apply BYOK Cloud"
        : "Apply profile";

  return (
    <div className="flex flex-col text-left max-w-2xl w-full h-full overflow-y-auto py-8 z-10">
      <h2 className="text-3xl font-extrabold tracking-tight mb-2">
        Choose your Voice Agent profile
      </h2>
      <p className="text-[#b5b3c4] text-sm leading-relaxed mb-6">
        SpeechKit&rsquo;s Voice Agent can run fully on-prem (zero egress, BSI C5 /
        ISO&nbsp;27001 ready) or via your own Google Cloud project (sub-second
        latency, DSGVO with DPA). See{" "}
        <a
          href="https://github.com/kombifyio/SpeechKit/blob/main/docs/compliance/voice-agent-decision-tree.md"
          target="_blank"
          rel="noreferrer"
          className="text-[#cabeff] hover:text-[#e4e1e9] underline underline-offset-2"
        >
          the decision tree
        </a>{" "}
        for the detailed reasoning.
      </p>

      <div className="space-y-3 mb-6">
        <ProfileCard
          profile="on-prem"
          title="On-Prem (Local Cascaded)"
          badges={["Zero egress", "BSI C5 ready"]}
          body="whisper.cpp + local LLM + local TTS, no cloud. Higher latency, full data residency."
          selected={selected === "on-prem"}
          applying={applying}
          onSelect={() => setSelected("on-prem")}
        />
        <ProfileCard
          profile="cloud-byok"
          title="BYOK Cloud (Gemini Live)"
          badges={["Sub-1s latency", "EU region"]}
          body="Your own Google Cloud project, EU region pin, DPA + SCCs apply. Customer is data controller."
          selected={selected === "cloud-byok"}
          applying={applying}
          onSelect={() => setSelected("cloud-byok")}
        />
      </div>

      {error && (
        <div className="rounded-xl border border-red-400/15 bg-red-500/8 px-4 py-3 text-xs text-red-200/85 mb-4">
          {error}
        </div>
      )}

      <div className="shrink-0 flex items-center justify-between gap-3 border-t border-[#35343a]/50 bg-[#131318]/95 pt-4 backdrop-blur-xl">
        <button
          type="button"
          onClick={onBack}
          className="text-[#b5b3c4] hover:text-[#e4e1e9] text-sm font-medium transition-colors"
        >
          Back
        </button>
        <button
          type="button"
          onClick={() => void apply()}
          disabled={!selected || applying}
          className="signature-gradient text-[#2b0088] h-12 rounded-full px-8 font-bold transition-all ambient-glow active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {applyLabel}
        </button>
      </div>
    </div>
  );
}
