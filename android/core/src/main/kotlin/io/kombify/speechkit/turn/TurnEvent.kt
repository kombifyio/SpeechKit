package io.kombify.speechkit.turn

/**
 * What [TurnEngine] decided about the microphone stream.
 *
 * A surface renders these and forwards [TurnAudio]; it never times anything
 * itself. There is deliberately no event that asks the consumer a question —
 * every member is a decision that has already been made.
 */
sealed interface TurnEvent {

    /**
     * Smoothed input level in 0..1 for a meter or the aura orb, rate-limited
     * by the engine. Emitted whether or not a turn is open, so a hands-free
     * surface can show that the microphone is live between turns.
     */
    data class InputLevel(val level: Float) : TurnEvent

    /**
     * Sustained speech was confirmed and the user's turn is now open.
     *
     * When [bargeIn] is true the user started while agent audio was still
     * audible: the host MUST cut playback (flush the player and drop queued
     * agent PCM). The engine has already stopped treating the speaker as
     * audible, so failing to cut leaves the engine's echo guard disarmed for
     * the rest of that playback.
     */
    data class SpeechStarted(val bargeIn: Boolean) : TurnEvent

    /**
     * PCM belonging to the open turn, in capture order.
     *
     * The first one after [SpeechStarted] carries the pre-roll — the audio
     * captured *before* onset was confirmed — so the beginning of the sentence
     * is part of the turn rather than the cost of detecting it. Forward every
     * one of these to the session; never send raw capture frames instead.
     *
     * Not a `data class`: it wraps a `ByteArray`, whose structural equality is
     * identity, so a generated `equals` would lie.
     */
    class TurnAudio(val pcm: ByteArray) : TurnEvent {
        override fun toString(): String = "TurnAudio(${pcm.size} bytes)"
    }

    /** The user's turn is over. [capturedMillis] counts from the pre-roll. */
    data class TurnEnded(val reason: TurnEndReason, val capturedMillis: Long) : TurnEvent

    /**
     * Audio during agent playback was loud enough to be a barge-in candidate
     * but did not qualify as speech. Emitted once per burst, not per frame.
     *
     * Diagnostic rather than a state change: the agent keeps talking. A
     * surface that shows nothing for this is behaving correctly; a device log
     * full of them means the echo guard is fighting the room.
     */
    data class BargeInRejected(val reason: BargeInRejection) : TurnEvent
}

/** Why a turn closed. */
enum class TurnEndReason {
    /** The engine endpointed on trailing silence. */
    Silence,

    /** The provider's own turn detection closed it; see `TurnPolicy`. */
    Provider,

    /** The host stopped the session or cancelled the turn. */
    Host,

    /** Safety cap: a turn cannot run forever if endpointing never fires. */
    MaxDuration,
}

/** Why a barge-in candidate was refused. */
enum class BargeInRejection {
    /**
     * The candidate ended before the confirmation window completed — a door,
     * a bang, a single loud frame.
     */
    TooShort,

    /**
     * The candidate stayed loud long enough but its envelope was flat, so it
     * is a steady source (fan, hum, road, air conditioning) rather than
     * speech. This is the check that keeps ambient noise from interrupting
     * the agent.
     */
    NotSpeech,
}
