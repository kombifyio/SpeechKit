package io.kombify.speechkit.coinstall.voiceagent.v1;
import android.os.ParcelFileDescriptor;

parcelable VoiceAgentSessionRequest {
    /** Server session id from POST /v1/speechkit/voiceagent/sessions. */
    String sessionId;
    /** Served ws_url; must use the receiving distribution's admitted secure origin. */
    String wsUrl;
    /** One-time upgrade ticket, sent only as Sec-WebSocket-Protocol ticket.<t>. */
    String ticket;
    long ticketExpiresAtEpochMs;
    /** Required durable Companion conversation the registered agent turns belong to. */
    String aiSessionId;
    /** BCP-47 hint; the server detects the spoken language. */
    String locale;
    /**
     * Optional caller-provided audio instead of the microphone: 16 kHz mono
     * PCM16 little-endian, raw or in a WAV container. Used for automated
     * acceptance and audio forwarding; null means microphone capture.
     */
    @nullable ParcelFileDescriptor audioSource;
    /** Optional assemblyai/deepgram native media; requires contract version 3. Null preserves legacy transport. */
    @nullable String mediaProvider;
}
