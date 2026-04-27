import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ModeSourceToggle } from "@/components/mode-source-toggle";
import type { ServerConnectionSetting } from "@/lib/speechkit";

const baseConn: ServerConnectionSetting = {
  enabled: true,
  url: "http://localhost:8080",
  bearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
  bearerTokenSet: true,
  fallbackToLocal: true,
  requestTimeoutSec: 30,
};

describe("ModeSourceToggle", () => {
  it("calls onChange with 'server' when the server pill is clicked", () => {
    const onChange = vi.fn();
    render(
      <ModeSourceToggle
        modeLabel="Dictation"
        value="local"
        onChange={onChange}
        serverConnection={baseConn}
      />,
    );
    fireEvent.click(screen.getByRole("radio", { name: /server/i }));
    expect(onChange).toHaveBeenCalledWith("server");
  });

  it("disables the server pill and shows hint when ServerConnection is disabled", () => {
    const onChange = vi.fn();
    render(
      <ModeSourceToggle
        modeLabel="Assist"
        value="local"
        onChange={onChange}
        serverConnection={{ ...baseConn, enabled: false }}
      />,
    );
    const serverButton = screen.getByRole("radio", { name: /server/i });
    expect(serverButton).toBeDisabled();
    fireEvent.click(serverButton);
    expect(onChange).not.toHaveBeenCalled();
    expect(
      screen.getByText(/Enable Server Connection/i),
    ).toBeInTheDocument();
  });

  it("disables the server pill when bearer token env var is missing", () => {
    const onChange = vi.fn();
    render(
      <ModeSourceToggle
        modeLabel="Voice Agent"
        value="local"
        onChange={onChange}
        serverConnection={{ ...baseConn, bearerTokenSet: false }}
      />,
    );
    const serverButton = screen.getByRole("radio", { name: /server/i });
    expect(serverButton).toBeDisabled();
    expect(
      screen.getByText(/SPEECHKIT_SERVER_TOKEN/i),
    ).toBeInTheDocument();
  });

  it("treats missing value as 'local' for backwards compatibility", () => {
    render(
      <ModeSourceToggle
        modeLabel="Dictation"
        onChange={vi.fn()}
        serverConnection={baseConn}
      />,
    );
    const localButton = screen.getByRole("radio", { name: /local/i });
    expect(localButton).toHaveAttribute("aria-checked", "true");
  });
});
