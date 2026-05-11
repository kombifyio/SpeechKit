import {
  fetchSettingsStateMock,
  saveSettingsStateMock,
  saveProviderCredentialMock,
  clearProviderCredentialMock,
  updateProviderIntegrationMock,
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

describe("SettingsApp integrations", () => {
  it("shows provider integrations in Integrations settings and toggles them", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      providerIntegrations: {
        ollama: {
          provider: "ollama",
          label: "Ollama",
          enabled: false,
          providerKind: "local_provider",
          integrationKind: "local_provider",
          credentialRequired: false,
          available: true,
          hasStoredSecret: false,
          source: "none",
          setupUrl: "https://ollama.com/download",
          supportedModes: ["voice_agent", "assist", "dictate"],
        },
        groq: {
          provider: "groq",
          label: "Groq",
          enabled: false,
          providerKind: "direct_provider",
          integrationKind: "direct_api",
          credentialRequired: true,
          available: false,
          hasStoredSecret: false,
          source: "none",
          envName: "GROQ_API_KEY",
          setupUrl: "https://console.groq.com/keys",
          supportedModes: ["assist", "dictate"],
        },
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          enabled: false,
          providerKind: "cloud_provider",
          integrationKind: "cloud_gateway",
          credentialRequired: true,
          available: false,
          hasStoredSecret: false,
          source: "none",
          envName: "HF_TOKEN",
          setupUrl: "https://huggingface.co/settings/tokens",
          supportedModes: ["voice_agent", "assist", "dictate"],
        },
        openai: {
          provider: "openai",
          label: "OpenAI",
          enabled: false,
          providerKind: "direct_provider",
          integrationKind: "direct_api",
          credentialRequired: true,
          available: false,
          hasStoredSecret: false,
          source: "none",
          envName: "OPENAI_API_KEY",
          setupUrl: "https://platform.openai.com/api-keys",
          supportedModes: ["assist", "dictate"],
        },
        google: {
          provider: "google",
          label: "Gemini / Google AI",
          enabled: true,
          providerKind: "direct_provider",
          integrationKind: "direct_api",
          credentialRequired: true,
          available: false,
          hasStoredSecret: false,
          source: "none",
          envName: "GOOGLE_AI_API_KEY",
          setupUrl: "https://aistudio.google.com/apikey",
          supportedModes: ["voice_agent", "assist", "dictate"],
        },
        openrouter: {
          provider: "openrouter",
          label: "OpenRouter",
          enabled: false,
          providerKind: "cloud_provider",
          integrationKind: "cloud_gateway",
          credentialRequired: true,
          available: false,
          hasStoredSecret: false,
          source: "none",
          envName: "OPENROUTER_API_KEY",
          setupUrl: "https://openrouter.ai/settings/keys",
          supportedModes: ["dictate", "assist", "voice_agent"],
        },
      },
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Integrations" }));

    const cards = screen.getAllByTestId("settings-integration-card");
    expect(
      cards.map((card) =>
        within(card).getByTestId("provider-brand-name").textContent,
      ),
    ).toEqual([
      "Hugging Face",
      "OpenRouter",
      "OpenAI",
      "Gemini / Google AI",
      "Groq",
      "Ollama",
    ]);
    expect(screen.getAllByText("Cloud Router / Gateway").length).toBeGreaterThan(1);
    expect(
      within(cards[3])
        .getAllByTestId("integration-mode-tag")
        .map((tag) => tag.textContent),
    ).toEqual(["Dictation", "Assist", "Voice Agent"]);
    expect(within(cards[1]).getByAltText("OpenRouter logo")).toHaveAttribute(
      "src",
      "/integrations/openrouter.svg",
    );
    expect(
      within(cards[1])
        .getAllByTestId("integration-mode-tag")
        .map((tag) => tag.textContent),
    ).toEqual(["Dictation", "Assist", "Voice Agent"]);
    const openRouterLink = within(cards[1]).getByRole("link", {
      name: /create api key/i,
    });
    expect(openRouterLink).toHaveAttribute(
      "href",
      "https://openrouter.ai/settings/keys",
    );
    expect(openRouterLink).toHaveAttribute(
      "title",
      "https://openrouter.ai/settings/keys",
    );
    expect(
      within(cards[1]).getByText("openrouter.ai/settings/keys"),
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("switch", { name: "Enable OpenRouter integration" }),
    );

    await waitFor(() =>
      expect(updateProviderIntegrationMock).toHaveBeenCalledWith(
        "openrouter",
        true,
      ),
    );
  });

  it("hides disabled integration profiles but shows enabled missing-key profiles", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whispercpp",
          modality: "stt",
          name: "Whisper.cpp (Local Built-in)",
          providerKind: "local_built_in",
          executionMode: "local",
        },
        {
          id: "stt.openai.whisper-1",
          modality: "stt",
          name: "Whisper-1 (OpenAI)",
          providerKind: "direct_provider",
          executionMode: "openai_api",
        },
        {
          id: "stt.google.chirp-3",
          modality: "stt",
          name: "Chirp 3 (Google)",
          providerKind: "direct_provider",
          executionMode: "google_api",
        },
      ],
      activeProfiles: { stt: "stt.local.whispercpp" },
      modelSelections: {
        ...baseSettings.modelSelections,
        dictate: {
          primaryProfileId: "stt.local.whispercpp",
          fallbackProfileId: "",
        },
      },
      providerIntegrations: {
        openai: {
          provider: "openai",
          label: "OpenAI",
          enabled: false,
          providerKind: "direct_provider",
          integrationKind: "direct_api",
          credentialRequired: true,
          available: true,
          hasStoredSecret: true,
          source: "user",
          envName: "OPENAI_API_KEY",
          setupUrl: "https://platform.openai.com/api-keys",
          supportedModes: ["dictate", "assist"],
        },
        google: {
          provider: "google",
          label: "Gemini / Google AI",
          enabled: true,
          providerKind: "direct_provider",
          integrationKind: "direct_api",
          credentialRequired: true,
          available: false,
          hasStoredSecret: false,
          source: "none",
          envName: "GOOGLE_AI_API_KEY",
          setupUrl: "https://aistudio.google.com/apikey",
          supportedModes: ["dictate", "assist", "voice_agent"],
        },
      },
      providerCredentials: {
        openai: {
          provider: "openai",
          label: "OpenAI",
          envName: "OPENAI_API_KEY",
          available: true,
          hasStoredSecret: true,
          source: "user",
        },
        google: {
          provider: "google",
          label: "Gemini / Google AI",
          envName: "GOOGLE_AI_API_KEY",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp initialTab="stt" />);

    expect(
      (await screen.findAllByText("Whisper.cpp (Local Built-in)")).length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText("Whisper-1 (OpenAI)")).not.toBeInTheDocument();
    expect(screen.getAllByText("Chirp 3 (Google)").length).toBeGreaterThan(0);
    expect(screen.getByText("Add Gemini / Google AI key")).toBeInTheDocument();
  });

  it("shows stored token status for the provider", async () => {
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
          source: "install",
        },
      },
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    expect(
      await screen.findByText(/install token active/i),
    ).toBeInTheDocument();
  });

  it("shows every model card per modality while keeping routing options visible", async () => {
    fetchSettingsStateMock.mockResolvedValue(baseSettings);

    const assistProfiles = Array.from({ length: 6 }, (_, index) => ({
      id: `assist-model-${index + 1}`,
      modality: "assist" as const,
      name: `Assist Model ${index + 1}`,
      executionMode: "groq_api" as const,
      description: `Assist profile ${index + 1}`,
    }));

    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: assistProfiles,
      activeProfiles: { assist: "assist-model-6" },
      providerCredentials: {
        groq: {
          provider: "groq",
          label: "Groq",
          envName: "GROQ_API_KEY",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp />);

    const assistNavButtons = await screen.findAllByRole("button", {
      name: "Assist Mode",
    });
    fireEvent.click(assistNavButtons[0]);

    const directProviderGroup = await screen.findByTestId(
      "model-provider-group-direct_provider",
    );
    for (const profile of assistProfiles) {
      expect(
        within(directProviderGroup).getByText(profile.name),
      ).toBeInTheDocument();
      expect(screen.getAllByText(profile.name)).toHaveLength(3);
    }
  });

  it("shows inline key entry on the model card when a provider key is missing", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.groq.llama-3.1-8b",
          modality: "assist",
          name: "LLaMA 3.1 8B (Groq)",
          executionMode: "groq_api",
          description: "Fast cloud LLM",
        },
      ],
      activeProfiles: {},
      providerCredentials: {
        groq: {
          provider: "groq",
          label: "Groq",
          envName: "GROQ_API_KEY",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp />);

    const assistNavButtons = await screen.findAllByRole("button", {
      name: "Assist Mode",
    });
    fireEvent.click(assistNavButtons[0]);

    const keyInput = await screen.findByLabelText(
      "LLaMA 3.1 8B (Groq) Groq key",
    );
    expect(keyInput).toBeInTheDocument();

    fireEvent.change(keyInput, {
      target: { value: "groq_user_token_123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save key" }));

    await waitFor(() =>
      expect(saveProviderCredentialMock).toHaveBeenCalledWith(
        "groq",
        "groq_user_token_123",
      ),
    );
  });

  it("keeps Hugging Face token setup directly on the model card", async () => {
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
      activeProfiles: {},
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    expect(
      (await screen.findAllByText("Whisper Large v3 (Hugging Face)")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("Add Hugging Face token")).toBeInTheDocument();
    expect(
      screen.getByLabelText(
        "Whisper Large v3 (Hugging Face) Hugging Face token",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Save token" }),
    ).toBeInTheDocument();
  });

  it("keeps Hugging Face profiles selectable even while the token is missing", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "stt.local.whispercpp",
          modality: "stt",
          name: "Whisper.cpp (Local Built-in)",
          providerKind: "local_built_in",
          executionMode: "local",
        },
        {
          id: "stt.routed.whisper-large-v3",
          modality: "stt",
          name: "Whisper Large v3 (Hugging Face)",
          executionMode: "hf_routed",
          description: "Managed STT profile",
        },
      ],
      activeProfiles: { stt: "stt.local.whispercpp" },
      modelSelections: {
        ...baseSettings.modelSelections,
        dictate: {
          primaryProfileId: "stt.local.whispercpp",
          fallbackProfileId: "",
        },
      },
      providerIntegrations: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          enabled: true,
          providerKind: "cloud_provider",
          integrationKind: "cloud_gateway",
          credentialRequired: true,
          available: false,
          hasStoredSecret: false,
          source: "none",
          envName: "HF_TOKEN",
          setupUrl: "https://huggingface.co/settings/tokens",
          supportedModes: ["dictate", "assist", "voice_agent"],
        },
      },
      providerCredentials: {
        huggingface: {
          provider: "huggingface",
          label: "Hugging Face",
          envName: "HF_TOKEN",
          available: false,
          hasStoredSecret: false,
          source: "none",
        },
      },
    });

    render(<SettingsApp initialTab="stt" />);

    const primaryModel = await screen.findByLabelText("Primary model");
    const hfOption = within(primaryModel).getByRole("option", {
      name: "Whisper Large v3 (Hugging Face)",
    });
    expect(hfOption).not.toBeDisabled();
    expect(screen.getByText("Add Hugging Face token")).toBeInTheDocument();

    fireEvent.change(primaryModel, {
      target: { value: "stt.routed.whisper-large-v3" },
    });

    await waitFor(() =>
      expect(saveSettingsStateMock).toHaveBeenCalledWith(
        expect.objectContaining({
          modelSelections: expect.objectContaining({
            dictate: {
              primaryProfileId: "stt.routed.whisper-large-v3",
              fallbackProfileId: "",
            },
          }),
        }),
      ),
    );
  });

  it("keeps Hugging Face-backed assist models selectable from the Assist tab", async () => {
    fetchSettingsStateMock.mockResolvedValue({
      ...baseSettings,
      profiles: [
        {
          id: "assist.hf.qwen3-coder",
          modality: "assist",
          name: "Qwen 3 Coder (Hugging Face)",
          executionMode: "hf_inference",
          description: "Managed coding assistant profile",
        },
      ],
      activeProfiles: {},
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

    render(<SettingsApp />);

    const assistNavButtons = await screen.findAllByRole("button", {
      name: "Assist Mode",
    });
    fireEvent.click(assistNavButtons[0]);

    expect(
      (await screen.findAllByText("Qwen 3 Coder (Hugging Face)")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText(/user token active/i)).toBeInTheDocument();
    expect(screen.getByLabelText("Assist LLM")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Use model" }),
    ).not.toBeInTheDocument();
  });

  it("does not render the duplicated connected providers section", async () => {
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

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));

    expect(
      (await screen.findAllByText("Whisper Large v3 (Hugging Face)")).length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText("Connected providers")).not.toBeInTheDocument();
  });

  it("clears a stored provider credential explicitly", async () => {
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

    render(<SettingsApp />);

    fireEvent.click(await screen.findByRole("button", { name: "Transcribe Mode" }));
    fireEvent.click(await screen.findByRole("button", { name: "Clear" }));

    await waitFor(() =>
      expect(clearProviderCredentialMock).toHaveBeenCalledWith("huggingface"),
    );
  });
});
