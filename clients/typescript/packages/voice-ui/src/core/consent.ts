/**
 * Voice consent persistence: confirmed once, then remembered on the device
 * and (through the host hook) in the user's account. A stored decision is
 * honoured until it is revoked or the host raises `consentVersion` (e.g. the
 * consent copy or the processing destination changed).
 *
 * Scope semantics (fail-closed, Floating Panel reference): granting
 * `continuous` implies `one_shot`; a plain one-shot grant never implies
 * `continuous`; declining revokes every scope.
 */

export type VoiceConsentScope = "one_shot" | "continuous";
export type VoiceConsentDecision = "granted" | "declined" | "unset";

/** Persistence boundary consumed by `<speechkit-voice-consent>` and the overlay. */
export interface VoiceConsentAdapter {
  read(scope: VoiceConsentScope): VoiceConsentDecision;
  write(decision: Exclude<VoiceConsentDecision, "unset">, scope: VoiceConsentScope): void;
}

/** Storage schema key (the schema version, not the consent version). */
export const VOICE_CONSENT_STORAGE_KEY = "speechkit.voice.consent.v1";
/** Consent version assumed for records written before versioning existed. */
export const VOICE_CONSENT_DEFAULT_VERSION = "1";

/** Serializable consent record; hosts store this in the user's account as-is. */
export interface VoiceConsentRecord {
  decision: "granted" | "declined";
  scopes: VoiceConsentScope[];
  /** Host consent version the user agreed to. */
  consent_version: string;
  /** ISO-8601 timestamp of the decision. */
  decided_at: string;
}

export interface VoiceConsentStoreOptions {
  /** Per-surface record id inside the storage entry. Default `default`. */
  surface?: string;
  /** Raise to re-ask everyone (records of another version read as `unset`). */
  consentVersion?: string;
  storageKey?: string;
  /** Override the device storage (defaults to `localStorage`, guarded). */
  storage?: Pick<Storage, "getItem" | "setItem"> | null;
  /**
   * Host hook: called after every user decision or revocation (never for
   * {@link VoiceConsentStore.setConsentRecord} seeding) so the host can
   * persist the record in the user's account.
   */
  onConsentChange?: (record: VoiceConsentRecord | null) => void;
}

export interface VoiceConsentStore extends VoiceConsentAdapter {
  readonly consentVersion: string;
  /** The current, version-valid record (or `null` when unset / outdated). */
  getRecord(): VoiceConsentRecord | null;
  /**
   * Seeds the store from the host (e.g. the account record loaded at sign-in).
   * Persists to the device too and notifies subscribers, but does not echo
   * through `onConsentChange`. `null` clears the device record.
   */
  setConsentRecord(record: VoiceConsentRecord | null): void;
  /** Revokes consent (the next voice start asks again). */
  revoke(): void;
  subscribe(listener: (record: VoiceConsentRecord | null) => void): () => void;
}

const SCOPES: readonly VoiceConsentScope[] = ["one_shot", "continuous"];

function defaultStorage(): Pick<Storage, "getItem" | "setItem"> | null {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    // Sandboxed iframes without allow-same-origin throw on access.
    return null;
  }
}

function normalizeRecord(raw: unknown): VoiceConsentRecord | null {
  if (!raw || typeof raw !== "object") return null;
  const value = raw as Record<string, unknown>;
  const decision = value["decision"];
  if (decision !== "granted" && decision !== "declined") return null;
  const scopes = Array.isArray(value["scopes"])
    ? value["scopes"].filter((s): s is VoiceConsentScope => SCOPES.includes(s as VoiceConsentScope))
    : [];
  const version = value["consent_version"];
  const decidedAt = value["decided_at"];
  return {
    decision,
    scopes: decision === "declined" ? [] : scopes,
    consent_version: typeof version === "string" ? version : VOICE_CONSENT_DEFAULT_VERSION,
    decided_at: typeof decidedAt === "string" ? decidedAt : ""
  };
}

/**
 * Creates the device-backed consent store. It is a {@link VoiceConsentAdapter},
 * so it plugs straight into `consentAdapter` on `<speechkit-voice-consent>` and
 * `<speechkit-voice-overlay>`.
 */
export function createVoiceConsentStore(options: VoiceConsentStoreOptions = {}): VoiceConsentStore {
  const surface = options.surface ?? "default";
  const storageKey = options.storageKey ?? VOICE_CONSENT_STORAGE_KEY;
  const consentVersion = options.consentVersion ?? VOICE_CONSENT_DEFAULT_VERSION;
  const storage = options.storage === undefined ? defaultStorage() : options.storage;
  const listeners = new Set<(record: VoiceConsentRecord | null) => void>();
  // Fallback copy for this page when the device storage is missing or throws;
  // otherwise storage stays the source so several stores never disagree.
  let memory: VoiceConsentRecord | null | undefined;

  function readEntry(): Record<string, unknown> {
    if (!storage) return {};
    try {
      const parsed: unknown = JSON.parse(storage.getItem(storageKey) ?? "{}");
      return parsed && typeof parsed === "object" && !Array.isArray(parsed)
        ? (parsed as Record<string, unknown>)
        : {};
    } catch {
      return {}; // corrupted entries are replaced on the next write
    }
  }

  function stored(): VoiceConsentRecord | null {
    if (memory !== undefined) return memory;
    return normalizeRecord(readEntry()[surface]);
  }

  function persist(record: VoiceConsentRecord | null): void {
    memory = record;
    if (storage) {
      const entry = readEntry();
      if (record) entry[surface] = record;
      else delete entry[surface];
      try {
        storage.setItem(storageKey, JSON.stringify(entry));
        memory = undefined;
      } catch {
        // Quota/security errors: the in-memory decision still applies.
      }
    }
    for (const listener of listeners) listener(current());
  }

  function current(): VoiceConsentRecord | null {
    const record = stored();
    return record && record.consent_version === consentVersion ? record : null;
  }

  return {
    consentVersion,
    read(scope) {
      const record = current();
      if (!record) return "unset";
      if (record.decision === "declined") return "declined";
      if (scope === "one_shot") return "granted";
      return record.scopes.includes("continuous") ? "granted" : "unset";
    },
    write(decision, scope) {
      const previous = current();
      const scopes: VoiceConsentScope[] =
        decision === "declined"
          ? []
          : [
              ...new Set<VoiceConsentScope>([
                ...(previous?.decision === "granted" ? previous.scopes : []),
                "one_shot",
                ...(scope === "continuous" ? (["continuous"] as const) : [])
              ])
            ];
      const record: VoiceConsentRecord = {
        decision,
        scopes,
        consent_version: consentVersion,
        decided_at: new Date().toISOString()
      };
      persist(record);
      options.onConsentChange?.(record);
    },
    getRecord: current,
    setConsentRecord(record) {
      persist(record ? normalizeRecord(record) : null);
    },
    revoke() {
      persist(null);
      options.onConsentChange?.(null);
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  };
}
