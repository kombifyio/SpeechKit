import { LayoutGrid, LibraryBig, Settings2, TerminalSquare } from "lucide-react";
import type { ReactNode } from "react";

import type { AppVersionInfo } from "@/lib/speechkit";

import { formatAppVersionLabel } from "./dashboard-formatters";
import type { DashboardTab } from "./dashboard-types";

export function DashboardSidebar({
  tab,
  appVersionInfo,
  onSelectTab,
}: {
  tab: DashboardTab;
  appVersionInfo: AppVersionInfo | null;
  onSelectTab: (tab: DashboardTab) => void;
}) {
  return (
    <>
      <nav className="flex-1 space-y-1 px-3 py-4">
        <NavBtn
          active={tab === "dashboard"}
          onClick={() => onSelectTab("dashboard")}
        >
          <LayoutGrid className="h-4.5 w-4.5 shrink-0" />
          Dashboard
        </NavBtn>
        <NavBtn
          active={tab === "library"}
          onClick={() => onSelectTab("library")}
        >
          <LibraryBig className="h-4.5 w-4.5 shrink-0" />
          Library
        </NavBtn>
        <NavBtn
          active={tab === "settings"}
          onClick={() => onSelectTab("settings")}
        >
          <Settings2 className="h-4.5 w-4.5 shrink-0" />
          Settings
        </NavBtn>
        <NavBtn active={tab === "logs"} onClick={() => onSelectTab("logs")}>
          <TerminalSquare className="h-4.5 w-4.5 shrink-0" />
          Logs
        </NavBtn>
      </nav>

      <div className="px-3 pb-3">
        <div className="rounded-[22px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-2)] px-4 py-3">
          <p className="sk-kicker">Version</p>
          <p className="mt-1 text-sm font-medium text-[color:var(--sk-text)]">
            {appVersionInfo?.version
              ? `Build ${formatAppVersionLabel(appVersionInfo.version)}`
              : "Custom chrome active"}
          </p>
          <p className="mt-2 text-xs leading-5 text-[color:var(--sk-text-muted)]">
            Light and dark chrome now share the same desktop shell and controls.
          </p>
        </div>
      </div>
    </>
  );
}

function NavBtn({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        "flex items-center gap-3 w-full px-3 py-2.5 rounded-2xl text-sm transition-all",
        active
          ? "bg-[color:var(--sk-accent-soft)] text-[color:var(--sk-accent)] font-semibold"
          : "text-[color:var(--sk-text-muted)] hover:bg-[color:var(--sk-surface-2)] hover:text-[color:var(--sk-text)]",
      ].join(" ")}
    >
      {children}
    </button>
  );
}
