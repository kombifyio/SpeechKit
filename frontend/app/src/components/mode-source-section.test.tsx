import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ModeSource, ServerConnectionSetting } from "@/lib/speechkit";

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

const serverConnection: ServerConnectionSetting = {
  enabled: false,
  url: "https://speechkit.example.com",
  bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
  authMode: "bearer",
  // bearerTokenSet=true represents a fully-ready connection (URL + token).
  // The frontend now blocks clicks on the Server pill when readiness != ready,
  // so these tests need a ready fixture; readiness=not-configured/token-missing
  // is covered by the dedicated "blocks ... without prompting the backend"
  // test below.
  bearerTokenSet: true,
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

  it("blocks the Server pill with an actionable hint when the connection is not configured", async () => {
    const notConfigured: ServerConnectionSetting = {
      ...serverConnection,
      url: "",
      bearerTokenSet: true,
      targets: [],
    };
    render(<ModeSourceSection serverConnection={notConfigured} />);

    const dictationGroup = await screen.findByRole("radiogroup", {
      name: "Dictation mode source",
    });
    const serverPill = within(dictationGroup).getByRole("radio", {
      name: /server/i,
    });
    expect(serverPill).toBeDisabled();
    fireEvent.click(serverPill);

    expect(mocks.patchAPIV1ServerConnection).not.toHaveBeenCalled();
    expect(mocks.patchAPIV1ModeSettings).not.toHaveBeenCalled();
    expect(
      screen.getAllByText(/Register a server target/i).length,
    ).toBeGreaterThan(0);
  });

  it("blocks the global Server radio when token is missing and names the env var", async () => {
    const tokenMissing: ServerConnectionSetting = {
      ...serverConnection,
      bearerTokenSet: false,
      bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
    };
    render(<ModeSourceSection serverConnection={tokenMissing} />);

    const globalGroup = await screen.findByRole("radiogroup", {
      name: "Global mode source",
    });
    const serverPill = within(globalGroup).getByRole("radio", {
      name: /server/i,
    });
    expect(serverPill).toBeDisabled();
  });

  it("applies global mode source updates sequentially", async () => {
    const resolvers: Array<() => void> = [];
    mocks.patchAPIV1ModeSettings.mockImplementation(
      async (_mode: string, patch: { modeSource?: ModeSource }) =>
        new Promise((resolve) => {
          resolvers.push(() =>
            resolve({ enabled: true, modeSource: patch.modeSource }),
          );
        }),
    );

    render(<ModeSourceSection serverConnection={{ ...serverConnection, enabled: true }} />);

    const globalGroup = await screen.findByRole("radiogroup", {
      name: "Global mode source",
    });
    fireEvent.click(within(globalGroup).getByRole("radio", { name: /server/i }));

    await waitFor(() => {
      expect(mocks.patchAPIV1ModeSettings).toHaveBeenCalledTimes(1);
    });
    expect(mocks.patchAPIV1ModeSettings).toHaveBeenNthCalledWith(1, "dictation", {
      modeSource: "server",
    });

    resolvers[0]();
    await waitFor(() => {
      expect(mocks.patchAPIV1ModeSettings).toHaveBeenCalledTimes(2);
    });
    expect(mocks.patchAPIV1ModeSettings).toHaveBeenNthCalledWith(2, "assist", {
      modeSource: "server",
    });

    resolvers[1]();
    await waitFor(() => {
      expect(mocks.patchAPIV1ModeSettings).toHaveBeenCalledTimes(3);
    });
    expect(mocks.patchAPIV1ModeSettings).toHaveBeenNthCalledWith(
      3,
      "voice_agent",
      {
        modeSource: "server",
      },
    );
    resolvers[2]();
  });
});
