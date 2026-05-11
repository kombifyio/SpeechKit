import {
  fetchSettingsStateMock,
  fetchModelProfilesMock,
  fetchOverlayStateMock,
  resetOverlayPositionMock,
  saveSettingsStateMock,
  fetchAPIV1DictionaryMock,
  fetchDownloadCatalogMock,
  selectDownloadedModelMock,
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
import type { SpeechKitSettingsState } from "@/lib/speechkit";

describe("SettingsApp modes", () => {
  it("keeps model selection available while hugging face inference is off", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: "openai/whisper-large-v3",
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    const modelsSection = await screen.findByText("Model setup");
    expect(modelsSection).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Transcribe Mode" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "TTS" }),
    ).not.toBeInTheDocument();
  });

  it("shows user dictionary usage counts from the API", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);
    fetchAPIV1DictionaryMock.mockResolvedValue({
      language: "de",
      entries: [
        {
          id: 1,
          spoken: "kombi fire",
          canonical: "Kombify",
          language: "de",
          source: "settings",
          enabled: true,
          usageCount: 7,
        },
      ],
    });

    render(<SettingsApp initialTab="stt" />);

    expect(await screen.findByText("Kombify")).toBeInTheDocument();
    expect(screen.getByText("7 uses")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Manage list" }));

    expect(
      await screen.findByLabelText("Dictionary usage count 1"),
    ).toHaveTextContent("7");
    expect(screen.getByRole("button", { name: "Import" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Export" })).toBeInTheDocument();
  });

  it("saves model changes even while hugging face inference is off", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      hfEnabled: false,
      hfModel: "openai/whisper-large-v3",
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    await screen.findByText("Model setup");
    expect(screen.getByText("Primary")).toBeInTheDocument();
  });

  it("keeps Assist LLM selection on Assist Mode, not Transcribe Mode", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whispercpp",
          modality: "stt",
          name: "Whisper.cpp (Local Built-in)",
          providerKind: "local_built_in",
          executionMode: "local",
          source: "Local Built-in",
          description: "SpeechKit-managed local transcription runtime.",
        },
        {
          id: "assist.builtin.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Local Built-in)",
          providerKind: "local_built_in",
          executionMode: "local",
          source: "Local Built-in",
          description: "Recommended local Assist LLM.",
          recommended: true,
        },
        {
          id: "assist.ollama.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Ollama)",
          providerKind: "local_provider",
          executionMode: "ollama_local",
          source: "Local Provider",
          description: "Externally managed local Assist LLM.",
        },
      ],
      activeProfiles: { stt: "stt.local.whispercpp" },
      modelSelections: {
        ...baseSettings.modelSelections,
        dictate: {
          primaryProfileId: "stt.local.whispercpp",
          fallbackProfileId: "",
        },
        assist: {
          primaryProfileId: "",
          fallbackProfileId: "",
        },
      },
    });

    render(<SettingsApp initialTab="stt" />);

    await screen.findByText("Model setup");
    expect(
      screen.queryByTestId("transcribe-companion-llm-selection"),
    ).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Assist LLM")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Assist Mode" }));

    const assistSelection = await screen.findByTestId("assist-llm-selection");
    const llmSelect = within(assistSelection).getByLabelText("Assist LLM");

    expect(llmSelect).toHaveValue("assist.builtin.gemma4-e4b");
    expect(assistSelection).toHaveTextContent("Gemma 4 E4B (Local Built-in)");

    fireEvent.change(llmSelect, {
      target: { value: "assist.ollama.gemma4-e4b" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          modelSelections: expect.objectContaining({
            assist: expect.objectContaining({
              primaryProfileId: "assist.ollama.gemma4-e4b",
            }),
          }),
        }),
      ),
    );
  });

  it("places routing controls above model setup without a card and defaults primary", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.builtin.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Local Built-in)",
          providerKind: "local_built_in",
          executionMode: "local",
          source: "Local Built-in",
          description: "SpeechKit-managed llama.cpp runtime.",
        },
      ],
      activeProfiles: {},
      modelSelections: {
        ...baseSettings.modelSelections,
        assist: { primaryProfileId: "", fallbackProfileId: "" },
      },
    });

    render(<SettingsApp initialTab="assist" />);

    const routing = await screen.findByTestId("model-routing-controls");
    const modelSetup = screen.getByText("Model setup");

    expect(
      routing.compareDocumentPosition(modelSetup) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(routing.className).not.toContain("bg-[color:var(--sk-surface-1)]");
    expect(screen.getByLabelText("Assist LLM")).toHaveValue(
      "assist.builtin.gemma4-e4b",
    );
    expect(within(routing).getByText("Primary & fallback")).toBeInTheDocument();
  });

  it("renders model descriptions for live-switchable profiles", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.builtin.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Local Built-in)",
          executionMode: "local",
          source: "Local Built-in",
          description: "SpeechKit-managed llama.cpp runtime.",
        },
        {
          id: "assist.ollama.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Ollama)",
          executionMode: "ollama_local",
          source: "Local Provider",
          description:
            "Laptop-friendly local model for summaries and quick actions.",
        },
      ],
      activeProfiles: { assist: "assist.ollama.gemma4-e4b" },
    });

    render(<SettingsApp />);

    const assistButtons = await screen.findAllByRole("button", {
      name: "Assist Mode",
    });
    fireEvent.click(assistButtons[0]);

    expect(
      await screen.findByText(/laptop-friendly local model/i),
    ).toBeInTheDocument();
    const builtInGroup = screen.getByTestId(
      "model-provider-group-local_built_in",
    );
    const localProviderGroup = screen.getByTestId(
      "model-provider-group-local_provider",
    );
    expect(builtInGroup).toHaveTextContent("Local Built-in");
    expect(builtInGroup).toHaveTextContent("built-in");
    expect(localProviderGroup).toHaveTextContent("Local Provider");
    expect(localProviderGroup).toHaveTextContent("provider");
  });

  it("groups model profiles into the four V23 provider kinds", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whisper-small",
          modality: "stt",
          name: "Whisper Small (Local)",
          providerKind: "local_built_in",
          executionMode: "local",
          description: "Small bundled model",
        },
        {
          id: "stt.local.whisper-large",
          modality: "stt",
          name: "Whisper Large (Local)",
          providerKind: "local_built_in",
          executionMode: "local",
          description: "Large bundled model",
        },
        {
          id: "stt.ollama.gemma4-e4b-transcribe",
          modality: "stt",
          name: "Gemma 4 E4B Transcribe (Ollama)",
          providerKind: "local_provider",
          executionMode: "ollama_local",
          description: "Ollama transcription adapter",
        },
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          providerKind: "cloud_provider",
          executionMode: "hf_routed",
          description: "Cloud provider profile",
        },
        {
          id: "stt.openai.whisper-1",
          modality: "stt",
          name: "Whisper-1 (OpenAI)",
          providerKind: "direct_provider",
          executionMode: "openai_api",
          description: "Direct provider profile",
        },
      ],
      activeProfiles: { stt: "stt.local.whisper-small" },
      modelSelections: {
        ...baseSettings.modelSelections,
        dictate: {
          primaryProfileId: "stt.local.whisper-small",
          fallbackProfileId: "",
        },
      },
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
        openai: {
          provider: "openai",
          label: "OpenAI",
          envName: "OPENAI_API_KEY",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
      },
    });

    render(<SettingsApp initialTab="stt" />);

    expect(
      await screen.findByTestId("model-provider-group-local_built_in"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("model-provider-group-local_provider"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("model-provider-group-cloud_provider"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("model-provider-group-direct_provider"),
    ).toBeInTheDocument();
    const builtInGroup = screen.getByTestId(
      "model-provider-group-local_built_in",
    );
    expect(
      within(builtInGroup).getByText("Whisper Small (Local)"),
    ).toBeInTheDocument();
    expect(
      within(builtInGroup).getByText("Whisper Large (Local)"),
    ).toBeInTheDocument();
  });

  it("uses General as the master place for all three mode toggles", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    await screen.findByText("Startup");

    expect(
      screen.getByRole("switch", { name: "Transcribe Mode" }),
    ).toHaveAttribute("aria-checked", "true");
    expect(
      screen.getByRole("switch", { name: "Assist Mode" }),
    ).toHaveAttribute("aria-checked", "true");
    expect(
      screen.getByRole("switch", { name: "Voice Agent Mode" }),
    ).toHaveAttribute("aria-checked", "true");

    fireEvent.click(screen.getByRole("switch", { name: "Voice Agent Mode" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          modeEnabled: expect.objectContaining({
            voice_agent: false,
          }),
          availableModes: expect.objectContaining({
            voice_agent: false,
          }),
        }),
      ),
    );
    expect(
      screen.getByRole("button", { name: "Voice Agent Mode" }),
    ).toBeDisabled();
  });

  it("supports an optional third key for each two-key hotkey base", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      assistHotkey: "ctrl+win+j",
      voiceAgentHotkey: "ctrl+shift+k",
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Assist Mode" }));
    await screen.findByText("Assist hotkey");

    expect(
      screen.getByRole("button", { name: "Assist hotkey Ctrl + Win" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Assist hotkey suffix")).toHaveValue("j");

    fireEvent.click(screen.getByRole("button", { name: "Transcribe Mode" }));

    fireEvent.change(screen.getByLabelText("Dictate hotkey suffix"), {
      target: { value: "d" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          dictateHotkey: "ctrl+win+d",
          hotkey: "ctrl+win+d",
        }),
      ),
    );
  });

  it("reactivates a reset mode from the General master toggle", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      assistHotkey: "",
      modeEnabled: {
        ...baseSettings.modeEnabled,
        assist: false,
      },
      availableModes: {
        ...baseSettings.availableModes,
        assist: false,
      },
    });

    render(<SettingsApp initialTab="assist" />);

    await screen.findByText("Startup");
    expect(
      screen.getByRole("button", { name: "Assist Mode" }),
    ).toBeDisabled();

    fireEvent.click(screen.getByRole("switch", { name: "Assist Mode" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistHotkey: "win+alt",
          modeEnabled: expect.objectContaining({
            assist: true,
          }),
          availableModes: expect.objectContaining({
            assist: true,
          }),
        }),
      ),
    );
    expect(
      screen.getByRole("button", { name: "Assist Mode" }),
    ).not.toBeDisabled();
  });

  it("saves per-mode hotkey trigger behavior independently", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Assist Mode" }));
    await screen.findByText("Assist hotkey");

    fireEvent.click(
      screen.getByRole("button", { name: "Assist hotkey Toggle on press" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistHotkeyBehavior: "toggle",
          dictateHotkeyBehavior: "push_to_talk",
          voiceAgentHotkeyBehavior: "push_to_talk",
        }),
      ),
    );
  });

  it("saves the voice agent close behavior independently", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp initialTab="realtime_voice" />);

    fireEvent.click(await screen.findByRole("button", { name: "End chat on close" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentCloseBehavior: "new_chat",
        }),
      ),
    );
  });

  it("renders only the personal refinement prompt and saves it independently", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      voiceAgentRefinementPrompt: "Address the user by first name.",
    });

    render(<SettingsApp initialTab="realtime_voice" />);

    expect(
      screen.queryByLabelText("Voice Agent framework prompt"),
    ).not.toBeInTheDocument();

    const refinementPrompt = await screen.findByLabelText(
      "Voice Agent personal refinement prompt",
    );

    expect(refinementPrompt).toHaveValue("Address the user by first name.");

    fireEvent.change(refinementPrompt, {
      target: { value: "Keep answers concise and warm." },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentRefinementPrompt: "Keep answers concise and warm.",
        }),
      ),
    );
  });

  it("saves the selected voice agent profile", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp initialTab="realtime_voice" />);

    const agentProfile = await screen.findByLabelText(
      "Voice Agent agent profile",
    );
    expect(agentProfile).toHaveValue("default");

    fireEvent.change(agentProfile, {
      target: { value: "brainstorming_companion" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentProfileId: "brainstorming_companion",
        }),
      ),
    );
  });

  it("saves the voice agent session summary setting", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp initialTab="realtime_voice" />);

    fireEvent.click(
      await screen.findByRole("switch", { name: "Session summary" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentSessionSummary: false,
        }),
      ),
    );
  });

  it("saves the app auto-start setting from general settings", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp initialTab="general" />);

    fireEvent.click(
      await screen.findByRole("switch", { name: "Auto-start on app launch" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          autoStartOnLaunch: true,
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Voice Agent Mode" }));
    expect(
      screen.queryByRole("switch", { name: "Auto-start on app launch" }),
    ).not.toBeInTheDocument();
  });

  it("reloads settings and model profiles when the dashboard is shown again", async () => {
    fetchSettingsStateMock.mockResolvedValueOnce(baseSettings);
    fetchSettingsStateMock.mockResolvedValueOnce({
      ...baseSettings,
      dictateHotkey: "win+alt+d",
    });
    fetchModelProfilesMock.mockResolvedValue([]);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));
    await screen.findByText("Dictate hotkey");
    expect(fetchSettingsStateMock).toHaveBeenCalledTimes(1);
    expect(fetchModelProfilesMock).toHaveBeenCalledTimes(1);

    window.dispatchEvent(new CustomEvent("speechkit:dashboard-show"));

    await waitFor(() =>
      expect(fetchSettingsStateMock).toHaveBeenCalledTimes(2),
    );
    await waitFor(() =>
      expect(fetchModelProfilesMock).toHaveBeenCalledTimes(2),
    );
    expect(await screen.findByLabelText("Dictate hotkey suffix")).toHaveValue(
      "d",
    );
  });

  it("renders model cards from settings state when the profile fetch fails", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          executionMode: "hf_routed",
          description: "Managed STT profile",
        },
      ],
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
      },
    });
    fetchModelProfilesMock.mockRejectedValue(new Error("profiles unavailable"));

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    expect(
      (await screen.findAllByText("Whisper Large v3 (Hugging Face)")).length,
    ).toBeGreaterThan(0);
  });

  it("blocks duplicate two-key bases across the three modes", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);
    saveSettingsStateMock.mockRejectedValueOnce(
      new Error("Each mode needs its own two-key base."),
    );

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Assist Mode" }));
    await screen.findByText("Assist hotkey");
    fireEvent.click(
      screen.getByRole("button", { name: "Assist hotkey Win + Alt" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistHotkey: "win+alt",
        }),
      ),
    );
    expect(
      await screen.findByText("Each mode needs its own two-key base."),
    ).toBeInTheDocument();
  });

  it("shows the selected microphone from the desktop selector", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    const micButton = await screen.findByRole("button", {
      name: /microphone studio mic/i,
    });
    expect(micButton).toBeInTheDocument();
  });

  it("saves vocabulary dictionary changes through the list editor", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      vocabularyDictionary: "kombi fire => Kombify",
    });

    render(<SettingsApp initialTab="stt" />);

    fireEvent.click(await screen.findByRole("button", { name: "Add word" }));
    fireEvent.change(screen.getByLabelText("Dictionary spoken word 2"), {
      target: { value: "AcmeOS" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save dictionary" }));

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          vocabularyDictionary: "kombi fire => Kombify\nAcmeOS",
        }),
      ),
    );
  });

  it("exposes the user dictionary in Transcribe settings", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      vocabularyDictionary: "kombi fire => Kombify\nAcmeOS\nGemma",
    });

    render(<SettingsApp initialTab="stt" />);

    await screen.findByText("Recent words");
    expect(screen.getByText("Kombify")).toBeInTheDocument();
    expect(screen.getByText("AcmeOS")).toBeInTheDocument();
    expect(screen.getByText("Gemma")).toBeInTheDocument();
  });

  it("saves overlay design changes", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(
      <SettingsApp
        features={{
          overlayFocusOption: false,
          overlayKombifyDesignOption: true,
        }}
      />,
    );

    const button = await screen.findByRole("button", { name: "kombify" });

    fireEvent.click(button);

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({ design: "kombify" }),
      ),
    );
  });

  it("saves separate assist and voice agent overlay feedback modes", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      assistOverlayMode: "small_feedback",
      voiceAgentOverlayMode: "big_productivity",
    });

    render(<SettingsApp />);

    await screen.findByText("Startup");
    expect(
      screen.queryByRole("button", { name: "Assist Mode Big Productivity" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: "Voice Agent Mode Small Feedback",
      }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Assist Mode" }));
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Overlay feedback Big Productivity",
      }),
    );
    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          assistOverlayMode: "big_productivity",
          voiceAgentOverlayMode: "big_productivity",
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Voice Agent Mode" }));
    await screen.findByText("Voice Agent hotkey");
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Overlay feedback Small Feedback",
      }),
    );
    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          voiceAgentOverlayMode: "small_feedback",
        }),
      ),
    );
  });

  it("allows enabling movable overlay while keeping the position chips", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    render(<SettingsApp />);

    fireEvent.click(
      await screen.findByRole("switch", { name: "Movable overlay" }),
    );

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          overlayMovable: true,
          overlayPosition: "top",
        }),
      ),
    );

    expect(
      screen.getByText(/drag the center bubble inside the pill panel/i),
    ).toBeInTheDocument();
  });

  it("saves the current live overlay spot from the runtime state", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      overlayMovable: true,
    });

    render(<SettingsApp />);

    expect(
      await screen.findByRole("button", { name: "Save current spot" }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save current spot" }));

    await waitFor(() => expect(fetchOverlayStateMock).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          overlayMovable: true,
          overlayFreeX: 884,
          overlayFreeY: 412,
        }),
      ),
    );
  });

  it("resets the saved overlay spot through the dedicated action", async () => {
    fetchSettingsStateMock
      .mockResolvedValueOnce({
        ...baseSettings,
        overlayMovable: true,
        overlayFreeX: 884,
        overlayFreeY: 412,
      })
      .mockResolvedValueOnce({
        ...baseSettings,
        overlayMovable: true,
        overlayFreeX: 0,
        overlayFreeY: 0,
      });

    render(<SettingsApp />);

    const button = await screen.findByRole("button", {
      name: "Reset saved spot",
    });
    expect(button).toBeEnabled();

    fireEvent.click(button);

    await waitFor(() =>
      expect(resetOverlayPositionMock).toHaveBeenCalledTimes(1),
    );
    await waitFor(() =>
      expect(fetchSettingsStateMock).toHaveBeenCalledTimes(2),
    );
  });

  it("allows switching between available local whisper downloads", async () => {
    const initialSettings: SpeechKitSettingsState = {
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whispercpp",
          modality: "stt",
          name: "Whisper.cpp (Local Built-in)",
          executionMode: "local",
          description: "SpeechKit-managed local transcription runtime.",
        },
      ],
      activeProfiles: { stt: "stt.local.whispercpp" },
    };
    fetchSettingsStateMock
      .mockResolvedValueOnce(initialSettings)
      .mockResolvedValueOnce(initialSettings);
    fetchDownloadCatalogMock
      .mockResolvedValueOnce([
        {
          id: "whisper.ggml-small",
          profileId: "stt.local.whispercpp",
          name: "Whisper Small Multilingual (466 MB)",
          description: "Small local model",
          sizeLabel: "466 MB",
          sizeBytes: 484264096,
          kind: "http",
          license: "mit",
          available: true,
          selected: true,
        },
        {
          id: "whisper.ggml-large-v3-turbo",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 Turbo",
          description: "Turbo local model",
          sizeLabel: "~1.6 GB",
          sizeBytes: 1624555275,
          kind: "http",
          license: "mit",
          available: true,
          recommended: true,
          selected: false,
        },
        {
          id: "whisper.ggml-large-v3",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 (Open Source)",
          description: "Large local model",
          sizeLabel: "~3.1 GB",
          sizeBytes: 3100000000,
          kind: "http",
          license: "mit",
          available: true,
          selected: false,
        },
      ])
      .mockResolvedValueOnce([
        {
          id: "whisper.ggml-small",
          profileId: "stt.local.whispercpp",
          name: "Whisper Small Multilingual (466 MB)",
          description: "Small local model",
          sizeLabel: "466 MB",
          sizeBytes: 484264096,
          kind: "http",
          license: "mit",
          available: true,
          selected: false,
        },
        {
          id: "whisper.ggml-large-v3-turbo",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 Turbo",
          description: "Turbo local model",
          sizeLabel: "~1.6 GB",
          sizeBytes: 1624555275,
          kind: "http",
          license: "mit",
          available: true,
          recommended: true,
          selected: true,
        },
        {
          id: "whisper.ggml-large-v3",
          profileId: "stt.local.whispercpp",
          name: "Whisper Large v3 (Open Source)",
          description: "Large local model",
          sizeLabel: "~3.1 GB",
          sizeBytes: 3100000000,
          kind: "http",
          license: "mit",
          available: true,
          selected: false,
        },
      ]);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    const switchButton = await screen.findByRole("button", {
      name: "Use Whisper Large v3 Turbo",
    });
    fireEvent.click(switchButton);

    await waitFor(() =>
      expect(selectDownloadedModelMock).toHaveBeenCalledWith(
        "whisper.ggml-large-v3-turbo",
      ),
    );
    expect(
      await screen.findByLabelText("Use Whisper Small Multilingual (466 MB)"),
    ).toBeInTheDocument();
    expect(screen.getByText("Selected on this device")).toBeInTheDocument();
    expect(screen.getByText("recommended")).toBeInTheDocument();
  });

  it("shows Gemma 4 download options for the built-in Assist provider", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.builtin.gemma4-e4b",
          modality: "assist",
          name: "Gemma 4 E4B (Local Built-in)",
          providerKind: "local_built_in",
          executionMode: "local",
          source: "Local Built-in",
          description: "SpeechKit-managed llama.cpp runtime.",
        },
      ],
      activeProfiles: {},
    });
    fetchDownloadCatalogMock.mockResolvedValue([
      {
        id: "llamacpp.gemma-4-e4b-it-q4-k-m",
        profileId: "assist.builtin.gemma4-e4b",
        name: "Gemma 4 E4B IT Q4_K_M (GGUF)",
        description: "Balanced local Assist model",
        sizeLabel: "~5.3 GB",
        sizeBytes: 5335289824,
        kind: "http",
        license: "gemma",
        available: false,
        selected: false,
        recommended: true,
      },
    ]);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Assist Mode" }));

    expect(
      (await screen.findAllByText("Gemma 4 E4B (Local Built-in)")).length,
    ).toBeGreaterThan(0);
    expect(
      screen.getByText("Gemma 4 E4B IT Q4_K_M (GGUF)"),
    ).toBeInTheDocument();
    expect(screen.getByText("recommended")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Download" })).toBeInTheDocument();
  });

  it("shows a runtime-missing warning instead of offering local model switching when whisper runtime is absent", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whispercpp",
          modality: "stt",
          name: "Whisper.cpp (Local Built-in)",
          executionMode: "local",
          description: "SpeechKit-managed local transcription runtime.",
        },
      ],
      activeProfiles: {},
    });
    fetchDownloadCatalogMock.mockResolvedValue([
      {
        id: "whisper.ggml-large-v3-turbo",
        profileId: "stt.local.whispercpp",
        name: "Whisper Large v3 Turbo",
        description: "Turbo local model",
        sizeLabel: "~1.6 GB",
        sizeBytes: 1624555275,
        kind: "http",
        license: "mit",
        available: true,
        selected: false,
        recommended: true,
        runtimeProblem: "Local runtime missing: whisper-server binary missing.",
      },
    ]);

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    expect(
      await screen.findAllByText(
        /local runtime missing: whisper-server binary missing/i,
      ),
    ).toHaveLength(2);
    expect(
      screen.queryByRole("button", { name: "Use Whisper Large v3 Turbo" }),
    ).not.toBeInTheDocument();
  });
});
