package io.kombify.speechkit.stt.system

import android.content.Context
import android.speech.SpeechRecognizer
import io.kombify.speechkit.stt.isMultilanguage
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.withContext

/**
 * Zero-config default STT tier (B-M2b): the platform `SpeechRecognizer` behind
 * the [StreamingSttSession] contract. No server, no keys — dictation works out
 * of the box, offline where the device has an on-device recognizer.
 *
 * Differences from the transport tiers, all captured by the contract:
 * - [capturesOwnAudio] is true: the platform service owns the microphone.
 *   Callers must not run their own AudioRecord (it would race the recognizer
 *   for the mic) and [sendAudio] is a no-op.
 * - Sessionless: there is no idle watchdog, so the [keepAlive] default
 *   (return true, do nothing) applies as-is.
 * - The recognizer auto-endpoints: a segment may complete on its own after a
 *   silence window, without [finishSegment] — `Final` + `SegmentDone` then
 *   arrive while the caller still believes the mic is open. Consumers already
 *   handle that ordering (it is the same "final while Listening" path).
 *
 * Event mapping: `onPartialResults` → [TranscriptEvent.Draft], `onResults` →
 * [TranscriptEvent.Final] + [TranscriptEvent.SegmentDone]. `ERROR_NO_MATCH`
 * and `ERROR_SPEECH_TIMEOUT` (the user said nothing) map to an EMPTY Final +
 * SegmentDone instead of a Failure — silence must not raise an error chip.
 * Other errors map to stable failure codes, see [failureCode].
 *
 * Threading: every SpeechRecognizer call must happen on the main looper, so
 * all session state is confined to [mainDispatcher]
 * (`Dispatchers.Main.immediate` in production; recognition callbacks already
 * arrive there). One segment at a time; [startSegment] while a segment is
 * active throws.
 */
class SystemSpeechRecognizerSession internal constructor(
    private val handleFactory: SpeechRecognizerHandle.Factory,
    private val preferOffline: Boolean,
    private val mainDispatcher: CoroutineDispatcher,
) : StreamingSttSession {

    constructor(context: Context, preferOffline: Boolean = true) : this(
        handleFactory = AndroidSpeechRecognizerHandle.factory(context),
        preferOffline = preferOffline,
        mainDispatcher = Dispatchers.Main.immediate,
    )

    override val capturesOwnAudio: Boolean get() = true

    private val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
    override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

    // All mutable state below is confined to [mainDispatcher].
    private var handle: SpeechRecognizerHandle? = null
    private var segmentActive = false
    private var closed = false
    private var segmentId = 0L
    private var sequence = 0L

    override suspend fun startSegment(options: DictationSegmentOptions): Unit =
        withContext(mainDispatcher) {
            check(!closed) { "session is closed" }
            check(!segmentActive) { "a segment is already active" }
            val open = handle ?: runCatching { handleFactory.create() }
                .onSuccess { handle = it }
                .getOrElse { error ->
                    // Surfaced as an event, not a throw: startSegment callers
                    // (the panel FSM) route Failures through their error
                    // state; an exception here would escape their launch.
                    channel.trySend(
                        TranscriptEvent.Failure(
                            code = ERROR_RECOGNIZER_UNAVAILABLE,
                            message = error.message ?: "speech recognizer unavailable",
                        ),
                    )
                    return@withContext
                }
            segmentId += 1
            sequence = 0
            segmentActive = true
            open.startListening(
                RecognitionRequest(
                    languageTag = toLanguageTag(options.language),
                    partialResults = options.interimResults,
                    preferOffline = preferOffline,
                ),
                SegmentCallbacks(segmentId),
            )
        }

    /** No-op: the platform recognizer records the microphone itself. */
    override suspend fun sendAudio(pcm: ByteArray) = Unit

    override suspend fun finishSegment(): Unit = withContext(mainDispatcher) {
        // stopListening flushes capture; the pending Final + SegmentDone (or
        // the empty-Final no-match path) still arrive via the callbacks. If
        // the recognizer already auto-endpointed, there is nothing to stop.
        if (!segmentActive) return@withContext
        handle?.stopListening()
    }

    override suspend fun close(): Unit = withContext(mainDispatcher) {
        if (closed) return@withContext
        closed = true
        segmentActive = false
        handle?.let { open ->
            runCatching { open.cancel() }
            runCatching { open.destroy() }
        }
        handle = null
        // Mirrors the transport tiers' StreamEndReasons.CLIENT.
        channel.trySend(TranscriptEvent.Closed(CLOSE_REASON_CLIENT))
        channel.close()
    }

    override fun toString(): String =
        "SystemSpeechRecognizerSession(preferOffline=$preferOffline)"

    /**
     * Bound to one segment id: the platform recognizer can deliver late
     * callbacks after cancel/close or after the segment already terminated
     * (e.g. an error trailing onResults) — those must not leak into the next
     * segment's event stream.
     */
    private inner class SegmentCallbacks(private val id: Long) : SpeechRecognizerHandle.Callbacks {

        private fun stale(): Boolean = closed || !segmentActive || id != segmentId

        override fun onReady() {
            if (stale()) return
            channel.trySend(TranscriptEvent.SegmentReady(id))
        }

        override fun onPartial(text: String) {
            if (stale() || text.isEmpty()) return
            channel.trySend(TranscriptEvent.Draft(id, text, sequence++))
        }

        override fun onResult(text: String) {
            if (stale()) return
            segmentActive = false
            channel.trySend(TranscriptEvent.Final(id, text, sequence++))
            channel.trySend(TranscriptEvent.SegmentDone(id))
        }

        override fun onError(errorCode: Int) {
            if (stale()) return
            segmentActive = false
            when (errorCode) {
                SpeechRecognizer.ERROR_NO_MATCH,
                SpeechRecognizer.ERROR_SPEECH_TIMEOUT,
                -> {
                    // Silence / nothing recognized: an empty Final, not a
                    // Failure — the caller commits "" (clearing any draft)
                    // and returns to idle without an error chip.
                    channel.trySend(TranscriptEvent.Final(id, "", sequence++))
                    channel.trySend(TranscriptEvent.SegmentDone(id))
                }

                else -> channel.trySend(
                    TranscriptEvent.Failure(
                        code = failureCode(errorCode),
                        message = "system speech recognizer error $errorCode",
                    ),
                )
            }
        }
    }

    companion object {
        /** No recognition service exists / recognizer creation failed. */
        const val ERROR_RECOGNIZER_UNAVAILABLE = "recognizer_unavailable"

        internal const val CLOSE_REASON_CLIENT = "client"

        /**
         * Translates SpeechKit's language value into what the Android platform
         * recognizer wants: a BCP-47 tag. Full tags pass through unchanged and
         * bare ISO 639-1 codes are expanded.
         *
         * Returns null for multilanguage, which is this provider's native way
         * of expressing it: with no `EXTRA_LANGUAGE` the recognizer follows the
         * device's own language configuration instead of being pinned. Passing
         * the literal `multi` through would set an invalid tag.
         */
        internal fun toLanguageTag(language: String?): String? {
            if (isMultilanguage(language)) return null
            val trimmed = language!!.trim().replace('_', '-')
            if (trimmed.contains('-')) return trimmed
            return when (trimmed.lowercase()) {
                "de" -> "de-DE"
                "en" -> "en-US"
                else -> trimmed
            }
        }

        /**
         * Stable failure codes for [TranscriptEvent.Failure]. Permission and
         * audio codes reuse the panel-level vocabulary
         * (`mic_permission_denied`, `audio_capture_failed`) so existing error
         * labels apply.
         */
        internal fun failureCode(errorCode: Int): String = when (errorCode) {
            SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS -> "mic_permission_denied"

            SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED,
            SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE,
            -> "language_not_supported"

            SpeechRecognizer.ERROR_NETWORK,
            SpeechRecognizer.ERROR_NETWORK_TIMEOUT,
            SpeechRecognizer.ERROR_SERVER,
            SpeechRecognizer.ERROR_SERVER_DISCONNECTED,
            -> "network"

            SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> "busy"

            SpeechRecognizer.ERROR_AUDIO -> "audio_capture_failed"

            else -> "recognizer_error"
        }
    }
}
