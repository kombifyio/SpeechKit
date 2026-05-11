import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ServerConnectionCard } from "@/components/server-connection-card";
import type { ServerConnectionSetting } from "@/lib/speechkit";

const mocks = vi.hoisted(() => ({
  fetchAPIV1ServerConnection: vi.fn(),
  patchAPIV1ServerConnection: vi.fn(),
  testAPIV1ServerConnection: vi.fn(),
}));

vi.mock("@/lib/speechkit", () => ({
  fetchAPIV1ServerConnection: mocks.fetchAPIV1ServerConnection,
  patchAPIV1ServerConnection: mocks.patchAPIV1ServerConnection,
  testAPIV1ServerConnection: mocks.testAPIV1ServerConnection,
}));

const setting: ServerConnectionSetting = {
  enabled: false,
  activeTargetId: "local-dev",
  url: "http://127.0.0.1:8080",
  bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
  authMode: "bearer",
  bearerTokenSet: true,
  fallbackToLocal: true,
  requestTimeoutSec: 30,
  targets: [
    {
      id: "local-dev",
      label: "Local dev",
      url: "http://127.0.0.1:8080",
      authMode: "bearer",
      bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
      bearerTokenSet: true,
      fallbackToLocal: true,
      requestTimeoutSec: 30,
    },
  ],
};

describe("ServerConnectionCard", () => {
  beforeEach(() => {
    mocks.fetchAPIV1ServerConnection.mockReset();
    mocks.patchAPIV1ServerConnection.mockReset();
    mocks.testAPIV1ServerConnection.mockReset();
    mocks.patchAPIV1ServerConnection.mockResolvedValue(setting);
    mocks.testAPIV1ServerConnection.mockResolvedValue({
      ok: true,
      status: "ok",
      message: "healthz and readyz passed",
      healthStatus: 200,
      readyStatus: 200,
    });
  });

  it("renders a registered-server selector without managed server cards", () => {
    render(<ServerConnectionCard serverConnection={setting} />);

    expect(
      screen.getByRole("combobox", { name: /active server/i }),
    ).toHaveValue("local-dev");
    expect(screen.getAllByText("Local dev").length).toBeGreaterThan(0);
    expect(
      screen.queryByRole("button", { name: /kombify Gateway/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /SpeechKit origin/i }),
    ).not.toBeInTheDocument();
  });

  it("smoke-tests and registers a new server target", async () => {
    render(<ServerConnectionCard serverConnection={setting} />);

    fireEvent.click(screen.getByRole("button", { name: /new server/i }));
    fireEvent.change(screen.getByLabelText(/server name/i), {
      target: { value: "Staging" },
    });
    fireEvent.change(screen.getByLabelText(/server url/i), {
      target: { value: "https://staging.speechkit.example.com" },
    });
    fireEvent.change(screen.getByLabelText(/token env var/i), {
      target: { value: "SPEECHKIT_STAGING_TOKEN" },
    });
    fireEvent.change(screen.getByLabelText(/token value/i), {
      target: { value: "secret-token" },
    });

    fireEvent.click(screen.getByRole("button", { name: /test server/i }));

    await waitFor(() => {
      expect(mocks.testAPIV1ServerConnection).toHaveBeenCalledWith(
        expect.objectContaining({
          url: "https://staging.speechkit.example.com",
          bearerTokenEnv: "SPEECHKIT_STAGING_TOKEN",
          token: "secret-token",
          authMode: "bearer",
        }),
      );
    });
    expect(await screen.findByText(/healthz and readyz passed/i)).toBeVisible();

    fireEvent.click(
      screen.getByRole("button", { name: /register and activate/i }),
    );

    await waitFor(() => {
      expect(mocks.patchAPIV1ServerConnection).toHaveBeenCalledWith(
        expect.objectContaining({
          activeTargetId: expect.any(String),
          token: "secret-token",
          targets: expect.arrayContaining([
            expect.objectContaining({ id: "local-dev", label: "Local dev" }),
            expect.objectContaining({
              label: "Staging",
              url: "https://staging.speechkit.example.com",
              bearerTokenEnv: "SPEECHKIT_STAGING_TOKEN",
              authMode: "bearer",
            }),
          ]),
        }),
      );
    });
  });
});
