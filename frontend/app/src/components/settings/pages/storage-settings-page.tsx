import { FolderOpen } from "lucide-react";

import { Chip, Row, Section } from "@/components/settings/settings-primitives";
import type { SpeechKitSettingsState } from "@/lib/speechkit";

export function StorageSettingsPage({
  settings,
  postgresReady,
  updateSettings,
  onChooseStorageFolder,
}: {
  settings: SpeechKitSettingsState;
  postgresReady: boolean;
  updateSettings: (patch: Partial<SpeechKitSettingsState>, delay?: number) => void;
  onChooseStorageFolder: (target: "sqlite" | "model_downloads") => Promise<void>;
}) {
  return (
    <div className="grid grid-cols-1 gap-y-5 xl:grid-cols-2 xl:gap-x-10">
      <Section
        title="Storage"
        className="xl:col-span-2"
        testId="storage-settings-card"
      >
        <div className="flex flex-wrap gap-1.5">
          <Chip
            active={settings.storeBackend === "sqlite"}
            onClick={() => updateSettings({ storeBackend: "sqlite" })}
          >
            SQLite
          </Chip>
          <Chip
            active={settings.storeBackend === "postgres"}
            onClick={() => updateSettings({ storeBackend: "postgres" })}
          >
            PostgreSQL
          </Chip>
        </div>
        <p
          className={[
            "mt-1.5 rounded border px-2.5 py-1.5 text-[11px]",
            settings.storeBackend === "postgres" && !postgresReady
              ? "border-orange-500/20 bg-orange-500/5 text-orange-200/70"
              : "border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] text-[color:var(--sk-text-muted)]",
          ].join(" ")}
        >
          {settings.storeBackend === "sqlite"
            ? "SQLite keeps metadata in the local SpeechKit app data folder."
            : postgresReady
              ? "PostgreSQL metadata backend is configured. Restart the app after changes."
              : "Add a PostgreSQL connection string before switching the metadata backend."}
        </p>
        {settings.storeBackend === "sqlite" ? (
          <div className="mt-1.5 flex gap-2">
            <input
              id="sqlite-path-input"
              aria-label="SQLite path"
              value={settings.sqlitePath}
              onChange={(e) =>
                updateSettings({ sqlitePath: e.target.value }, 350)
              }
              placeholder="%APPDATA%/SpeechKit/feedback.db"
              className="sk-input h-8 min-w-0 flex-1 rounded px-2.5 text-xs"
            />
            <button
              type="button"
              aria-label="Choose SQLite storage folder"
              title="Choose SQLite storage folder"
              onClick={() => void onChooseStorageFolder("sqlite")}
              className="sk-secondary-button inline-flex h-8 shrink-0 items-center gap-1.5 rounded px-2.5 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
            >
              <FolderOpen className="h-3.5 w-3.5" />
              Browse
            </button>
          </div>
        ) : (
          <input
            id="postgres-dsn-input"
            aria-label="PostgreSQL connection string"
            value={settings.postgresDSN}
            onChange={(e) =>
              updateSettings({ postgresDSN: e.target.value }, 350)
            }
            placeholder="postgres://user:password@host:5432/speechkit?sslmode=disable"
            className="sk-input mt-1.5 h-8 w-full rounded px-2.5 text-xs"
          />
        )}
        <label className="mt-2.5 flex flex-col gap-1.5">
          <span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">
            Model downloads
          </span>
          <div className="flex gap-2">
            <input
              id="model-download-dir-input"
              aria-label="Default model download folder"
              value={settings.modelDownloadDir}
              onChange={(e) =>
                updateSettings({ modelDownloadDir: e.target.value }, 350)
              }
              placeholder="%LOCALAPPDATA%/SpeechKit/models"
              className="sk-input h-8 min-w-0 flex-1 rounded px-2.5 text-xs"
            />
            <button
              type="button"
              aria-label="Choose model download folder"
              title="Choose model download folder"
              onClick={() => void onChooseStorageFolder("model_downloads")}
              className="sk-secondary-button inline-flex h-8 shrink-0 items-center gap-1.5 rounded px-2.5 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
            >
              <FolderOpen className="h-3.5 w-3.5" />
              Browse
            </button>
          </div>
        </label>
        <div className="mt-2.5">
          <Row
            label="Save raw audio locally"
            on={settings.saveAudio}
            onToggle={() => updateSettings({ saveAudio: !settings.saveAudio })}
          />
        </div>
        <div className="mt-2 grid grid-cols-2 gap-3">
          <div>
            <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">
              Audio retention
            </div>
            <select
              id="audio-retention-select"
              aria-label="Audio retention"
              value={String(settings.audioRetentionDays)}
              onChange={(e) =>
                updateSettings({
                  audioRetentionDays: Number(e.target.value),
                })
              }
              className="h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
            >
              <option value="0">No automatic deletion</option>
              <option value="1">1 day</option>
              <option value="7">7 days</option>
              <option value="30">30 days</option>
              <option value="90">90 days</option>
            </select>
          </div>
          <div>
            <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-[#938ea1]">
              Max storage (MB)
            </div>
            <input
              id="max-audio-storage-input"
              aria-label="Max local audio storage (MB)"
              type="number"
              min="0"
              value={String(settings.maxAudioStorageMB)}
              onChange={(e) => {
                const nextValue = Number.parseInt(e.target.value, 10);
                if (Number.isNaN(nextValue) || nextValue < 0) return;
                updateSettings({ maxAudioStorageMB: nextValue }, 250);
              }}
              className="h-8 w-full rounded border border-[#484555] bg-[#0e0e13] px-2.5 text-xs text-[#e4e1e9] outline-none focus:border-[#947dff]/50"
            />
          </div>
        </div>
      </Section>
    </div>
  );
}
