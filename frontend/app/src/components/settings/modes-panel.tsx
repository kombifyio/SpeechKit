import type { ReactNode } from "react";

export type ModeGuideKey = "stt" | "assist" | "realtime_voice";

const MODE_GUIDES = {
  stt: {
    label: "Dictation",
    title: "Use this for exact text output",
    summary:
      "Dictation should be the fastest path: record, transcribe, apply your dictionary, and type into the focused app without Assist utilities.",
    checks: [
      "Test in a plain text field before long writing sessions.",
      "Add product names, people, and acronyms after the first miss.",
      "Keep one primary STT model ready before relying on the hotkey.",
    ],
    tryThis:
      "Hold the trigger, say one complete sentence, release, then check whether the text landed where the cursor was.",
  },
  assist: {
    label: "Assist",
    title: "Use this for one-shot transformations",
    summary:
      "Assist works best when you want one result: summarize, rewrite, draft, copy, insert, or save a quick note.",
    checks: [
      "Select editable text when you expect a direct replacement.",
      "Use exact utility phrases before relying on open-ended prompts.",
      "Long or uncertain results stay in the Assist panel for review.",
    ],
    tryThis: "Select a paragraph and say: summarize this in three bullets.",
  },
  realtime_voice: {
    label: "Voice Agent",
    title: "Use this for live follow-up thinking",
    summary:
      "Voice Agent is a realtime session for dialogue, interruption, summaries, and decisions. It is intentionally separate from direct text insertion.",
    checks: [
      "Pick a low-latency realtime provider before long sessions.",
      "Keep session summaries enabled when decisions matter.",
      "Use close behavior to choose between parking and ending a chat.",
    ],
    tryThis:
      "Start a session and ask: help me decide the next release slice, then interrupt with a follow-up.",
  },
} as const;

export function ModeGuidePanel({ mode }: { mode: ModeGuideKey }) {
  const guide = MODE_GUIDES[mode];
  return (
    <section
      data-testid={`mode-guide-${mode}`}
      className="mb-5 rounded-[22px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] px-4 py-3.5"
    >
      <div className="grid gap-4 xl:grid-cols-[1.05fr_1.35fr_0.9fr] xl:items-start">
        <div>
          <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-accent)]">
            {guide.label} guide
          </p>
          <h3 className="mt-1 text-sm font-semibold text-[color:var(--sk-text)]">
            {guide.title}
          </h3>
          <p className="mt-1.5 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]">
            {guide.summary}
          </p>
        </div>
        <div className="grid gap-2 sm:grid-cols-3 xl:grid-cols-1">
          {guide.checks.map((check) => (
            <div
              key={check}
              className="flex items-start gap-2 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]"
            >
              <span className="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-[color:var(--sk-accent)]" />
              <span>{check}</span>
            </div>
          ))}
        </div>
        <div className="rounded-[16px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 py-2.5">
          <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-subtle)]">
            Try next
          </p>
          <p className="mt-1.5 text-[11px] leading-relaxed text-[color:var(--sk-text)]/80">
            {guide.tryThis}
          </p>
        </div>
      </div>
    </section>
  );
}

export function ModeSection({
  title,
  children,
  testId,
}: {
  title: string;
  children: ReactNode;
  testId?: string;
}) {
  return (
    <section
      data-testid={testId}
      className="min-w-0 bg-transparent py-1"
    >
      <div className="mb-4 border-b border-[color:var(--sk-shell-divider)]/85 pb-3">
        <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
          {title}
        </span>
      </div>
      {children}
    </section>
  );
}
