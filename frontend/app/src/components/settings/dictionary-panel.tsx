import { useEffect, useRef, useState, type ChangeEvent } from "react";

import {
  fetchAPIV1Dictionary,
  importAPIV1Dictionary,
  type DictionaryEntry as APIV1DictionaryEntry,
} from "@/lib/speechkit";

type DictionaryDraftEntry = {
  id: string;
  spoken: string;
  canonical: string;
  language?: string;
  source?: string;
  enabled?: boolean;
  usageCount?: number;
};

function createDictionaryEntry(index: number): DictionaryDraftEntry {
  return {
    id: `dictionary-entry-${Date.now()}-${index}`,
    spoken: "",
    canonical: "",
    enabled: true,
    usageCount: 0,
  };
}

function parseDictionaryEntries(value: string): DictionaryDraftEntry[] {
  return value
    .split(/\r?\n/)
    .map((line, index): DictionaryDraftEntry | null => {
      const trimmed = line.trim();
      if (!trimmed) return null;
      const arrowIndex = trimmed.indexOf("=>");
      const spoken =
        arrowIndex >= 0 ? trimmed.slice(0, arrowIndex).trim() : trimmed;
      const canonical =
        arrowIndex >= 0 ? trimmed.slice(arrowIndex + 2).trim() : "";
      if (!spoken) return null;
      return {
        id: `dictionary-entry-${index}-${spoken}-${canonical}`,
        spoken,
        canonical,
        enabled: true,
        usageCount: 0,
      };
    })
    .filter((entry): entry is DictionaryDraftEntry => Boolean(entry));
}

function serializeDictionaryEntries(entries: DictionaryDraftEntry[]) {
  return entries
    .map((entry) => ({
      spoken: entry.spoken.trim(),
      canonical: entry.canonical.trim(),
    }))
    .filter((entry) => entry.spoken.length > 0)
    .map((entry) =>
      entry.canonical && entry.canonical.toLowerCase() !== entry.spoken.toLowerCase()
        ? `${entry.spoken} => ${entry.canonical}`
        : entry.spoken,
    )
    .join("\n");
}

function draftEntriesFromAPIV1(
  entries: APIV1DictionaryEntry[],
): DictionaryDraftEntry[] {
  return entries.map((entry, index) => ({
    id: `dictionary-entry-api-${entry.id ?? index}-${entry.spoken}-${entry.canonical}`,
    spoken: entry.spoken,
    canonical: entry.canonical,
    language: entry.language,
    source: entry.source,
    enabled: entry.enabled,
    usageCount: entry.usageCount,
  }));
}

function apiEntriesFromDraft(
  entries: DictionaryDraftEntry[],
  language: string,
): APIV1DictionaryEntry[] {
  return entries
    .map((entry) => {
      const spoken = entry.spoken.trim();
      const canonical = entry.canonical.trim() || spoken;
      return {
        spoken,
        canonical,
        language: entry.language || language,
        source: entry.source || "settings",
        enabled: entry.enabled ?? true,
        usageCount: entry.usageCount ?? 0,
      };
    })
    .filter((entry) => entry.spoken.length > 0);
}

function parseDictionaryImportPayload(
  text: string,
): { language: string; entries: APIV1DictionaryEntry[] } {
  const payload = JSON.parse(text) as
    | APIV1DictionaryEntry[]
    | { language?: string; entries?: APIV1DictionaryEntry[] };
  if (Array.isArray(payload)) {
    return { language: "", entries: payload };
  }
  if (!payload || !Array.isArray(payload.entries)) {
    throw new Error("Dictionary import needs an entries array.");
  }
  return {
    language: typeof payload.language === "string" ? payload.language : "",
    entries: payload.entries,
  };
}

function downloadDictionaryExport(language: string, entries: APIV1DictionaryEntry[]) {
  const blob = new Blob(
    [
      JSON.stringify(
        {
          language,
          entries,
        },
        null,
        2,
      ),
    ],
    { type: "application/json" },
  );
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `speechkit-dictionary-${language || "default"}.json`;
  anchor.click();
  URL.revokeObjectURL(url);
}

export function DictionaryListEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [draftEntries, setDraftEntries] = useState<DictionaryDraftEntry[]>([]);
  const [apiLanguage, setApiLanguage] = useState("");
  const [apiEntries, setApiEntries] = useState<DictionaryDraftEntry[] | null>(
    null,
  );
  const [apiError, setApiError] = useState("");
  const [syncing, setSyncing] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const fallbackEntries = parseDictionaryEntries(value);
  const entries = apiEntries ?? fallbackEntries;
  const previewEntries = entries.slice(-3).reverse();

  useEffect(() => {
    let active = true;
    setSyncing(true);
    void fetchAPIV1Dictionary()
      .then((response) => {
        if (!active) return;
        const nextEntries = draftEntriesFromAPIV1(response.entries);
        setApiLanguage(response.language);
        setApiEntries(nextEntries.length > 0 ? nextEntries : null);
        setApiError("");
      })
      .catch(() => {
        if (active) {
          setApiEntries(null);
          setApiError("Dictionary API unavailable; editing local settings only.");
        }
      })
      .finally(() => {
        if (active) setSyncing(false);
      });
    return () => {
      active = false;
    };
  }, []);

  const openEditor = (appendBlank: boolean) => {
    const sourceEntries = entries.length > 0 ? entries : fallbackEntries;
    const nextEntries = sourceEntries.length > 0 ? sourceEntries : [createDictionaryEntry(0)];
    setDraftEntries(
      appendBlank
        ? [...nextEntries, createDictionaryEntry(nextEntries.length)]
        : nextEntries,
    );
    setOpen(true);
  };

  const updateDraftEntry = (
    index: number,
    patch: Partial<Omit<DictionaryDraftEntry, "id">>,
  ) => {
    setDraftEntries((current) =>
      current.map((entry, entryIndex) =>
        entryIndex === index ? { ...entry, ...patch } : entry,
      ),
    );
  };

  const removeDraftEntry = (index: number) => {
    setDraftEntries((current) => {
      const next = current.filter((_, entryIndex) => entryIndex !== index);
      return next.length > 0 ? next : [createDictionaryEntry(0)];
    });
  };

  const addDraftEntry = () => {
    setDraftEntries((current) => [
      ...current,
      createDictionaryEntry(current.length),
    ]);
  };

  const canSave = draftEntries.some((entry) => entry.spoken.trim().length > 0);

  const saveDraft = async () => {
    if (!canSave) return;
    setSyncing(true);
    setApiError("");
    const nextAPIEntries = apiEntriesFromDraft(draftEntries, apiLanguage);
    try {
      const response = await importAPIV1Dictionary(apiLanguage, nextAPIEntries);
      const nextDraftEntries = draftEntriesFromAPIV1(response.entries);
      setApiLanguage(response.language);
      setApiEntries(nextDraftEntries);
      onChange(serializeDictionaryEntries(nextDraftEntries));
      setOpen(false);
    } catch (error) {
      setApiEntries(draftEntries);
      onChange(serializeDictionaryEntries(draftEntries));
      setApiError(
        error instanceof Error
          ? `${error.message}; saved local settings only.`
          : "Dictionary API import failed; saved local settings only.",
      );
      setOpen(false);
    } finally {
      setSyncing(false);
    }
  };

  const handleExport = () => {
    downloadDictionaryExport(
      apiLanguage,
      apiEntriesFromDraft(entries, apiLanguage),
    );
  };

  const handleImportFile = async (
    event: ChangeEvent<HTMLInputElement>,
  ) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;

    let parsed: { language: string; entries: APIV1DictionaryEntry[] };
    try {
      parsed = parseDictionaryImportPayload(await file.text());
    } catch (error) {
      setApiError(
        error instanceof Error ? error.message : "Dictionary import failed.",
      );
      return;
    }

    const language = parsed.language || apiLanguage;
    const importedDraftEntries = draftEntriesFromAPIV1(parsed.entries);
    setSyncing(true);
    setApiError("");
    try {
      const response = await importAPIV1Dictionary(language, parsed.entries);
      const nextDraftEntries = draftEntriesFromAPIV1(response.entries);
      setApiLanguage(response.language);
      setApiEntries(nextDraftEntries);
      onChange(serializeDictionaryEntries(nextDraftEntries));
    } catch (error) {
      setApiLanguage(language);
      setApiEntries(importedDraftEntries);
      onChange(serializeDictionaryEntries(importedDraftEntries));
      setApiError(
        error instanceof Error
          ? `${error.message}; imported local settings only.`
          : "Dictionary API import failed; imported local settings only.",
      );
    } finally {
      setSyncing(false);
    }
  };

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-sm font-medium text-[color:var(--sk-text)]">
            Recent words
          </div>
          <p className="mt-1 text-[11px] leading-relaxed text-[color:var(--sk-text-muted)]/80">
            Add hard-to-recognise words and track which corrections are active.
          </p>
          <p className="mt-1 text-[10px] text-[color:var(--sk-text-subtle)]">
            {syncing ? "Syncing dictionary..." : `${entries.length} entries`}
            {apiLanguage ? ` · ${apiLanguage}` : ""}
          </p>
          {apiError && (
            <p className="mt-1 text-[10px] leading-relaxed text-amber-300/90">
              {apiError}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          <input
            ref={importInputRef}
            type="file"
            accept="application/json,.json"
            className="hidden"
            aria-label="Import dictionary file"
            onChange={handleImportFile}
          />
          <button
            type="button"
            onClick={() => importInputRef.current?.click()}
            className="sk-secondary-button h-8 rounded-lg px-3 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
          >
            Import
          </button>
          <button
            type="button"
            onClick={handleExport}
            className="sk-secondary-button h-8 rounded-lg px-3 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
          >
            Export
          </button>
          <button
            type="button"
            onClick={() => openEditor(true)}
            className="sk-secondary-button h-8 rounded-lg px-3 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
          >
            Add word
          </button>
          <button
            type="button"
            onClick={() => openEditor(false)}
            className="sk-secondary-button h-8 rounded-lg px-3 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
          >
            Manage list
          </button>
        </div>
      </div>

      {previewEntries.length > 0 ? (
        <div className="mt-3 grid gap-2">
          {previewEntries.map((entry) => (
            <div
              key={entry.id}
              className="flex min-h-9 items-center justify-between gap-3 border-b border-[color:var(--sk-shell-divider)]/70 py-2 last:border-b-0"
            >
              <span className="min-w-0 truncate text-sm text-[color:var(--sk-text)]">
                {entry.canonical || entry.spoken}
              </span>
              <span className="flex shrink-0 items-center gap-2 text-[11px] text-[color:var(--sk-text-muted)]">
                {entry.canonical && (
                  <span className="max-w-44 truncate">heard as {entry.spoken}</span>
                )}
                <span className="rounded-full bg-[color:var(--sk-surface-2)] px-2 py-0.5 text-[10px]">
                  {entry.usageCount ?? 0} uses
                </span>
              </span>
            </div>
          ))}
        </div>
      ) : (
        <button
          type="button"
          onClick={() => openEditor(true)}
          className="mt-3 flex h-12 w-full items-center justify-center rounded-lg border border-dashed border-[color:var(--sk-panel-border)] text-xs font-medium text-[color:var(--sk-text-muted)] transition-colors hover:border-[color:var(--sk-accent)]/40 hover:text-[color:var(--sk-text)]"
        >
          Add your first dictionary word
        </button>
      )}

      {open && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="Dictionary editor"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-6 py-6"
        >
          <div className="flex max-h-full w-full max-w-3xl flex-col rounded-[20px] border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-1)] shadow-2xl">
            <div className="flex items-center justify-between gap-4 border-b border-[color:var(--sk-shell-divider)] px-5 py-4">
              <div>
                <h3 className="text-sm font-semibold text-[color:var(--sk-text)]">
                  User Dictionary
                </h3>
                <p className="mt-1 text-[11px] text-[color:var(--sk-text-muted)]">
                  One row per word. Preferred spelling is optional.
                </p>
              </div>
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="sk-secondary-button h-8 rounded-lg px-3 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
              >
                Cancel
              </button>
            </div>

            <div className="min-h-0 overflow-y-auto px-5 py-4">
              <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_72px_auto] gap-2 text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--sk-text-muted)]">
                <span>Word or phrase</span>
                <span>Preferred spelling</span>
                <span className="text-right">Uses</span>
                <span className="w-16 text-right">Remove</span>
              </div>
              <div className="mt-2 grid gap-2">
                {draftEntries.map((entry, index) => (
                  <div
                    key={entry.id}
                    className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_72px_auto] items-center gap-2"
                  >
                    <input
                      aria-label={`Dictionary spoken word ${index + 1}`}
                      value={entry.spoken}
                      onChange={(event) =>
                        updateDraftEntry(index, {
                          spoken: event.target.value,
                        })
                      }
                      placeholder="AcmeOS"
                      className="sk-input h-9 min-w-0 rounded-lg px-3 text-xs"
                    />
                    <input
                      aria-label={`Dictionary preferred spelling ${index + 1}`}
                      value={entry.canonical}
                      onChange={(event) =>
                        updateDraftEntry(index, {
                          canonical: event.target.value,
                        })
                      }
                      placeholder="Optional"
                      className="sk-input h-9 min-w-0 rounded-lg px-3 text-xs"
                    />
                    <div
                      className="h-9 rounded-lg border border-[color:var(--sk-panel-border)] bg-[color:var(--sk-surface-0)] px-3 text-right text-xs leading-9 text-[color:var(--sk-text-muted)]"
                      aria-label={`Dictionary usage count ${index + 1}`}
                    >
                      {entry.usageCount ?? 0}
                    </div>
                    <button
                      type="button"
                      aria-label={`Remove dictionary word ${index + 1}`}
                      onClick={() => removeDraftEntry(index)}
                      className="sk-secondary-button h-9 w-16 rounded-lg px-2 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
                    >
                      Remove
                    </button>
                  </div>
                ))}
              </div>
              <button
                type="button"
                onClick={addDraftEntry}
                className="sk-secondary-button mt-3 h-8 rounded-lg px-3 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
              >
                Add row
              </button>
            </div>

            <div className="flex items-center justify-end gap-2 border-t border-[color:var(--sk-shell-divider)] px-5 py-4">
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="sk-secondary-button h-8 rounded-lg px-3 text-xs font-medium transition-colors hover:bg-[color:var(--sk-surface-3)]"
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={!canSave || syncing}
                onClick={() => void saveDraft()}
                className={[
                  "h-8 rounded-lg px-3 text-xs font-semibold transition-colors",
                  canSave && !syncing
                    ? "bg-[color:var(--sk-accent)] text-white hover:bg-[color:var(--sk-accent-strong)]"
                    : "cursor-not-allowed bg-[color:var(--sk-surface-2)] text-[color:var(--sk-text-subtle)]",
                ].join(" ")}
              >
                {syncing ? "Saving..." : "Save dictionary"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
