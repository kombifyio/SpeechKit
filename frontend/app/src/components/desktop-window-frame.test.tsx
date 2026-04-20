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

  it("marks the custom titlebar as a Wails drag region while keeping controls interactive", () => {
    const { container } = render(
      <DesktopWindowFrame
        appLabel="SpeechKit"
        title="Dashboard"
        theme="dark"
        onToggleTheme={() => {}}
      >
        <main />
      </DesktopWindowFrame>,
    );

    const titlebar = container.querySelector("header.desktop-titlebar");
    const controls = titlebar?.children.item(1);
    const titlebarStyle = titlebar?.getAttribute("style") ?? "";
    const controlsStyle = controls?.getAttribute("style") ?? "";

    expect(titlebar).not.toBeNull();
    expect(titlebarStyle).toContain("--wails-draggable: drag");
    expect(controls).not.toBeNull();
    expect(controlsStyle).toContain("--wails-draggable: no-drag");
  });

  it("renders compact chrome for small overlay windows", () => {
    const { container, queryByText } = render(
      <DesktopWindowFrame
        appLabel="Voice Agent"
        title="Voice Agent"
        subtitle="Realtime dialog surface"
        theme="dark"
        onToggleTheme={() => {}}
        allowMaximise={false}
        density="compact"
        showThemeToggle={false}
      >
        <main />
      </DesktopWindowFrame>,
    );

    const titlebar = container.querySelector("header.desktop-titlebar");
    const contentWrap = container.querySelector("div.desktop-content-wrap");
    const contentPanel = container.querySelector("section.desktop-content-panel");

    expect(titlebar).toHaveClass("gap-2");
    expect(titlebar).toHaveClass("px-2.5");
    expect(contentWrap).toHaveClass("px-2.5");
    expect(contentPanel).toHaveClass("rounded-[20px]");
    expect(queryByText("Realtime dialog surface")).not.toBeInTheDocument();
    expect(container.querySelector("[aria-label='Switch to light mode']")).toBeNull();
    expect(container.querySelector("[aria-label='Maximise window']")).toBeNull();
  });
});
