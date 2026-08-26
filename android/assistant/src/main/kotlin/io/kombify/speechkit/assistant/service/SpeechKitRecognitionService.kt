package io.kombify.speechkit.assistant.service

import android.content.Intent
import android.os.Bundle
import android.speech.RecognitionListener
import android.speech.RecognitionService
import android.speech.SpeechRecognizer

/**
 * Declared so Android will qualify SpeechKit for `ROLE_ASSISTANT`.
 *
 * Without a [RecognitionService] the platform logs "unqualified voice
 * interaction service" and refuses the assistant role, so long-press power
 * never reaches [SpeechKitAssistant]. The overlay session captures through
 * [SpeechKitVoiceSession], not this service. Other apps that ask the default
 * recognizer still need a real implementation, so this forwards to the
 * platform [SpeechRecognizer].
 */
class SpeechKitRecognitionService : RecognitionService() {

    private var recognizer: SpeechRecognizer? = null

    override fun onStartListening(recognizerIntent: Intent, listener: Callback) {
        runCatching { recognizer?.destroy() }
        if (!SpeechRecognizer.isRecognitionAvailable(this)) {
            listener.error(SpeechRecognizer.ERROR_CLIENT)
            return
        }
        val sr = SpeechRecognizer.createSpeechRecognizer(this)
        recognizer = sr
        sr.setRecognitionListener(ForwardingListener(listener))
        sr.startListening(recognizerIntent)
    }

    override fun onCancel(listener: Callback) {
        recognizer?.cancel()
    }

    override fun onStopListening(listener: Callback) {
        recognizer?.stopListening()
    }

    override fun onDestroy() {
        runCatching { recognizer?.destroy() }
        recognizer = null
        super.onDestroy()
    }

    private class ForwardingListener(
        private val callback: Callback,
    ) : RecognitionListener {
        override fun onReadyForSpeech(params: Bundle?) {
            callback.readyForSpeech(params)
        }

        override fun onBeginningOfSpeech() {
            callback.beginningOfSpeech()
        }

        override fun onRmsChanged(rmsdB: Float) {
            callback.rmsChanged(rmsdB)
        }

        override fun onBufferReceived(buffer: ByteArray?) {
            if (buffer != null) callback.bufferReceived(buffer)
        }

        override fun onEndOfSpeech() {
            callback.endOfSpeech()
        }

        override fun onError(error: Int) {
            callback.error(error)
        }

        override fun onResults(results: Bundle?) {
            callback.results(results)
        }

        override fun onPartialResults(partialResults: Bundle?) {
            callback.partialResults(partialResults)
        }

        override fun onEvent(eventType: Int, params: Bundle?) = Unit
    }
}
