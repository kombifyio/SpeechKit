package io.kombify.speechkit.stt.system

/**
 * Platform-neutral mirror of the RecognizerIntent extras
 * [SystemSpeechRecognizerSession] sets per segment.
 */
data class RecognitionRequest(
    /** BCP-47 tag for `RecognizerIntent.EXTRA_LANGUAGE`; null = device default. */
    val languageTag: String? = null,
    /** `RecognizerIntent.EXTRA_PARTIAL_RESULTS` — live drafts. */
    val partialResults: Boolean = true,
    /**
     * `RecognizerIntent.EXTRA_PREFER_OFFLINE` on the pre-31 fallback path.
     * The API 31+ on-device recognizer is offline by construction.
     */
    val preferOffline: Boolean = true,
)

/**
 * Thin seam over `android.speech.SpeechRecognizer` so the session FSM in
 * [SystemSpeechRecognizerSession] stays a plain-JVM unit-testable class (this
 * repo deliberately runs no Robolectric). The real implementation is
 * [AndroidSpeechRecognizerHandle]; tests fake this interface.
 *
 * Threading contract: every method is called on the main thread (the
 * SpeechRecognizer requirement), and [Callbacks] arrive on the main thread.
 */
interface SpeechRecognizerHandle {
    /** Begins one recognition segment. */
    fun startListening(request: RecognitionRequest, callbacks: Callbacks)

    /** Ends audio capture; pending results still arrive via [Callbacks]. */
    fun stopListening()

    /** Drops the active segment without delivering results. */
    fun cancel()

    /** Releases the platform recognizer. */
    fun destroy()

    /**
     * Recognition callbacks with their Bundles already unpacked — Bundle is
     * not constructible in plain-JVM unit tests, so it must not cross this
     * seam.
     */
    interface Callbacks {
        /** Maps `onReadyForSpeech`: the recognizer is capturing audio. */
        fun onReady()

        /** Best partial hypothesis from `onPartialResults` (never empty). */
        fun onPartial(text: String)

        /** Best final hypothesis from `onResults` (may be empty). */
        fun onResult(text: String)

        /** `RecognitionListener.onError` code (`SpeechRecognizer.ERROR_*`). */
        fun onError(errorCode: Int)
    }

    /** Called on the main thread; may throw when no recognizer exists. */
    fun interface Factory {
        fun create(): SpeechRecognizerHandle
    }
}
