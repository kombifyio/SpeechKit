import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { VoiceAgentProfileStep } from "./voice-agent-profile-step";

describe("VoiceAgentProfileStep", () => {
  let onNext: () => void;
  let onBack: () => void;
  let fetchMock: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    onNext = vi.fn<() => void>();
    onBack = vi.fn<() => void>();
    fetchMock = vi.spyOn(globalThis, "fetch");
  });

  afterEach(() => {
    fetchMock.mockRestore();
  });

  it("auto-applies on-prem profile when target is local", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ applied: true, profile: "on-prem" }),
      text: async () => "",
    } as Response);

    render(
      <VoiceAgentProfileStep
        target="local"
        onNext={onNext}
        onBack={onBack}
      />,
    );

    expect(
      screen.getByText(/On-Prem \(Local Cascaded\)/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/BYOK Cloud \(Gemini Live\)/i),
    ).not.toBeInTheDocument();

    await waitFor(() =>
      expect(
        screen.getByTestId("voice-agent-profile-applied"),
      ).toBeInTheDocument(),
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/voice-agent/apply-profile",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ profile: "on-prem" }),
      }),
    );
  });

  it("auto-applies cloud-byok profile when target is cloud", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ applied: true, profile: "cloud-byok" }),
      text: async () => "",
    } as Response);

    render(
      <VoiceAgentProfileStep
        target="cloud"
        onNext={onNext}
        onBack={onBack}
      />,
    );

    expect(
      screen.getByText(/BYOK Cloud \(Gemini Live\)/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/On-Prem \(Local Cascaded\)/i),
    ).not.toBeInTheDocument();

    await waitFor(() =>
      expect(
        screen.getByTestId("voice-agent-profile-applied"),
      ).toBeInTheDocument(),
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/voice-agent/apply-profile",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ profile: "cloud-byok" }),
      }),
    );
  });

  it("disables Continue until apply succeeds", async () => {
    let resolveFetch: ((value: Response) => void) | null = null;
    fetchMock.mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          resolveFetch = resolve;
        }),
    );

    render(
      <VoiceAgentProfileStep
        target="local"
        onNext={onNext}
        onBack={onBack}
      />,
    );

    const continueBtn = screen.getByRole("button", { name: /Continue/i });
    expect(continueBtn).toBeDisabled();
    expect(
      screen.getByTestId("voice-agent-profile-applying"),
    ).toBeInTheDocument();

    // Resolve the pending fetch as success
    resolveFetch!({
      ok: true,
      json: async () => ({ applied: true }),
      text: async () => "",
    } as Response);

    await waitFor(() => expect(continueBtn).not.toBeDisabled());
  });

  it("Continue advances after successful auto-apply", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ applied: true }),
      text: async () => "",
    } as Response);

    render(
      <VoiceAgentProfileStep
        target="local"
        onNext={onNext}
        onBack={onBack}
      />,
    );

    await waitFor(() =>
      expect(
        screen.getByTestId("voice-agent-profile-applied"),
      ).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByRole("button", { name: /Continue/i }));
    expect(onNext).toHaveBeenCalled();
  });

  it("shows error and offers retry on apply failure", async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 500,
      text: async () => "internal error",
    } as Response);

    render(
      <VoiceAgentProfileStep
        target="cloud"
        onNext={onNext}
        onBack={onBack}
      />,
    );

    await waitFor(() =>
      expect(
        screen.getByTestId("voice-agent-profile-error"),
      ).toBeInTheDocument(),
    );
    expect(onNext).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: /Continue/i })).toBeDisabled();

    // Retry button is visible while in error state
    const retryBtn = screen.getByRole("button", { name: /Retry/i });
    expect(retryBtn).toBeInTheDocument();

    fetchMock.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ applied: true }),
      text: async () => "",
    } as Response);
    fireEvent.click(retryBtn);

    await waitFor(() =>
      expect(
        screen.getByTestId("voice-agent-profile-applied"),
      ).toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: /Continue/i })).not.toBeDisabled();
  });

  it("calls onBack when back button is clicked", () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ applied: true }),
      text: async () => "",
    } as Response);

    render(
      <VoiceAgentProfileStep
        target="local"
        onNext={onNext}
        onBack={onBack}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Back/i }));
    expect(onBack).toHaveBeenCalled();
  });
});
