import type { ReactNode } from "react";

import type { OverlayFeedbackMode } from "@/lib/speechkit";

const OVERLAY_FEEDBACK_MODE_OPTIONS: {
  value: OverlayFeedbackMode;
  label: string;
}[] = [
  { value: "big_productivity", label: "Big Productivity" },
  { value: "small_feedback", label: "Small Feedback" },
];

export function Section({
  title,
  children,
  className = "",
  testId,
}: {
  title: string;
  children: ReactNode;
  className?: string;
  testId?: string;
}) {
  return (
    <section
      data-testid={testId}
      className={["min-w-0 py-2", className].join(" ")}
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

export function Row({
  label,
  on,
  onToggle,
}: {
  label: string;
  on: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-sm text-[color:var(--sk-text)]">{label}</span>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={on}
        onClick={onToggle}
        className={[
          "relative inline-flex h-5.5 w-9.5 shrink-0 cursor-pointer items-center rounded-full transition-colors",
          on ? "bg-[color:var(--sk-accent)]" : "bg-[color:var(--sk-border)]",
        ].join(" ")}
      >
        <span
          className={[
            "inline-block h-4 w-4 rounded-full bg-white shadow transition-transform",
            on ? "translate-x-4.75" : "translate-x-0.75",
          ].join(" ")}
        />
      </button>
    </div>
  );
}

export function Chip({
  active,
  ariaLabel,
  onClick,
  disabled = false,
  children,
  className = "",
}: {
  active: boolean;
  ariaLabel?: string;
  onClick?: () => void;
  disabled?: boolean;
  children: ReactNode;
  className?: string;
}) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      aria-pressed={active}
      disabled={disabled}
      onClick={onClick}
      className={[
        "h-8 rounded-lg border px-3 text-xs font-medium transition-all",
        active
          ? "border-[color:var(--sk-accent)]/60 bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]"
          : disabled
            ? "cursor-not-allowed border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] text-[color:var(--sk-text-subtle)]"
            : "border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-text)]",
        className,
      ].join(" ")}
    >
      {children}
    </button>
  );
}

export function OverlayFeedbackModePicker({
  label,
  value,
  onChange,
}: {
  label: string;
  value: OverlayFeedbackMode;
  onChange: (value: OverlayFeedbackMode) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="mr-1 text-[11px] text-[color:var(--sk-text-muted)]">
        {label}
      </span>
      {OVERLAY_FEEDBACK_MODE_OPTIONS.map((option) => (
        <Chip
          key={option.value}
          active={value === option.value}
          ariaLabel={`${label} ${option.label}`}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </Chip>
      ))}
    </div>
  );
}
