import { describe, expect, it } from "vitest";

import {
  deriveAvailableModes,
  directoryFromPath,
  joinFolderAndFile,
  reconcileSettingsState,
  sqliteFilenameFromPath,
} from "@/components/settings/settings-state";
import { defaultSettingsState } from "@/lib/speechkit";

describe("settings state helpers", () => {
  it("derives available modes only when enabled modes still have hotkeys", () => {
    const settings = {
      ...defaultSettingsState,
      assistHotkey: "",
      modeEnabled: {
        dictate: true,
        assist: true,
        voice_agent: false,
      },
    };

    expect(deriveAvailableModes(settings)).toEqual({
      dictate: true,
      assist: false,
      voice_agent: false,
    });
  });

  it("reconciles active mode to none when its hotkey becomes unavailable", () => {
    const next = reconcileSettingsState({
      ...defaultSettingsState,
      activeMode: "assist",
      assistHotkey: "",
      modeEnabled: {
        dictate: true,
        assist: true,
        voice_agent: true,
      },
    });

    expect(next.activeMode).toBe("none");
    expect(next.modeEnabled.assist).toBe(false);
  });

  it("normalizes SQLite folder and filename fields without changing separators", () => {
    expect(directoryFromPath("C:\\data\\feedback.db")).toBe("C:\\data");
    expect(sqliteFilenameFromPath("C:\\data\\feedback")).toBe("feedback.db");
    expect(joinFolderAndFile("C:\\data", "feedback.db")).toBe(
      "C:\\data\\feedback.db",
    );
    expect(joinFolderAndFile("/var/lib/", "feedback.db")).toBe(
      "/var/lib/feedback.db",
    );
  });
});
