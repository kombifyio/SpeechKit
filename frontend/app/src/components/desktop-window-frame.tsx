import { useEffect, useState, type CSSProperties, type ReactNode } from "react";
import { MoonStar, Square, SunMedium, X } from "lucide-react";
import { Window } from "@wailsio/runtime";

import type { DesktopTheme } from "@/lib/desktop-theme";

type DesktopWindowFrameProps = {
  appLabel: string;
  title: string;
  subtitle?: string;
  icon?: ReactNode;
  theme: DesktopTheme;
  onToggleTheme: () => void;
  density?: "default" | "compact";
  showThemeToggle?: boolean;
  sidebar?: ReactNode;
  actions?: ReactNode;
  contentClassName?: string;
  allowMaximise?: boolean;
  onClose?: () => void | Promise<void>;
  children: ReactNode;
};

const desktopDragRegionStyle = {
  ["--wails-draggable" as string]: "drag",
  WebkitAppRegion: "drag",
} as CSSProperties;

const desktopNoDragRegionStyle = {
  ["--wails-draggable" as string]: "no-drag",
  WebkitAppRegion: "no-drag",
} as CSSProperties;

export function DesktopWindowFrame({
  appLabel,
  title,
  subtitle,
  icon,
  theme,
  onToggleTheme,
  density = "default",
  showThemeToggle = true,
  sidebar,
  actions,
  contentClassName,
  allowMaximise = true,
  onClose,
  children,
}: DesktopWindowFrameProps) {
  const [isMaximised, setIsMaximised] = useState(false);
  const compact = density === "compact";

  useEffect(() => {
    let active = true;
    void Window.IsMaximised()
      .then((next) => {
        if (active) {
          setIsMaximised(Boolean(next));
        }
      })
      .catch(() => {
        if (active) {
          setIsMaximised(false);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const handleMinimise = () => {
    void Window.Minimise();
  };

  const handleMaximise = () => {
    if (!allowMaximise) {
      return;
    }
    void Window.IsMaximised()
      .then((maximised) => {
        if (maximised) {
          return Window.Restore().then(() => setIsMaximised(false));
        }
        return Window.Maximise().then(() => setIsMaximised(true));
      })
      .catch(() => {});
  };

  const handleClose = () => {
    if (onClose) {
      void onClose();
      return;
    }
    void Window.Close();
  };

  return (
    <div className="desktop-shell-root flex h-screen w-screen flex-col overflow-hidden text-[color:var(--sk-text)]">
      <header
        className={[
          "desktop-titlebar flex shrink-0 items-center justify-between",
          compact ? "gap-2 px-2.5 py-2" : "gap-4 px-4 py-3",
        ].join(" ")}
        style={desktopDragRegionStyle}
      >
        <div
          className={["flex min-w-0 items-center", compact ? "gap-2" : "gap-3"].join(" ")}
        >
          <div
            className={[
              "flex shrink-0 items-center justify-center border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] text-[color:var(--sk-accent)]",
              compact ? "h-8 w-8 rounded-xl" : "h-10 w-10 rounded-2xl",
            ].join(" ")}
          >
            {icon}
          </div>
          <div className="min-w-0">
            <p className="sk-kicker">{appLabel}</p>
            <h1 className={["truncate font-semibold", compact ? "text-sm" : "text-lg"].join(" ")}>
              {title}
            </h1>
            {!compact && subtitle ? (
              <p className="truncate text-xs text-[color:var(--sk-text-muted)]">
                {subtitle}
              </p>
            ) : null}
          </div>
        </div>

        <div
          className={["flex items-center", compact ? "gap-1" : "gap-2"].join(" ")}
          style={desktopNoDragRegionStyle}
        >
          {actions}
          {showThemeToggle ? (
            <button
              type="button"
              onClick={onToggleTheme}
              aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
              className={[
                "sk-secondary-button inline-flex items-center justify-center rounded-full transition-colors hover:bg-[color:var(--sk-surface-3)]",
                compact ? "h-8 w-8" : "h-10 w-10",
              ].join(" ")}
            >
              {theme === "dark" ? (
                <SunMedium className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
              ) : (
                <MoonStar className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
              )}
            </button>
          ) : null}
          <button
            type="button"
            onClick={handleMinimise}
            aria-label="Minimise window"
            className={[
              "sk-secondary-button inline-flex items-center justify-center rounded-full transition-colors hover:bg-[color:var(--sk-surface-3)]",
              compact ? "h-8 w-8" : "h-10 w-10",
            ].join(" ")}
          >
            <span className={["block h-0.5 rounded-full bg-current", compact ? "w-3.5" : "w-4"].join(" ")} />
          </button>
          {allowMaximise ? (
            <button
              type="button"
              onClick={handleMaximise}
              aria-label={isMaximised ? "Restore window" : "Maximise window"}
              className={[
                "sk-secondary-button inline-flex items-center justify-center rounded-full transition-colors hover:bg-[color:var(--sk-surface-3)]",
                compact ? "h-8 w-8" : "h-10 w-10",
              ].join(" ")}
            >
              <Square className={compact ? "h-3 w-3" : "h-3.5 w-3.5"} />
            </button>
          ) : null}
          <button
            type="button"
            onClick={handleClose}
            aria-label="Close window"
            className={[
              "inline-flex items-center justify-center rounded-full border border-red-400/18 bg-red-500/10 text-red-100 transition-colors hover:bg-red-500/16",
              compact ? "h-8 w-8" : "h-10 w-10",
            ].join(" ")}
          >
            <X className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
          </button>
        </div>
      </header>

      <div className={["desktop-content-wrap min-h-0 flex-1", compact ? "px-2.5 pb-2.5" : "px-4 pb-4"].join(" ")}>
        <div className={["flex h-full min-h-0", compact ? "gap-2.5" : "gap-4"].join(" ")}>
          {sidebar ? (
            <aside className="desktop-sidebar-panel sk-panel hidden w-64 shrink-0 rounded-[28px] p-3 md:flex md:flex-col">
              {sidebar}
            </aside>
          ) : null}
          <section
            className={[
              "desktop-content-panel sk-panel flex min-h-0 flex-1 flex-col overflow-hidden",
              compact ? "rounded-[20px]" : "rounded-[28px]",
              contentClassName ?? "",
            ].join(" ")}
          >
            {children}
          </section>
        </div>
      </div>
    </div>
  );
}
