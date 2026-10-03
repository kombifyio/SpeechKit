// Apache-2.0. Compiled by both apps; implemented only by SpeechKit.
package io.kombify.speechkit.coinstall.voiceagent.v1;

import io.kombify.speechkit.coinstall.voiceagent.v1.IVoiceAgentCallback;
import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentCapability;
import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentSessionRequest;
import android.app.PendingIntent;

/**
 * speechkit.coinstall.voiceagent.v1: the Companion hands a hosted Voice Agent
 * session to the co-installed SpeechKit app, which owns native capture and
 * playback. The Companion mints the session through the Gateway with its own
 * sign-in and passes only the one-time session ticket; no bearer crosses.
 */
interface IVoiceAgentService {
    /** Contract version the callee implements. Callers must not assume. */
    int getContractVersion();

    /** Cheap, synchronous. Never prompts. */
    VoiceAgentCapability getCapability();

    /**
     * Opens the ticket WebSocket and runs the session. Exactly one terminal
     * onEnded follows, also after a fatal onError. A caller that fails the
     * identity check receives onError(caller_not_attested, fatal) and onEnded.
     */
    void startSession(in VoiceAgentSessionRequest request, in IVoiceAgentCallback callback);

    /** Ends the session. Safe to call after it has already ended. */
    void stopSession(String sessionId);
    /** Cuts the current reply without replacing the account-bound hosted session. */
    void interruptSession(String sessionId);

    /**
     * Version 2: reserves an attested request without opening audio. The visible
     * caller sends this immutable, one-shot foreground-service intent. Tickets
     * remain in the callee's memory; stopSession also cancels a reservation.
     * A rejected request returns null and receives the usual terminal callback.
     */
    @nullable PendingIntent prepareSession(in VoiceAgentSessionRequest request, in IVoiceAgentCallback callback);
}
