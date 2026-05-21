// Welcome-step branching tests for the onboarding-target-selection rollout.
// Covers: default Local-selected state, Cloud switch, Get Started → integrations
// (cloud path), server-disclosure expand behavior, and Use this server →
// wake_word (server-path → InstallMode.Cloud).
//
// Mocks the actual server-api exports (`testAPIV1ServerConnection`,
// `patchAPIV1ServerConnection`) and `postInstallMode` from
// `install-mode-api`. Stubs the `@/lib/speechkit` barrel side-effects
// (fetchAudioDevices etc.) so the component can mount in jsdom without
// network access.

import {
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { describe, it, expect, beforeEach, vi } from "vitest";

import type { DownloadItem, DownloadJob } from "@/lib/speechkit";

const {
  testAPIV1ServerConnectionMock,
  patchAPIV1ServerConnectionMock,
  postInstallModeMock,
  fetchAudioDevicesMock,
  fetchWakewordStateMock,
  enableWakewordMock,
  disableWakewordMock,
  saveProviderCredentialMock,
  setAudioDeviceMock,
  updateProviderIntegrationMock,
} = vi.hoisted(() => ({
  testAPIV1ServerConnectionMock: vi.fn(),
  patchAPIV1ServerConnectionMock: vi.fn(),
  postInstallModeMock: vi.fn(),
  fetchAudioDevicesMock: vi.fn(),
  fetchWakewordStateMock: vi.fn(),
  enableWakewordMock: vi.fn(),
  disableWakewordMock: vi.fn(),
  saveProviderCredentialMock: vi.fn(),
  setAudioDeviceMock: vi.fn(),
  updateProviderIntegrationMock: vi.fn(),
}));

vi.mock("@/lib/speechkit", async () => {
  const actual =
    await vi.importActual<typeof import("@/lib/speechkit")>("@/lib/speechkit");
  return {
    ...actual,
    fetchAudioDevices: fetchAudioDevicesMock,
    fetchWakewordState: fetchWakewordStateMock,
    enableWakeword: enableWakewordMock,
    disableWakeword: disableWakewordMock,
    saveProviderCredential: saveProviderCredentialMock,
    setAudioDevice: setAudioDeviceMock,
    updateProviderIntegration: updateProviderIntegrationMock,
  };
});

vi.mock("@/lib/speechkit/server-api", () => ({
  testAPIV1ServerConnection: testAPIV1ServerConnectionMock,
  patchAPIV1ServerConnection: patchAPIV1ServerConnectionMock,
}));

vi.mock("@/lib/speechkit/install-mode-api", () => ({
  postInstallMode: postInstallModeMock,
}));

import { SetupWizard } from "./setup-wizard";

function renderWizard() {
  const onStartDownload = vi.fn<
    (itemId: string) => Promise<DownloadJob>
  >();
  const onCancelDownload = vi.fn<(jobId: string) => Promise<void>>();
  const onSelectDownloadedModel = vi.fn<
    (itemId: string) => Promise<{ message?: string }>
  >();
  const onComplete = vi.fn();
  return render(
    <SetupWizard
      catalog={[] as DownloadItem[]}
      jobs={[] as DownloadJob[]}
      onStartDownload={onStartDownload}
      onCancelDownload={onCancelDownload}
      onSelectDownloadedModel={onSelectDownloadedModel}
      onComplete={onComplete}
    />,
  );
}

describe("SetupWizard welcome step", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fetchAudioDevicesMock.mockResolvedValue({
      devices: [],
      selectedDeviceId: "",
    });
    fetchWakewordStateMock.mockResolvedValue({
      enabled: false,
      listening: false,
      backend: "sherpa_kws",
      phraseId: "hey_quby",
      defaultMode: "voice_agent",
      threshold: 0.5,
      status: "disabled",
    });
    enableWakewordMock.mockResolvedValue({
      enabled: true,
      listening: true,
      backend: "sherpa_kws",
      phraseId: "hey_quby",
      defaultMode: "voice_agent",
      threshold: 0.5,
      status: "listening",
    });
    disableWakewordMock.mockResolvedValue({
      enabled: false,
      listening: false,
      backend: "sherpa_kws",
      phraseId: "hey_quby",
      defaultMode: "voice_agent",
      threshold: 0.5,
      status: "disabled",
    });
    testAPIV1ServerConnectionMock.mockResolvedValue({
      ok: true,
      status: "ready",
      message: "ok",
      healthStatus: 200,
      readyStatus: 200,
    });
    patchAPIV1ServerConnectionMock.mockResolvedValue(undefined);
    postInstallModeMock.mockResolvedValue(undefined);
  });

  it("renders Local card selected by default", () => {
    renderWizard();
    const local = screen.getByRole("radio", { name: /^Local(?:Selected|\s)/i });
    expect(local).toHaveAttribute("aria-checked", "true");
    const cloud = screen.getByRole("radio", { name: /^Cloud(?:Selected|\s)/i });
    expect(cloud).toHaveAttribute("aria-checked", "false");
  });

  it("switches selection when Cloud is clicked", () => {
    renderWizard();
    fireEvent.click(
      screen.getByRole("radio", { name: /^Cloud(?:Selected|\s)/i }),
    );
    expect(
      screen.getByRole("radio", { name: /^Cloud(?:Selected|\s)/i }),
    ).toHaveAttribute("aria-checked", "true");
    expect(
      screen.getByRole("radio", { name: /^Local(?:Selected|\s)/i }),
    ).toHaveAttribute("aria-checked", "false");
  });

  it("Get Started routes to integrations when Cloud is selected", async () => {
    renderWizard();
    fireEvent.click(
      screen.getByRole("radio", { name: /^Cloud(?:Selected|\s)/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: /Get Started/i }));
    await waitFor(() =>
      expect(postInstallModeMock).toHaveBeenCalledWith("cloud"),
    );
    // Integrations step renders the "Integrations" heading.
    expect(
      await screen.findByRole("heading", { name: /Integrations/i }),
    ).toBeInTheDocument();
  });

  it("server form stays collapsed until disclosure clicked; Use this server disabled before test passes", () => {
    renderWizard();
    // Form is closed: URL input not present
    expect(
      screen.queryByPlaceholderText("https://speechkit.example.com"),
    ).not.toBeInTheDocument();
    // Click disclosure
    fireEvent.click(
      screen.getByRole("button", { name: /I have my own SpeechKit server/i }),
    );
    expect(
      screen.getByPlaceholderText("https://speechkit.example.com"),
    ).toBeInTheDocument();
    // Use this server is disabled by default
    const useBtn = screen.getByRole("button", { name: /Use this server/i });
    expect(useBtn).toBeDisabled();
  });

  it("Use this server saves and advances to wake_word after a successful test", async () => {
    renderWizard();

    fireEvent.click(
      screen.getByRole("button", { name: /I have my own SpeechKit server/i }),
    );
    fireEvent.change(
      screen.getByPlaceholderText("https://speechkit.example.com"),
      { target: { value: "https://srv.example.com" } },
    );
    fireEvent.click(screen.getByRole("button", { name: /Test connection/i }));
    await waitFor(() =>
      expect(testAPIV1ServerConnectionMock).toHaveBeenCalled(),
    );

    // After test passes, "Use this server" becomes enabled.
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: /Use this server/i }),
      ).not.toBeDisabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: /Use this server/i }));
    await waitFor(() =>
      expect(patchAPIV1ServerConnectionMock).toHaveBeenCalled(),
    );
    await waitFor(() =>
      expect(postInstallModeMock).toHaveBeenCalledWith("server"),
    );

    // Verify the production handler called patch BEFORE postInstallMode.
    // Without this, a regression that reverses the order would still pass
    // the two waitFor calls above.
    const patchOrder =
      patchAPIV1ServerConnectionMock.mock.invocationCallOrder[0];
    const postOrder = postInstallModeMock.mock.invocationCallOrder[0];
    expect(patchOrder).toBeLessThan(postOrder);

    // Wake-word step header.
    expect(
      await screen.findByRole("heading", {
        name: /Wake word.*one click to enable/i,
      }),
    ).toBeInTheDocument();
  });
});
