// Token-level validation for the custom hotkey combo input.
// Lives in its own file so the picker component can stay
// react-refresh-compatible (components only).

const MODIFIER_TOKENS = new Set([
  "alt",
  "ctrl",
  "control",
  "shift",
  "win",
  "windows",
  "cmd",
  "meta",
]);

const NAMED_KEY_TOKENS = new Set([
  "space",
  "enter",
  "return",
  "esc",
  "escape",
  "tab",
  "backspace",
]);

// Mouse side-button tokens. The low-level mouse activation lands in
// v0.39.0 via the polling path in internal/hotkey/Manager.pollLoop
// (kombify-SpeechKit-mtb). Left/right/middle buttons are intentionally
// excluded — binding them would block everyday clicks.
const MOUSE_TOKENS = new Set(["mouse-x1", "mouse-x2", "mb4", "mb5"]);

const FUNCTION_KEY_RE = /^f([1-9]|1[0-9]|2[0-4])$/;
const SINGLE_ALPHANUM_RE = /^[a-z0-9]$/;

export type ComboValidation =
  | { valid: true; normalized: string }
  | { valid: false; reason: string };

export function validateHotkeyCombo(value: string): ComboValidation {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return { valid: false, reason: "Combo darf nicht leer sein." };
  }
  if (trimmed.startsWith("+") || trimmed.endsWith("+")) {
    return {
      valid: false,
      reason: "Combo darf nicht mit `+` beginnen oder enden.",
    };
  }
  const tokens = trimmed
    .toLowerCase()
    .split("+")
    .map((t) => t.trim());
  if (tokens.some((t) => t.length === 0)) {
    return { valid: false, reason: "Leerer Token zwischen `+`." };
  }
  const seen = new Set<string>();
  for (const token of tokens) {
    if (seen.has(token)) {
      return { valid: false, reason: `Doppelter Token: ${token}` };
    }
    seen.add(token);
    const known =
      MODIFIER_TOKENS.has(token) ||
      NAMED_KEY_TOKENS.has(token) ||
      MOUSE_TOKENS.has(token) ||
      FUNCTION_KEY_RE.test(token) ||
      SINGLE_ALPHANUM_RE.test(token);
    if (!known) {
      return { valid: false, reason: `Unbekannter Token: ${token}` };
    }
  }
  return { valid: true, normalized: tokens.join("+") };
}
