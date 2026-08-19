package io.kombify.speechkit.stt.system

import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer

/**
 * Real [SpeechRecognizerHandle] over `android.speech.SpeechRecognizer`.
 *
 * API gates:
 * - **31+**: `createOnDeviceSpeechRecognizer` when
 *   `isOnDeviceRecognitionAvailable` — a fully offline recognizer.
 * - **26–30** (minSdk 26), or 31+ without an on-device recognizer:
 *   `createSpeechRecognizer` with `RecognizerIntent.EXTRA_PREFER_OFFLINE`,
 *   which asks the default service for its offline path but cannot
 *   guarantee one.
 *
 * All methods must run on the main thread (SpeechRecognizer contract);
 * [SystemSpeechRecognizerSession] confines itself to `Dispatchers.Main`.
 * A missing RECORD_AUDIO grant is not checked here — it surfaces as
 * `ERROR_INSUFFICIENT_PERMISSIONS` and maps to a stable failure code.
 */
internal class AndroidSpeechRecognizerHandle private constructor(
    private val recognizer: SpeechRecognizer,
) : SpeechRecognizerHandle {

    override fun startListening(
        request: RecognitionRequest,
        callbacks: SpeechRecognizerHandle.Callbacks,
    ) {
        recognizer.setRecognitionListener(ListenerAdapter(callbacks))
        recognizer.startListening(recognitionIntent(request))
    }

    override fun stopListening() = recognizer.stopListening()

    override fun cancel() = recognizer.cancel()

    override fun destroy() = recognizer.destroy()

    /** Unpacks the listener Bundles so no Bundle crosses the testable seam. */
    private class ListenerAdapter(
        private val callbacks: SpeechRecognizerHandle.Callbacks,
    ) : RecognitionListener {
        override fun onReadyForSpeech(params: Bundle?) = callbacks.onReady()

        override fun onPartialResults(partialResults: Bundle?) {
            val text = firstResult(partialResults)
            if (text.isNotEmpty()) callbacks.onPartial(text)
        }

        override fun onResults(results: Bundle?) = callbacks.onResult(firstResult(results))

        override fun onError(error: Int) = callbacks.onError(error)

        override fun onBeginningOfSpeech() = Unit
        override fun onRmsChanged(rmsdB: Float) = Unit
        override fun onBufferReceived(buffer: ByteArray?) = Unit
        override fun onEndOfSpeech() = Unit
        override fun onEvent(eventType: Int, params: Bundle?) = Unit

        private fun firstResult(bundle: Bundle?): String =
            bundle?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
                ?.firstOrNull()
                ?.trim()
                .orEmpty()
    }

    companion object {
        /** Deferred creation: the recognizer is built on first use, on main. */
        fun factory(context: Context): SpeechRecognizerHandle.Factory {
            val appContext = context.applicationContext
            return SpeechRecognizerHandle.Factory {
                AndroidSpeechRecognizerHandle(createBestRecognizer(appContext))
            }
        }

        private fun createBestRecognizer(context: Context): SpeechRecognizer = when {
            Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
                SpeechRecognizer.isOnDeviceRecognitionAvailable(context) ->
                SpeechRecognizer.createOnDeviceSpeechRecognizer(context)

            SpeechRecognizer.isRecognitionAvailable(context) ->
                SpeechRecognizer.createSpeechRecognizer(context)

            else -> throw IllegalStateException(
                "no speech recognition service available on this device",
            )
        }

        private fun recognitionIntent(request: RecognitionRequest): Intent =
            Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                putExtra(
                    RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                    RecognizerIntent.LANGUAGE_MODEL_FREE_FORM,
                )
                putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, request.partialResults)
                request.languageTag?.let { putExtra(RecognizerIntent.EXTRA_LANGUAGE, it) }
                if (request.preferOffline) {
                    // Redundant on the API 31+ on-device recognizer, harmless.
                    putExtra(RecognizerIntent.EXTRA_PREFER_OFFLINE, true)
                }
            }
    }
}
