import { MastraVoice } from "@mastra/core/voice";
import { Buffer } from "node:buffer";
import { Readable, addAbortSignal } from "node:stream";
import { SpeechKitClient, type RequestOverrides } from "./client.js";
import type { TTSSynthesizeRequest, TTSSynthesizeResponse, TranscribeOptions, Voice } from "./types.js";

export interface SpeechKitVoiceOptions {
  client: SpeechKitClient;
  /** Default SpeechKit voice ID. The server still owns provider routing. */
  speaker?: string;
}

export type SpeechKitSpeakOptions = Omit<TTSSynthesizeRequest, "text" | "voice"> &
  RequestOverrides & { speaker?: string };

export type SpeechKitListenOptions = TranscribeOptions &
  RequestOverrides & {
    /** File extension must match the encoded audio. Defaults to speech.wav. */
    filename?: string;
    /** MIME type for a Node audio stream. Blob inputs retain their own type. */
    contentType?: string;
  };

/** Complete encoded audio, with the actual format reported by SpeechKit. */
export type SpeechKitAudioStream = Readable & Omit<TTSSynthesizeResponse, "audio_base64">;
export type SpeechKitSpeaker = Voice & { voiceId: string };

const maxAudioBytes = 25 * 1024 * 1024;
const maxTextBytes = 256 * 1024;

/**
 * Buffered STT and TTS through a local, self-hosted or Gateway-fronted SpeechKit
 * server. Microphone capture, playback, consent and realtime sessions belong to
 * the host and the existing SpeechKit Voice Agent client.
 */
export class SpeechKitVoice extends MastraVoice<unknown, SpeechKitSpeakOptions, SpeechKitListenOptions> {
  readonly #client: SpeechKitClient;

  constructor({ client, speaker }: SpeechKitVoiceOptions) {
    super({
      name: "SpeechKitVoice",
      speechModel: { name: "speechkit" },
      listeningModel: { name: "speechkit" },
      ...(speaker === undefined ? {} : { speaker }),
    });
    this.#client = client;
  }

  override async speak(
    input: string | NodeJS.ReadableStream,
    options: SpeechKitSpeakOptions = {},
  ): Promise<SpeechKitAudioStream> {
    const { speaker = this.speaker, signal, headers, timeoutMs, ...speech } = options;
    signal?.throwIfAborted();
    const text = typeof input === "string"
      ? input
      : (await readStream(input, maxTextBytes, signal)).toString("utf8");
    if (Buffer.byteLength(text) > maxTextBytes) throw new RangeError("SpeechKit text exceeds 256 KiB");
    if (!text.trim()) throw new TypeError("SpeechKit speech input must contain text");
    const response = await this.#client.ttsSynthesize(
      { ...speech, text, ...(speaker === undefined ? {} : { voice: speaker }) },
      requestOverrides({ signal, headers, timeoutMs }),
    );
    const { audio_base64, format, sample_rate, duration_ms, provider, voice } = response;
    if (typeof audio_base64 !== "string" || typeof format !== "string") {
      throw new TypeError("SpeechKit returned an invalid speech response");
    }
    const audio = Buffer.from(audio_base64, "base64");
    if (!audio.length) throw new TypeError("SpeechKit returned empty speech audio");
    return Object.assign(Readable.from([audio]), {
      format,
      ...(sample_rate === undefined ? {} : { sample_rate }),
      ...(duration_ms === undefined ? {} : { duration_ms }),
      ...(provider === undefined ? {} : { provider }),
      ...(voice === undefined ? {} : { voice }),
    });
  }

  override async listen(audio: unknown, options: SpeechKitListenOptions = {}): Promise<string> {
    const { filename = "speech.wav", contentType = "audio/wav", signal, headers, timeoutMs, ...transcription } = options;
    signal?.throwIfAborted();
    let blob: Blob;
    if (audio instanceof Blob) {
      if (audio.size > maxAudioBytes) throw new RangeError("SpeechKit audio exceeds 25 MiB");
      blob = audio;
    } else if (audio instanceof Readable) {
      const bytes = await readStream(audio, maxAudioBytes, signal);
      blob = new Blob([new Uint8Array(bytes)], { type: contentType });
    } else {
      throw new TypeError("SpeechKit listen requires a Node Readable or an encoded audio Blob");
    }
    if (!blob.size) throw new TypeError("SpeechKit audio input must not be empty");
    const response = await this.#client.transcribe(
      blob,
      { ...transcription, filename },
      requestOverrides({ signal, headers, timeoutMs }),
    );
    if (typeof response.text !== "string") throw new TypeError("SpeechKit returned an invalid transcript");
    return response.text;
  }

  /** Configured server snapshot. Voice IDs do not pin a provider. */
  override async getSpeakers(overrides?: RequestOverrides): Promise<SpeechKitSpeaker[]> {
    const voices = await this.#client.ttsVoices(overrides);
    return voices.map((voice) => ({ ...voice, voiceId: voice.id }));
  }

  /** Advertises the transcription capability, without a provider health probe. */
  override async getListener(): Promise<{ enabled: boolean }> {
    return { enabled: true };
  }

  override async connect(): Promise<never> {
    throw realtimeUnsupported();
  }

  override async send(): Promise<never> {
    throw realtimeUnsupported();
  }

  override async answer(): Promise<never> {
    throw realtimeUnsupported();
  }

  override updateConfig(): never {
    throw new Error("Create a new SpeechKitVoice to change its configuration");
  }
}

function realtimeUnsupported(): Error {
  return new Error("SpeechKitVoice supports buffered STT/TTS. Use @kombifyio/speechkit-voiceagent-client for realtime sessions.");
}

function requestOverrides({ signal, headers, timeoutMs }: {
  signal: AbortSignal | undefined;
  headers: Record<string, string> | undefined;
  timeoutMs: number | undefined;
}): RequestOverrides {
  return {
    ...(signal === undefined ? {} : { signal }),
    ...(headers === undefined ? {} : { headers }),
    ...(timeoutMs === undefined ? {} : { timeoutMs }),
  };
}

async function readStream(
  stream: NodeJS.ReadableStream,
  limit: number,
  signal?: AbortSignal,
): Promise<Buffer> {
  if (!(stream instanceof Readable)) throw new TypeError("SpeechKit requires a Node Readable stream");
  signal?.throwIfAborted();
  if (signal) addAbortSignal(signal, stream);
  const chunks: Uint8Array[] = [];
  let size = 0;
  for await (const value of stream) {
    const chunk: unknown = value;
    if (typeof chunk !== "string" && !(chunk instanceof Uint8Array)) {
      throw new TypeError("SpeechKit streams must contain text or bytes");
    }
    const bytes = typeof chunk === "string" ? Buffer.from(chunk) : chunk;
    size += bytes.byteLength;
    if (size > limit) throw new RangeError(`SpeechKit stream exceeds ${limit} bytes`);
    chunks.push(bytes);
  }
  signal?.throwIfAborted();
  return Buffer.concat(chunks);
}
