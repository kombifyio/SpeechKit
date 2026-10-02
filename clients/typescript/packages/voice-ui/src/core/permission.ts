/**
 * Microphone permission helpers. Hosts call {@link checkMicrophonePermission}
 * BEFORE opening a server session so a blocked or missing microphone never
 * costs a session/ticket, and {@link requestMicrophone} to acquire the stream
 * with canonical reason codes instead of raw DOMException messages.
 */

export type MicrophonePermissionState = "granted" | "prompt" | "denied" | "unsupported";

/** Canonical reason codes for microphone failures (rendered by `speechkit-voice-notice`). */
export type MicrophoneReasonCode =
  | "microphone_permission_denied"
  | "microphone_unavailable"
  | "microphone_in_use"
  | "microphone_unsupported";

export interface CheckMicrophonePermissionOptions {
  /**
   * When the Permissions API cannot answer, briefly open and immediately
   * release the microphone to learn the state. This may show the browser
   * prompt, so only pass it from a user gesture. Default `false`: the
   * fallback then answers `prompt` unless a labelled input device proves an
   * earlier grant.
   */
  probe?: boolean;
}

interface MediaDevicesLike {
  getUserMedia?: (constraints: MediaStreamConstraints) => Promise<MediaStream>;
  enumerateDevices?: () => Promise<MediaDeviceInfo[]>;
}

function mediaDevices(): MediaDevicesLike | undefined {
  const nav = globalThis.navigator as (Navigator & { mediaDevices?: MediaDevicesLike }) | undefined;
  return nav?.mediaDevices;
}

function captureSupported(): boolean {
  if ((globalThis as { isSecureContext?: boolean }).isSecureContext === false) return false;
  return typeof mediaDevices()?.getUserMedia === "function";
}

/** Stops every track of a stream (idempotent, null-safe). */
export function releaseMediaStream(stream: MediaStream | null | undefined): void {
  if (!stream) return;
  for (const track of stream.getTracks()) {
    try {
      track.stop();
    } catch {
      /* already stopped */
    }
  }
}

/** Maps a getUserMedia rejection to a canonical reason code. */
export function microphoneReasonFromError(error: unknown): MicrophoneReasonCode {
  const name = (error as { name?: unknown } | null)?.name;
  switch (name) {
    case "NotAllowedError":
    case "PermissionDeniedError":
    case "SecurityError":
      return "microphone_permission_denied";
    case "NotFoundError":
    case "DevicesNotFoundError":
    case "OverconstrainedError":
      return "microphone_unavailable";
    case "NotReadableError":
    case "TrackStartError":
    case "AbortError":
      return "microphone_in_use";
    case "TypeError":
    case "NotSupportedError":
      return "microphone_unsupported";
    default:
      return "microphone_permission_denied";
  }
}

/**
 * Resolves the microphone permission state without opening a server session:
 * Permissions API first (`navigator.permissions.query({ name: "microphone" })`),
 * then a device-label heuristic, then (only with `probe`) a getUserMedia
 * round-trip whose stream is released immediately.
 */
export async function checkMicrophonePermission(
  options: CheckMicrophonePermissionOptions = {}
): Promise<MicrophonePermissionState> {
  if (!captureSupported()) return "unsupported";

  const permissions = (globalThis.navigator as Navigator | undefined)?.permissions;
  if (permissions && typeof permissions.query === "function") {
    try {
      const status = await permissions.query({ name: "microphone" as PermissionName });
      if (status.state === "granted" || status.state === "denied" || status.state === "prompt") {
        return status.state;
      }
    } catch {
      // Browsers without the "microphone" permission name fall through.
    }
  }

  const devices = mediaDevices();
  if (typeof devices?.enumerateDevices === "function") {
    try {
      const inputs = (await devices.enumerateDevices()).filter((d) => d.kind === "audioinput");
      // Labels are only exposed after an earlier grant.
      if (inputs.some((d) => d.label.length > 0)) return "granted";
    } catch {
      // Ignore and fall through.
    }
  }

  if (!options.probe) return "prompt";
  const result = await requestMicrophone();
  if (result.ok) {
    releaseMediaStream(result.stream);
    return "granted";
  }
  switch (result.reason) {
    case "microphone_permission_denied":
      return "denied";
    case "microphone_unsupported":
      return "unsupported";
    case "microphone_in_use":
      return "granted"; // the browser granted access; another app holds the device
    case "microphone_unavailable":
      return "prompt"; // no device to ask for; requestMicrophone() reports the reason
  }
}

export type MicrophoneRequestResult =
  | { ok: true; stream: MediaStream }
  | { ok: false; reason: MicrophoneReasonCode; error?: unknown };

/**
 * Acquires the microphone (mono, echo cancellation, noise suppression by
 * default) and maps failures to canonical reason codes. The caller owns the
 * stream and releases it with {@link releaseMediaStream}.
 */
export async function requestMicrophone(
  constraints: MediaTrackConstraints = { echoCancellation: true, noiseSuppression: true, channelCount: 1 }
): Promise<MicrophoneRequestResult> {
  if (!captureSupported()) return { ok: false, reason: "microphone_unsupported" };
  try {
    const stream = await (mediaDevices() as Required<MediaDevicesLike>).getUserMedia({ audio: constraints });
    return { ok: true, stream };
  } catch (error) {
    return { ok: false, reason: microphoneReasonFromError(error), error };
  }
}
