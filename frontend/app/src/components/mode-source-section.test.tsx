import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ModeSource } from "@/lib/speechkit";

const mocks = vi.hoisted(() => ({
  fetchAPIV1Modes: vi.fn(),
  fetchAPIV1ServerConnection: vi.fn(),
  patchAPIV1ModeSettings: vi.fn(),
  patchAPIV1ServerConnection: vi.fn(),
}));

vi.mock("@/lib/speechkit", () => ({
  fetchAPIV1Modes: mocks.fetchAPIV1Modes,
  fetchAPIV1ServerConnection: mocks.fetchAPIV1ServerConnection,
  patchAPIV1ModeSettings: mocks.patchAPIV1ModeSettings,
  patchAPIV1ServerConnection: mocks.patchAPIV1ServerConnection,
}));

import { ModeSourceSection } from "@/components/mode-source-section";

const serverConnection = {
  enabled: false,
  url: "https://speechkit.example.com",
  bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
  bearerTokenSet: false,
  fallbackToLocal: true,
  requestTimeoutSec: 30,
};

function modes(modeSource: ModeSource = "local") {
  return {
    contracts: [],
    settings: {
      dictation: { enabled: true, modeSource, dictionaryEnabled: false },
      assist: { enabled: true, modeSource: "local", ttsEnabled: true },
      voiceAgent: {
        enabled: true,
        modeSource: "local",
        sessionSummary: true,
        pipelineFallback: false,
      },
      serverConnection,
    },
  };
}

describe("ModeSourceSection", () => {
  beforeEach(() => {
    mocks.fetchAPIV1Modes.mockReset();
    mocks.fetchAPIV1ServerConnection.mockReset();
    mocks.patchAPIV1ModeSettings.mockReset();
    mocks.patchAPIV1ServerConnection.mockReset();

    mocks.fetchAPIV1Modes.mockResolvedValue(modes());
    mocks.fetchAPIV1ServerConnection.mockResolvedValue(serverConnection);
    mocks.patchAPIV1ServerConnection.mockResolvedValue({
      ...serverConnection,
      enabled: true,
    });
  });

  it("clears a stale server error after a successful local switch", async () => {
    mocks.patchAPIV1ModeSettings.mockImplementation(
      async (_mode: string, patch: { modeSource?: ModeSource }) => {
        if (patch.modeSource === "server") {
          throw new Error(
            'serverclient: env var "SPEECHKIT_SERVER_TOKEN" is empty; cannot authenticate',
          );
        }
        return { enabled: true, modeSource: "local" };
      },
    );

    render(<ModeSourceSection />);

    const dictationGroup = await screen.findByRole("radiogroup", {
      name: "Dictation mode source",
    });

    fireEvent.click(within(dictationGroup).getByRole("radio", { name: /server/i }));
    expect(await screen.findByText(/SPEECHKIT_SERVER_TOKEN/)).toBeInTheDocument();

    fireEvent.click(within(dictationGroup).getByRole("radio", { name: /local/i }));

    await waitFor(() => {
      expect(screen.queryByText(/SPEECHKIT_SERVER_TOKEN/)).not.toBeInTheDocument();
    });
  });

  it("notifies the parent when enabling server mode updates shared connection state", async () => {
    mocks.patchAPIV1ModeSettings.mockResolvedValue({
      enabled: true,
      modeSource: "server",
    });
    const onServerConnectionChange = vi.fn();

    render(
      <ModeSourceSection
        serverConnection={serverConnection}
        onServerConnectionChange={onServerConnectionChange}
      />,
    );

    const dictationGroup = await screen.findByRole("radiogroup", {
      name: "Dictation mode source",
    });
    fireEvent.click(within(dictationGroup).getByRole("radio", { name: /server/i }));

    await waitFor(() => {
      expect(mocks.patchAPIV1ServerConnection).toHaveBeenCalledWith({
        enabled: true,
      });
      expect(onServerConnectionChange).toHaveBeenCalledWith({
        ...serverConnection,
        enabled: true,
      });
    });
    expect(mocks.fetchAPIV1ServerConnection).not.toHaveBeenCalled();
  });
});
