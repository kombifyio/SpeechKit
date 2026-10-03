package io.kombify.speechkit.coinstall.voiceagent.v1

/** Wire constants for `speechkit.coinstall.voiceagent.v1`. Both apps compile this. */
object VoiceAgentContract {
    const val VERSION: Int = 2
    const val VISIBLE_START_VERSION: Int = 2
    const val BIND_ACTION: String = "io.kombify.speechkit.voiceagent.v1.BIND"

    /** SpeechKit app ids that may host the service (store and oss flavors). */
    val SPEECHKIT_PACKAGES: List<String> = listOf("io.kombify.speechkit", "io.kombify.speechkit.oss")

    /** Companion app ids that may call it (release and preview). */
    val COMPANION_PACKAGES: List<String> = listOf("io.kombify.companion", "io.kombify.companion.preview")

    const val STATE_CONNECTING = "connecting"
    const val STATE_LISTENING = "listening"
    const val STATE_THINKING = "thinking"
    const val STATE_SPEAKING = "speaking"

    const val ROLE_USER = 0
    const val ROLE_AGENT = 1

    const val ERROR_CALLER_NOT_ATTESTED = "caller_not_attested"
    const val ERROR_HOSTED_ORIGIN_UNAVAILABLE = "hosted_origin_unavailable"
    const val ERROR_MICROPHONE_PERMISSION = "microphone_permission_missing"
    const val ERROR_TICKET_INVALID = "ticket_invalid"
    const val ERROR_SESSION_BUSY = "session_busy"
    const val ERROR_TRANSPORT = "transport_failed"
    const val ERROR_TURN_FAILED = "turn_failed"
    const val ERROR_SERVER = "server_error"
}
