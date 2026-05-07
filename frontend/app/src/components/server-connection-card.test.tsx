import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ServerConnectionCard } from "@/components/server-connection-card";
import type { ServerConnectionSetting } from "@/lib/speechkit";

const { fetchServerConnectionMock, patchServerConnectionMock } = vi.hoisted(
  () => ({
    fetchServerConnectionMock: vi.fn<() => Promise<ServerConnectionSetting>>(),
    patchServerConnectionMock:
      vi.fn<(patch: Partial<ServerConnectionSetting>) => Promise<ServerConnectionSetting>>(),
  }),
);

vi.mock("@/lib/speechkit", () => ({
  fetchAPIV1ServerConnection: fetchServerConnectionMock,
  patchAPIV1ServerConnection: patchServerConnectionMock,
}));

const baseConnection: ServerConnectionSetting = {
  enabled: true,
  url: "https://speechkit.example",
  bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
  bearerTokenSet: false,
  fallbackToLocal: true,
  requestTimeoutSec: 30,
};

beforeEach(() => {
  fetchServerConnectionMock.mockReset();
  patchServerConnectionMock.mockReset();
  patchServerConnectionMock.mockImplementation(async (patch) => ({
    ...baseConnection,
    ...patch,
  }));
});

describe("ServerConnectionCard", () => {
  it("loads settings and patches edited server target fields", async () => {
    const onSettingsChange = vi.fn();
    fetchServerConnectionMock.mockResolvedValue(baseConnection);

    render(<ServerConnectionCard onSettingsChange={onSettingsChange} />);

    expect(screen.getByText(/Loading server connection/)).toBeInTheDocument();
    const urlInput = await screen.findByDisplayValue(baseConnection.url);
    expect(screen.getByText("token missing")).toBeInTheDocument();

    fireEvent.change(urlInput, { target: { value: "https://new.example" } });
    expect(urlInput).toHaveValue("https://new.example");
    fireEvent.blur(urlInput);

    await waitFor(() => {
      expect(patchServerConnectionMock).toHaveBeenCalledWith({
        url: "https://new.example",
      });
    });
    expect(onSettingsChange).toHaveBeenCalledWith(
      expect.objectContaining({ url: "https://new.example" }),
    );
  });

  it("patches fallback and timeout controls from a supplied setting", async () => {
    const onSettingsChange = vi.fn();
    render(
      <ServerConnectionCard
        serverConnection={{ ...baseConnection, bearerTokenSet: true }}
        onSettingsChange={onSettingsChange}
      />,
    );

    expect(fetchServerConnectionMock).not.toHaveBeenCalled();
    expect(screen.getByText("token set")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("switch"));
    await waitFor(() => {
      expect(patchServerConnectionMock).toHaveBeenCalledWith({
        fallbackToLocal: false,
      });
    });

    const timeoutInput = screen.getByDisplayValue("30");
    fireEvent.change(timeoutInput, { target: { value: "12" } });
    expect(timeoutInput).toHaveValue(12);
    fireEvent.blur(timeoutInput);

    await waitFor(() => {
      expect(patchServerConnectionMock).toHaveBeenLastCalledWith({
        requestTimeoutSec: 12,
      });
    });
    expect(onSettingsChange).toHaveBeenCalledTimes(2);
  });

  it("shows a deterministic load error when settings cannot be fetched", async () => {
    fetchServerConnectionMock.mockRejectedValue(new Error("offline"));

    render(<ServerConnectionCard />);

    expect(
      await screen.findByText(/Failed to load server connection settings: offline/),
    ).toBeInTheDocument();
  });
});
