// Apache-2.0. Implemented by Companion; SpeechKit reports the hosted session.
package io.kombify.speechkit.coinstall.voiceagent.v1;

oneway interface IVoiceAgentCallback {
    /** One of VoiceAgentContract.STATE_*. */
    void onState(String sessionId, String state);

    /** role is VoiceAgentContract.ROLE_USER or ROLE_AGENT. */
    void onTranscript(String sessionId, int role, String text, boolean isFinal);

    /** code is one of VoiceAgentContract.ERROR_*; a non-fatal error keeps the session. */
    void onError(String sessionId, String code, String message, boolean fatal);

    /** Terminal. reason mirrors the server session_end reason or "client". */
    void onEnded(String sessionId, String reason);
}
