package io.kombify.speechkit.net

/**
 * The user-facing reason class SpeechKit puts on every voice error frame and
 * classed HTTP error body (`class`, Go `cascaded.ErrorClass`). A surface renders
 * its localized text from the class, never from the server's English message:
 * a used-up limit never reads like a missing right, an outage never like a denial.
 */
enum class VoiceErrorClass(val wire: String) {
    /** A usage limit of the account or device is reached (credit, rate, conversations per device). */
    LIMIT("limit"),

    /** The service or a provider could not answer; trying again later helps. */
    OUTAGE("outage"),

    /** The account, plan or agent lacks the right. */
    RIGHT("right"),

    /** Cloud voice or recording consent is missing. */
    CONSENT("consent"),

    /** The session's authorization ran out; start a new session. */
    EXPIRED("expired"),
    ;

    companion object {
        /** The class a server named, or null when it named none or an unknown one. */
        fun fromWire(value: String?): VoiceErrorClass? = entries.firstOrNull { it.wire == value }
    }
}
