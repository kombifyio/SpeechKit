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

  it("renders both profile cards", () => {
    render(<VoiceAgentProfileStep onNext={onNext} onBack={onBack} />);
    expect(screen.getByText(/On-Prem \(Local Cascaded\)/i)).toBeInTheDocument();
    expect(screen.getByText(/BYOK Cloud \(Gemini Live\)/i)).toBeInTheDocument();
  });

  it("disables apply button until a profile is selected", () => {
    render(<VoiceAgentProfileStep onNext={onNext} onBack={onBack} />);
    const applyBtn = screen.getByRole("button", { name: /Apply profile/i });
    expect(applyBtn).toBeDisabled();
  });

  it("applies on-prem profile and calls onNext on success", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ applied: true, profile: "on-prem" }),
      text: async () => "",
    } as Response);

    render(<VoiceAgentProfileStep onNext={onNext} onBack={onBack} />);

    // Select the on-prem card
    fireEvent.click(screen.getByText(/On-Prem.*Local Cascaded/i).closest("button")!);
    // Button label updates
    const applyBtn = screen.getByRole("button", { name: /Apply On-Prem/i });
    expect(applyBtn).not.toBeDisabled();
    fireEvent.click(applyBtn);

    await waitFor(() => expect(onNext).toHaveBeenCalled());
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/voice-agent/apply-profile",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ profile: "on-prem" }),
      }),
    );
  });

  it("shows error and does not advance on apply failure", async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => "internal error",
    } as Response);

    render(<VoiceAgentProfileStep onNext={onNext} onBack={onBack} />);
    // Select cloud-byok
    fireEvent.click(screen.getByText(/BYOK Cloud.*Gemini Live/i).closest("button")!);
    fireEvent.click(screen.getByRole("button", { name: /Apply BYOK Cloud/i }));

    await waitFor(() =>
      expect(screen.getByText(/Apply failed: 500/)).toBeInTheDocument(),
    );
    expect(onNext).not.toHaveBeenCalled();
  });

  it("calls onBack when back button is clicked", () => {
    render(<VoiceAgentProfileStep onNext={onNext} onBack={onBack} />);
    fireEvent.click(screen.getByRole("button", { name: /Back/i }));
    expect(onBack).toHaveBeenCalled();
  });
});
