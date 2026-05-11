import { render, screen } from "@testing-library/react";
import { vi } from "vitest";

import { baseSettings } from "@/components/settings-test-fixtures";
import { GeneralSettingsPage } from "@/components/settings/pages/general-settings-page";

vi.mock("@/components/ui/mic-selector", () => ({
  MicSelector: () => <div data-testid="mic-selector" />,
}));

const noopUpdate = () => {};
const noopToggle = () => {};
const noopAsync = async () => {};

function renderPage() {
  render(
    <GeneralSettingsPage
      settings={baseSettings}
      updateSettings={noopUpdate}
      toggleModeEnabled={noopToggle}
      hasSavedOverlaySpot={false}
      onSaveCurrentOverlaySpot={noopAsync}
      onResetOverlaySpot={noopAsync}
    />,
  );
}

describe("GeneralSettingsPage release-gated overlay options", () => {
  it("hides unreleased Focus and kombify overlay options by default", () => {
    renderPage();

    expect(
      screen.getByRole("button", { name: /Default \(Pill\)/ }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Focus \(Dot\)/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "kombify" }),
    ).not.toBeInTheDocument();
  });
});
