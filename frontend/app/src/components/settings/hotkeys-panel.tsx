import { useId, useState, type ReactNode } from "react";

import { validateHotkeyCombo } from "@/components/settings/hotkey-combo-validation";
import type { HotkeyBehavior } from "@/lib/speechkit";

const HOTKEY_SUFFIX_KEYS = ["", "d", "j", "k", "v", "space"] as const;
const HOTKEY_BASE_OPTIONS = [
  { value: "win+alt", label: "Win + Alt" },
  { value: "ctrl+win", label: "Ctrl + Win" },
  { value: "ctrl+shift", label: "Ctrl + Shift" },
] as const;
const HOTKEY_BEHAVIOR_OPTIONS = [
  { value: "hold_to_talk", label: "Hold to talk" },
  { value: "toggle", label: "Toggle on press" },
] as const;

export type HotkeyBase = (typeof HOTKEY_BASE_OPTIONS)[number]["value"];
type HotkeySuffix = (typeof HOTKEY_SUFFIX_KEYS)[number];

function parseModeHotkeyValue(
  value: string,
  fallbackBase: HotkeyBase,
): { base: HotkeyBase; suffix: HotkeySuffix; isPreset: boolean } {
  const normalized = value.trim().toLowerCase();
  for (const option of HOTKEY_BASE_OPTIONS) {
    if (normalized === option.value) {
      return { base: option.value, suffix: "", isPreset: true };
    }
    const prefix = `${option.value}+`;
    if (normalized.startsWith(prefix)) {
      const suffix = normalized.slice(prefix.length) as HotkeySuffix;
      if (HOTKEY_SUFFIX_KEYS.includes(suffix)) {
        return { base: option.value, suffix, isPreset: true };
      }
    }
  }
  return { base: fallbackBase, suffix: "", isPreset: false };
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
  conflictWith,
  onToggleEnabled,
  onChange,
  onChangeBehavior,
}: {
  label: string;
  enabled: boolean;
  value: string;
  behavior: HotkeyBehavior;
  defaultBase: HotkeyBase;
  /** Other mode hotkeys to check for duplicate-binding conflicts. */
  conflictWith?: ReadonlyArray<{ label: string; value: string }>;
  onToggleEnabled: () => void;
  onChange: (value: string) => void;
  onChangeBehavior: (value: HotkeyBehavior) => void;
}) {
  const { base, suffix, isPreset } = parseModeHotkeyValue(value, defaultBase);
  // Local override for the editable draft. Null means "echo the live value"
  // so the input always reflects what was saved when the picker re-opens.
  const [draftOverride, setDraftOverride] = useState<string | null>(null);
  // Whether the custom panel is explicitly opened. Non-preset values force
  // it open implicitly so the user can see and edit the saved value.
  const [customExplicitlyOpen, setCustomExplicitlyOpen] =
    useState<boolean>(false);
  const inputId = useId();

  const customOpen = customExplicitlyOpen || !isPreset;
  const customDraft = draftOverride ?? (isPreset ? "" : value.trim());

  const conflict = conflictWith?.find(
    (other) =>
      other.value.trim().toLowerCase() === value.trim().toLowerCase() &&
      other.value.trim().length > 0,
  );

  const draftValidation =
    customDraft.length > 0 ? validateHotkeyCombo(customDraft) : null;

  const handleCustomApply = () => {
    if (!draftValidation || !draftValidation.valid) return;
    setDraftOverride(null);
    onChange(draftValidation.normalized);
  };

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
            active={isPreset && base === option.value}
            ariaLabel={`${label} ${option.label}`}
            onClick={() => {
              setCustomExplicitlyOpen(false);
              setDraftOverride(null);
              onChange(option.value);
            }}
          >
            {option.label}
          </HotkeyChip>
        ))}
        <select
          aria-label={`${label} suffix`}
          value={isPreset ? suffix : ""}
          disabled={!isPreset}
          onChange={(event) =>
            onChange(buildModeHotkey(base, event.target.value as HotkeySuffix))
          }
          className="sk-input h-8 rounded-lg px-2.5 text-xs font-medium disabled:opacity-50"
        >
          {HOTKEY_SUFFIX_KEYS.map((key) => (
            <option key={key || "none"} value={key}>
              {key === "" ? "None" : key === "space" ? "Space" : key.toUpperCase()}
            </option>
          ))}
        </select>
        <HotkeyChip
          active={customOpen}
          ariaLabel={`${label} Custom`}
          onClick={() => setCustomExplicitlyOpen((open) => !open)}
        >
          Custom…
        </HotkeyChip>
      </div>
      {customOpen && (
        <div className="mt-2 flex flex-col gap-1">
          <label
            htmlFor={inputId}
            className="text-[11px] font-medium text-[color:var(--sk-text-muted)]"
          >
            Custom-Combo (z.B. <code>ctrl+alt+f12</code>,{" "}
            <code>ctrl+mouse-x1</code>)
          </label>
          <div className="flex items-center gap-1.5">
            <input
              id={inputId}
              type="text"
              value={customDraft}
              onChange={(event) => setDraftOverride(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  handleCustomApply();
                }
              }}
              placeholder={value || defaultBase}
              aria-label={`${label} custom combo`}
              className="sk-input h-8 flex-1 rounded-lg px-2.5 text-xs font-mono"
            />
            <HotkeyChip
              active={false}
              ariaLabel={`${label} apply custom`}
              onClick={handleCustomApply}
            >
              Übernehmen
            </HotkeyChip>
            <HotkeyChip
              active={false}
              ariaLabel={`${label} reset to default`}
              onClick={() => {
                setDraftOverride(null);
                setCustomExplicitlyOpen(false);
                onChange(defaultBase);
              }}
            >
              Reset
            </HotkeyChip>
          </div>
          {draftValidation && !draftValidation.valid && (
            <p className="text-[11px] text-red-500" role="alert">
              {draftValidation.reason}
            </p>
          )}
          {draftValidation && draftValidation.valid && (
            <p className="text-[11px] text-[color:var(--sk-text-muted)]">
              OK · wird als <code>{draftValidation.normalized}</code> gespeichert.
            </p>
          )}
        </div>
      )}
      {conflict && (
        <p
          className="mt-1.5 text-[11px] text-amber-500"
          role="status"
          aria-label={`${label} conflict`}
        >
          ⚠ Gleiche Combo wie {conflict.label}. Das ist erlaubt, aber der zuletzt
          aktive Modus gewinnt.
        </p>
      )}
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
