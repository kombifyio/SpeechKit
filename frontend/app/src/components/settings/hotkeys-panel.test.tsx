import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { validateHotkeyCombo } from "@/components/settings/hotkey-combo-validation";
import { HotkeyPicker } from "@/components/settings/hotkeys-panel";

describe("validateHotkeyCombo", () => {
  it.each([
    "ctrl+shift",
    "win+alt",
    "ctrl+alt+f12",
    "ctrl+space",
    "ctrl+a",
    "alt+enter",
    "ctrl+escape",
    "shift+backspace",
    "ctrl+mouse-x1",
    "alt+mouse-x2",
    "mouse-x1",
    "f24",
    "mb4",
    "mb5",
  ])("accepts %s as a valid combo", (input) => {
    const result = validateHotkeyCombo(input);
    expect(result).toMatchObject({ valid: true });
  });

  it.each([
    "",
    " ",
    "+",
    "ctrl+",
    "ctrl+ctrl",
    "ctrl+nope",
    "f25",
    "f0",
    "mouse-x3",
    "mouse-left",
    "really_long_token",
  ])("rejects %s as invalid", (input) => {
    const result = validateHotkeyCombo(input);
    expect(result).toMatchObject({ valid: false });
    if (!result.valid) {
      expect(result.reason).toBeTruthy();
    }
  });

  it("normalises casing and surrounding whitespace", () => {
    const result = validateHotkeyCombo("  CTRL + SHIFT + A  ");
    expect(result).toMatchObject({ valid: true, normalized: "ctrl+shift+a" });
  });

  it("flags duplicate tokens with a specific reason", () => {
    const result = validateHotkeyCombo("ctrl+shift+ctrl");
    expect(result.valid).toBe(false);
    if (!result.valid) {
      expect(result.reason).toContain("ctrl");
    }
  });
});

describe("HotkeyPicker", () => {
  const baseProps = {
    label: "Dictate hotkey",
    enabled: true,
    behavior: "hold_to_talk" as const,
    defaultBase: "ctrl+win" as const,
    onToggleEnabled: () => {},
    onChangeBehavior: () => {},
  };

  it("renders the three preset chips with the current preset active", () => {
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+shift"
        onChange={() => {}}
      />,
    );
    const chip = screen.getByRole("button", {
      name: "Dictate hotkey Ctrl + Shift",
    });
    expect(chip).toHaveAttribute("aria-pressed", "true");
  });

  it("auto-opens the custom panel when the saved value is not a preset", () => {
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+alt+f12"
        onChange={() => {}}
      />,
    );
    const customInput = screen.getByLabelText("Dictate hotkey custom combo");
    expect(customInput).toHaveValue("ctrl+alt+f12");
  });

  it("calls onChange with the normalised custom combo when Apply is clicked", () => {
    const onChange = vi.fn();
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+win"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Dictate hotkey Custom" }));
    const input = screen.getByLabelText("Dictate hotkey custom combo");
    fireEvent.change(input, { target: { value: "ctrl+ALT+F11" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Dictate hotkey apply custom" }),
    );
    expect(onChange).toHaveBeenCalledWith("ctrl+alt+f11");
  });

  it("rejects invalid custom combo via inline alert and does not call onChange", () => {
    const onChange = vi.fn();
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+win"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Dictate hotkey Custom" }));
    const input = screen.getByLabelText("Dictate hotkey custom combo");
    fireEvent.change(input, { target: { value: "ctrl+nope" } });
    expect(screen.getByRole("alert")).toHaveTextContent("Unbekannter Token");
    fireEvent.click(
      screen.getByRole("button", { name: "Dictate hotkey apply custom" }),
    );
    expect(onChange).not.toHaveBeenCalled();
  });

  it("shows a conflict warning when another mode shares the same binding", () => {
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+shift"
        conflictWith={[{ label: "Voice Agent", value: "ctrl+shift" }]}
        onChange={() => {}}
      />,
    );
    const status = screen.getByRole("status", {
      name: "Dictate hotkey conflict",
    });
    expect(status).toHaveTextContent("Voice Agent");
  });

  it("does not show a conflict warning when bindings differ", () => {
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+shift"
        conflictWith={[{ label: "Voice Agent", value: "win+alt" }]}
        onChange={() => {}}
      />,
    );
    expect(
      screen.queryByRole("status", { name: "Dictate hotkey conflict" }),
    ).not.toBeInTheDocument();
  });

  it("accepts mouse-x1 as a custom combo for forward compatibility with mtb", () => {
    const onChange = vi.fn();
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+win"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Dictate hotkey Custom" }));
    const input = screen.getByLabelText("Dictate hotkey custom combo");
    fireEvent.change(input, { target: { value: "ctrl+mouse-x1" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Dictate hotkey apply custom" }),
    );
    expect(onChange).toHaveBeenCalledWith("ctrl+mouse-x1");
  });

  it("resets to the default base when Reset is clicked", () => {
    const onChange = vi.fn();
    render(
      <HotkeyPicker
        {...baseProps}
        value="ctrl+alt+f12"
        defaultBase="ctrl+win"
        onChange={onChange}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Dictate hotkey reset to default" }),
    );
    expect(onChange).toHaveBeenCalledWith("ctrl+win");
  });
});
