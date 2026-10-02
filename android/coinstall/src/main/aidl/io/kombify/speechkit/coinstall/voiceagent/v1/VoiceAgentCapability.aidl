package io.kombify.speechkit.coinstall.voiceagent.v1;

parcelable VoiceAgentCapability {
    boolean available;
    /** Empty when available, else one of VoiceAgentContract.ERROR_*. */
    String unavailableReason;
    boolean microphonePermission;
}
