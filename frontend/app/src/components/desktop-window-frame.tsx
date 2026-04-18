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
  sidebar?: ReactNode;
  actions?: ReactNode;
  contentClassName?: string;
  allowMaximise?: boolean;
  onClose?: () => void | Promise<void>;
  children: ReactNode;
};

export function DesktopWindowFrame({
  appLabel,
  title,
  subtitle,
  icon,
  theme,
  onToggleTheme,
  sidebar,
  actions,
  contentClassName,
  allowMaximise = true,
  onClose,
  children,
}: DesktopWindowFrameProps) {
  const [isMaximised, setIsMaximised] = useState(false);

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
      <header className="desktop-titlebar flex shrink-0 items-center justify-between gap-4 px-4 py-3">
        <div
          className="flex min-w-0 items-center gap-3"
          style={{ WebkitAppRegion: "drag" } as CSSProperties}
        >
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] text-[color:var(--sk-accent)]">
            {icon}
          </div>
          <div className="min-w-0">
            <p className="sk-kicker">{appLabel}</p>
            <h1 className="truncate text-lg font-semibold">{title}</h1>
            {subtitle ? (
              <p className="truncate text-xs text-[color:var(--sk-text-muted)]">
                {subtitle}
              </p>
            ) : null}
          </div>
        </div>

        <div
          className="flex items-center gap-2"
          style={{ WebkitAppRegion: "no-drag" } as CSSProperties}
        >
          {actions}
          <button
            type="button"
            onClick={onToggleTheme}
            aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
            className="sk-secondary-button inline-flex h-10 w-10 items-center justify-center rounded-full transition-colors hover:bg-[color:var(--sk-surface-3)]"
          >
            {theme === "dark" ? (
              <SunMedium className="h-4 w-4" />
            ) : (
              <MoonStar className="h-4 w-4" />
            )}
          </button>
          <button
            type="button"
            onClick={handleMinimise}
            aria-label="Minimise window"
            className="sk-secondary-button inline-flex h-10 w-10 items-center justify-center rounded-full transition-colors hover:bg-[color:var(--sk-surface-3)]"
          >
            <span className="block h-0.5 w-4 rounded-full bg-current" />
          </button>
          {allowMaximise ? (
            <button
              type="button"
              onClick={handleMaximise}
              aria-label={isMaximised ? "Restore window" : "Maximise window"}
              className="sk-secondary-button inline-flex h-10 w-10 items-center justify-center rounded-full transition-colors hover:bg-[color:var(--sk-surface-3)]"
            >
              <Square className="h-3.5 w-3.5" />
            </button>
          ) : null}
          <button
            type="button"
            onClick={handleClose}
            aria-label="Close window"
            className="inline-flex h-10 w-10 items-center justify-center rounded-full border border-red-400/18 bg-red-500/10 text-red-100 transition-colors hover:bg-red-500/16"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </header>

      <div className="min-h-0 flex-1 px-4 pb-4">
        <div className="flex h-full min-h-0 gap-4">
          {sidebar ? (
            <aside className="desktop-sidebar-panel sk-panel hidden w-64 shrink-0 rounded-[28px] p-3 md:flex md:flex-col">
              {sidebar}
            </aside>
          ) : null}
          <section
            className={[
              "desktop-content-panel sk-panel flex min-h-0 flex-1 flex-col overflow-hidden rounded-[28px]",
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
