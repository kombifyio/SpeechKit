import type {
  APIV1DictionaryResponse,
  APIV1ModesResponse,
  AudioDevice,
  AudioDevicesResponse,
  AudioDeviceUpdateResponse,
  DictionaryEntry,
  ModeModelSetting,
  ModeSource,
  RuntimeMode,
  VoiceAgentCloseBehavior,
  VoiceAgentSessionRecord,
} from "./types";

export async function fetchAudioDevices(): Promise<AudioDevicesResponse> {
  const response = await fetch("/audio/devices", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`audio devices request failed: ${response.status}`);
  }

  const payload = (await response.json()) as
    | AudioDevicesResponse
    | AudioDevice[]
    | {
        devices?: AudioDevice[];
        selectedDeviceId?: string;
        selectedAudioDeviceId?: string;
        deviceId?: string;
        currentDeviceId?: string;
      };

  if (Array.isArray(payload)) {
    return {
      devices: payload,
      selectedDeviceId:
        payload.find((device) => device.isDefault)?.deviceId ??
        payload[0]?.deviceId ??
        "",
    };
  }

  const normalizedPayload = payload as {
    devices?: AudioDevice[];
    selectedDeviceId?: string;
    selectedAudioDeviceId?: string;
    deviceId?: string;
    currentDeviceId?: string;
  };

  return {
    devices: normalizedPayload.devices ?? [],
    selectedDeviceId:
      normalizedPayload.selectedDeviceId ??
      normalizedPayload.selectedAudioDeviceId ??
      normalizedPayload.currentDeviceId ??
      normalizedPayload.deviceId ??
      "",
  };
}

export async function fetchAudioOutputDevices(): Promise<AudioDevicesResponse> {
  const response = await fetch("/audio/output/devices", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`audio output devices request failed: ${response.status}`);
  }

  const payload = (await response.json()) as
    | AudioDevicesResponse
    | AudioDevice[]
    | {
        devices?: AudioDevice[];
        selectedDeviceId?: string;
        selectedOutputDeviceId?: string;
        deviceId?: string;
        currentDeviceId?: string;
      };

  if (Array.isArray(payload)) {
    return {
      devices: payload,
      selectedDeviceId:
        payload.find((device) => device.isDefault)?.deviceId ??
        payload[0]?.deviceId ??
        "",
    };
  }

  const normalizedPayload = payload as {
    devices?: AudioDevice[];
    selectedDeviceId?: string;
    selectedOutputDeviceId?: string;
    deviceId?: string;
    currentDeviceId?: string;
  };

  return {
    devices: normalizedPayload.devices ?? [],
    selectedDeviceId:
      normalizedPayload.selectedDeviceId ??
      normalizedPayload.selectedOutputDeviceId ??
      normalizedPayload.currentDeviceId ??
      normalizedPayload.deviceId ??
      "",
  };
}

export async function setAudioDevice(deviceId: string): Promise<string> {
  const body = new URLSearchParams({
    device_id: deviceId,
    audio_device_id: deviceId,
    selected_audio_device_id: deviceId,
  });
  const response = await fetch("/audio/device", {
    method: "POST",
    body,
  });

  if (!response.ok) {
    throw new Error(`set audio device failed: ${response.status}`);
  }

  const payload = (await response.json()) as AudioDeviceUpdateResponse;
  return payload.message ?? "";
}

export async function setAudioOutputDevice(deviceId: string): Promise<string> {
  const body = new URLSearchParams({
    device_id: deviceId,
    selected_output_device_id: deviceId,
  });
  const response = await fetch("/audio/output/device", {
    method: "POST",
    body,
  });

  if (!response.ok) {
    throw new Error(`set audio output device failed: ${response.status}`);
  }

  const payload = (await response.json()) as AudioDeviceUpdateResponse;
  return payload.message ?? "";
}

export async function setActiveMode(mode: RuntimeMode): Promise<string> {
  const body = new URLSearchParams({ mode });
  const response = await fetch("/mode/active", {
    method: "POST",
    body,
  });

  if (!response.ok) {
    throw new Error(`set mode failed: ${response.status}`);
  }

  const payload = (await response.json()) as { message?: string };
  return payload.message ?? "";
}

export async function setModeEnabled(
  mode: Exclude<RuntimeMode, "none">,
  enabled: boolean,
): Promise<string> {
  const body = new URLSearchParams({
    mode,
    enabled: enabled ? "1" : "0",
  });
  const response = await fetch("/mode/enabled", {
    method: "POST",
    body,
  });

  if (!response.ok) {
    throw new Error(`set mode enabled failed: ${response.status}`);
  }

  const payload = (await response.json()) as { message?: string };
  return payload.message ?? "";
}

export async function fetchAPIV1Modes(): Promise<APIV1ModesResponse> {
  const response = await fetch("/api/v1/modes", { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`api v1 modes request failed: ${response.status}`);
  }
  return (await response.json()) as APIV1ModesResponse;
}

export async function fetchAPIV1Dictionary(
  language?: string,
): Promise<APIV1DictionaryResponse> {
  const suffix = language ? `?language=${encodeURIComponent(language)}` : "";
  const response = await fetch(`/api/v1/dictionary${suffix}`, {
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`api v1 dictionary request failed: ${response.status}`);
  }
  return (await response.json()) as APIV1DictionaryResponse;
}

export async function importAPIV1Dictionary(
  language: string,
  entries: DictionaryEntry[],
): Promise<APIV1DictionaryResponse> {
  const response = await fetch("/api/v1/dictionary", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ language, entries }),
  });
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(errorText || `api v1 dictionary import failed: ${response.status}`);
  }
  return (await response.json()) as APIV1DictionaryResponse;
}

export async function fetchAPIV1VoiceSessions(
  limit = 20,
): Promise<VoiceAgentSessionRecord[]> {
  const response = await fetch(`/api/v1/voice-sessions?limit=${limit}`, {
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`api v1 voice sessions request failed: ${response.status}`);
  }
  return (await response.json()) as VoiceAgentSessionRecord[];
}

export async function patchAPIV1ModeSettings(
  mode: "dictation" | "assist" | "voice_agent",
  patch: Partial<
    ModeModelSetting & {
      dictionaryEnabled: boolean;
      ttsEnabled: boolean;
      sessionSummary: boolean;
      pipelineFallback: boolean;
      closeBehavior: VoiceAgentCloseBehavior;
      agentProfileId: string;
      modeSource: ModeSource;
    }
  >,
): Promise<ModeModelSetting> {
  const response = await fetch(`/api/v1/modes/${mode}/settings`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(
      errorText || `api v1 mode settings failed: ${response.status}`,
    );
  }
  return (await response.json()) as ModeModelSetting;
}
