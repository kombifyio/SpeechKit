import { openAsBlob } from "node:fs";
import { createWriteStream } from "node:fs";
import { basename } from "node:path";
import { pipeline } from "node:stream/promises";
import { Agent } from "@mastra/core/agent";
import { SpeechKitClient } from "@kombifyio/speechkit-client";
import { SpeechKitVoice } from "@kombifyio/speechkit-client/mastra";

const [input, output = "reply.wav"] = process.argv.slice(2);
if (!input) throw new Error("Usage: node mastra-voice.mjs <recording.wav> [reply.wav]");

const client = new SpeechKitClient({
  baseUrl: process.env.SPEECHKIT_SERVER_URL ?? "http://localhost:8080",
  basePath: process.env.SPEECHKIT_BASE_PATH ?? "/v1",
  token: process.env.SPEECHKIT_TOKEN ?? "",
});
const voice = new SpeechKitVoice({ client });
const agent = new Agent({
  id: "speechkit-example",
  name: "SpeechKit assistant",
  instructions: "Answer the user briefly in the language they use.",
  model: process.env.MASTRA_MODEL ?? "openai/gpt-4.1-mini",
  voice,
});

const audio = await openAsBlob(input, { type: "audio/wav" });
const transcript = await voice.listen(audio, { filename: basename(input) });
const response = await agent.generate(transcript);
const speech = await voice.speak(response.text, { format: "wav" });
if (speech.format !== "wav") {
  speech.destroy();
  throw new Error(`The configured TTS provider returned ${speech.format}; choose a provider that supports WAV.`);
}
await pipeline(speech, createWriteStream(output));
console.log(`Saved speech to ${output}`);
