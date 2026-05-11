import {
  fetchSettingsStateMock,
  fetchModelProfilesMock,
  saveSettingsStateMock,
} from "@/components/settings-test-harness";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { baseSettings } from "@/components/settings-test-fixtures";
import { SettingsApp } from "@/components/settings-app";

describe("SettingsApp navigation", () => {
  it("keeps general settings separate from per-mode controls", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    await screen.findByText("Startup");
    expect(screen.queryByText("Storage")).not.toBeInTheDocument();
    expect(screen.queryByText("Server Target")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Active mode Assist" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Active mode None" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Dictate hotkey")).not.toBeInTheDocument();
    expect(screen.queryByText("Assist hotkey")).not.toBeInTheDocument();
    expect(screen.queryByText("Voice Agent hotkey")).not.toBeInTheDocument();
    expect(screen.queryByText("Overlay feedback")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Assist Mode Big Productivity" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: "Voice Agent Mode Small Feedback",
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: "Auto-start on app launch" }),
    ).toHaveAttribute("aria-checked", "false");

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    expect(screen.getByText("Dictate hotkey")).toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: "Enable Dictate hotkey" }),
    ).toHaveAttribute("aria-checked", "true");

    fireEvent.click(screen.getByRole("button", { name: "Dictate hotkey Ctrl + Win" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          dictateHotkey: "ctrl+win",
          hotkey: "ctrl+win",
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Assist Mode" }));
    expect(screen.getByText("Assist hotkey")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Assist hotkey Ctrl + Shift" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistHotkey: "ctrl+shift",
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Voice Agent Mode" }));
    expect(screen.getByText("Voice Agent hotkey")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("switch", { name: "Enable Voice Agent hotkey" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          modeEnabled: expect.objectContaining({
            voice_agent: false,
          }),
          availableModes: expect.objectContaining({
            voice_agent: false,
          }),
          voiceAgentHotkey: "ctrl+shift",
        }),
      ),
    );

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Voice Agent Mode" }),
      ).toBeDisabled(),
    );
    expect(screen.queryByText("Voice Agent hotkey")).not.toBeInTheDocument();
  });

  it("renders General, Integrations, SpeechKit Server, and Storage as separate settings pages", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    expect(await screen.findByRole("button", { name: "General" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Integrations" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "SpeechKit Server" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Storage & Data" })).toBeInTheDocument();

    expect(screen.getByText("Startup")).toBeInTheDocument();
    expect(screen.queryByText("Storage")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Storage & Data" }));
    expect(await screen.findByText("Storage")).toBeInTheDocument();
    expect(screen.getByLabelText("Audio retention")).toBeInTheDocument();
    expect(screen.queryByText("Startup")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "SpeechKit Server" }));
    expect(await screen.findByText("Server Target")).toBeInTheDocument();
    expect(await screen.findByText("Mode Source")).toBeInTheDocument();
    expect(screen.queryByText("Storage")).not.toBeInTheDocument();
  });

  it("groups settings navigation under audio and mode section titles", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    expect(
      await screen.findByRole("button", { name: "General" }),
    ).toBeInTheDocument();

    const audioGroup = screen.getByTestId("settings-nav-group-audio");
    expect(audioGroup).toHaveTextContent("Audio Settings");
    expect(
      within(audioGroup).getByRole("button", { name: "Integrations" }),
    ).toBeInTheDocument();
    expect(
      within(audioGroup).getByRole("button", { name: "SpeechKit Server" }),
    ).toBeInTheDocument();
    expect(
      within(audioGroup).getByRole("button", { name: "Storage & Data" }),
    ).toBeInTheDocument();

    const modeGroup = screen.getByTestId("settings-nav-group-mode");
    expect(modeGroup).toHaveTextContent("Mode Settings");
    expect(
      within(modeGroup).getByRole("button", { name: "Transcribe Mode" }),
    ).toBeInTheDocument();
    expect(
      within(modeGroup).getByRole("button", { name: "Assist Mode" }),
    ).toBeInTheDocument();
    expect(
      within(modeGroup).getByRole("button", { name: "Voice Agent Mode" }),
    ).toBeInTheDocument();
  });

  it("keeps the settings navigation wide enough for grouped labels", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    expect(await screen.findByTestId("settings-nav-panel")).toHaveClass(
      "w-56",
    );
  });

  it("redirects away from a disabled initial mode tab", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      modeEnabled: {
        ...baseSettings.modeEnabled,
        voice_agent: false,
      },
      availableModes: {
        ...baseSettings.availableModes,
        voice_agent: false,
      },
    });

    render(<SettingsApp initialTab="realtime_voice" />);

    await screen.findByText("Startup");
    await waitFor(() =>
      expect(screen.queryByText("Voice Agent hotkey")).not.toBeInTheDocument(),
    );
    expect(
      screen.getByRole("button", { name: "Voice Agent Mode" }),
    ).toBeDisabled();
  });

  it("shows compact usage guidance on each mode settings page", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp initialTab="stt" />);

    expect(await screen.findByTestId("mode-guide-stt")).toHaveTextContent(
      "Use this for exact text output",
    );
    expect(screen.getByTestId("mode-guide-stt")).toHaveTextContent(
      "without Assist utilities.",
    );

    fireEvent.click(screen.getByRole("button", { name: "Assist Mode" }));
    expect(screen.getByTestId("mode-guide-assist")).toHaveTextContent(
      "Use this for one-shot transformations",
    );
    expect(screen.getByTestId("mode-guide-assist")).toHaveTextContent(
      "summarize this in three bullets",
    );

    fireEvent.click(screen.getByRole("button", { name: "Voice Agent Mode" }));
    expect(screen.getByTestId("mode-guide-realtime_voice")).toHaveTextContent(
      "Use this for live follow-up thinking",
    );
    expect(screen.getByTestId("mode-guide-realtime_voice")).toHaveTextContent(
      "help me decide the next release slice",
    );
  });

  it("renames the mode settings tabs and shows matching icons", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    expect(
      await screen.findByRole("button", { name: "Transcribe Mode" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Assist Mode" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Voice Agent Mode" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("settings-mode-nav-icon-stt")).toHaveAttribute(
      "data-icon",
      "audio-lines",
    );
    expect(screen.getByTestId("settings-mode-nav-icon-assist")).toHaveAttribute(
      "data-icon",
      "bot",
    );
    expect(
      screen.getByTestId("settings-mode-nav-icon-realtime_voice"),
    ).toHaveAttribute("data-icon", "headphones");
    expect(
      screen.queryByRole("button", { name: "STT" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the mode settings layout scrollable and flattens the top control chrome", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);
    fetchModelProfilesMock.mockResolvedValue(baseSettings.profiles ?? []);

    render(<SettingsApp initialTab="assist" />);

    await screen.findByText("Assist hotkey");

    expect(screen.getByTestId("settings-layout")).toHaveClass("min-h-0");
    expect(screen.getByTestId("settings-scroll-region")).toHaveClass(
      "overflow-y-auto",
    );
    expect(screen.getByTestId("assist-mode-controls")).not.toHaveClass(
      "sk-panel",
    );
  });
});
