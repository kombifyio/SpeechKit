import type { ReactNode } from "react";

import type { HotkeyBehavior } from "@/lib/speechkit";

const HOTKEY_SUFFIX_KEYS = ["", "d", "j", "k", "v", "space"] as const;
const HOTKEY_BASE_OPTIONS = [
  { value: "win+alt", label: "Win + Alt" },
  { value: "ctrl+win", label: "Ctrl + Win" },
  { value: "ctrl+shift", label: "Ctrl + Shift" },
] as const;
const HOTKEY_BEHAVIOR_OPTIONS = [
  { value: "push_to_talk", label: "Hold to talk" },
  { value: "toggle", label: "Toggle on press" },
] as const;

export type HotkeyBase = (typeof HOTKEY_BASE_OPTIONS)[number]["value"];
type HotkeySuffix = (typeof HOTKEY_SUFFIX_KEYS)[number];

function parseModeHotkeyValue(
  value: string,
  fallbackBase: HotkeyBase,
): { base: HotkeyBase; suffix: HotkeySuffix } {
  const normalized = value.trim().toLowerCase();
  for (const option of HOTKEY_BASE_OPTIONS) {
    if (normalized === option.value) {
      return { base: option.value, suffix: "" };
    }
    const prefix = `${option.value}+`;
    if (normalized.startsWith(prefix)) {
      const suffix = normalized.slice(prefix.length) as HotkeySuffix;
      if (HOTKEY_SUFFIX_KEYS.includes(suffix)) {
        return { base: option.value, suffix };
      }
    }
  }
  return { base: fallbackBase, suffix: "" };
}

function buildModeHotkey(base: HotkeyBase, suffix: HotkeySuffix) {
  return suffix ? `${base}+${suffix}` : base;
}

export function HotkeyPicker({
  label,
  enabled,
  value,
  behavior,
  defaultBase,
  onToggleEnabled,
  onChange,
  onChangeBehavior,
}: {
  label: string;
  enabled: boolean;
  value: string;
  behavior: HotkeyBehavior;
  defaultBase: HotkeyBase;
  onToggleEnabled: () => void;
  onChange: (value: string) => void;
  onChangeBehavior: (value: HotkeyBehavior) => void;
}) {
  const { base, suffix } = parseModeHotkeyValue(value, defaultBase);

  return (
    <div>
      <div className="mb-1.5 text-xs font-medium text-[color:var(--sk-text)]">
        {label}
      </div>
      <div className="mb-2">
        <HotkeyToggleRow
          label={`Enable ${label}`}
          on={enabled}
          onToggle={onToggleEnabled}
        />
      </div>
      <div className="mb-2 flex flex-wrap items-center gap-1.5">
        {HOTKEY_BEHAVIOR_OPTIONS.map((option) => (
          <HotkeyChip
            key={option.value}
            active={behavior === option.value}
            ariaLabel={`${label} ${option.label}`}
            onClick={() => onChangeBehavior(option.value)}
          >
            {option.label}
          </HotkeyChip>
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        {HOTKEY_BASE_OPTIONS.map((option) => (
          <HotkeyChip
            key={option.value}
            active={base === option.value}
            ariaLabel={`${label} ${option.label}`}
            onClick={() => onChange(option.value)}
          >
            {option.label}
          </HotkeyChip>
        ))}
        <select
          aria-label={`${label} suffix`}
          value={suffix}
          onChange={(event) =>
            onChange(buildModeHotkey(base, event.target.value as HotkeySuffix))
          }
          className="sk-input h-8 rounded-lg px-2.5 text-xs font-medium"
        >
          {HOTKEY_SUFFIX_KEYS.map((key) => (
            <option key={key || "none"} value={key}>
              {key === "" ? "None" : key === "space" ? "Space" : key.toUpperCase()}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

function HotkeyChip({
  active,
  ariaLabel,
  onClick,
  children,
}: {
  active: boolean;
  ariaLabel?: string;
  onClick?: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      aria-pressed={active}
      onClick={onClick}
      className={[
        "h-8 rounded-lg border px-3 text-xs font-medium transition-all",
        active
          ? "border-[color:var(--sk-accent)]/60 bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)]"
          : "border-[color:var(--sk-border)] bg-[color:var(--sk-surface-0)] text-[color:var(--sk-text-muted)] hover:border-[color:var(--sk-accent)]/30 hover:text-[color:var(--sk-text)]",
      ].join(" ")}
    >
      {children}
    </button>
  );
}

function HotkeyToggleRow({
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
