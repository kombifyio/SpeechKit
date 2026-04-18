import { render } from "@testing-library/react";
import { vi } from "vitest";

import { DesktopWindowFrame } from "@/components/desktop-window-frame";

const {
  windowMinimiseMock,
  windowMaximiseMock,
  windowRestoreMock,
  windowCloseMock,
  windowIsMaximisedMock,
} = vi.hoisted(() => ({
  windowMinimiseMock: vi.fn<() => Promise<void>>(),
  windowMaximiseMock: vi.fn<() => Promise<void>>(),
  windowRestoreMock: vi.fn<() => Promise<void>>(),
  windowCloseMock: vi.fn<() => Promise<void>>(),
  windowIsMaximisedMock: vi.fn<() => Promise<boolean>>(),
}));

vi.mock("@wailsio/runtime", () => ({
  Window: {
    Minimise: windowMinimiseMock,
    Maximise: windowMaximiseMock,
    Restore: windowRestoreMock,
    Close: windowCloseMock,
    IsMaximised: windowIsMaximisedMock,
  },
}));

describe("DesktopWindowFrame", () => {
  beforeEach(() => {
    windowMinimiseMock.mockReset();
    windowMaximiseMock.mockReset();
    windowRestoreMock.mockReset();
    windowCloseMock.mockReset();
    windowIsMaximisedMock.mockReset();
    windowMinimiseMock.mockResolvedValue(undefined);
    windowMaximiseMock.mockResolvedValue(undefined);
    windowRestoreMock.mockResolvedValue(undefined);
    windowCloseMock.mockResolvedValue(undefined);
    windowIsMaximisedMock.mockResolvedValue(false);
  });

  it("keeps the content panel as a constrained flex column for nested scrolling", () => {
    const { container } = render(
      <DesktopWindowFrame
        appLabel="SpeechKit"
        title="Settings"
        theme="dark"
        onToggleTheme={() => {}}
      >
        <main className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div className="min-h-0 flex-1 overflow-y-auto" data-testid="scroll-region" />
        </main>
      </DesktopWindowFrame>,
    );

    const contentPanel = container.querySelector("section.desktop-content-panel");

    expect(contentPanel).not.toBeNull();
    expect(contentPanel).toHaveClass("flex");
    expect(contentPanel).toHaveClass("flex-col");
    expect(contentPanel).toHaveClass("overflow-hidden");
  });
});
