/**
 * Side-effectful entry: registers every kit element (guarded, so double
 * imports are safe). SSR hosts import this client-side only, e.g.
 * `onMount(() => import("@kombifyio/speechkit-voice-ui/define"))`.
 */
import { registerSpeechKitElements } from "./index.js";

registerSpeechKitElements();

// Full API re-export so the CDN bundle's global (`SpeechKitVoiceUi`) carries
// the reducers, i18n, and element classes alongside the registration effect.
export * from "./index.js";
