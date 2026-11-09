package io.kombify.speechkit.assistant.service

import java.util.Locale

/**
 * An optional on-device voice that speaks the assistant's replies. The platform text-to-speech engine stays the
 * baseline; this is tried first and gives way to it whenever it cannot speak.
 *
 * The implementation lives in the app module because its runtime is a file dependency a library module cannot
 * carry; the assistant only needs this seam.
 */
interface NeuralReplyVoice {
    /**
     * Speaks [text] in the voice that fits [locale] and suspends until it has finished or been [stop]ped.
     *
     * Returns false, having made no sound, when the neural voice cannot speak this reply (no voice downloaded for
     * the locale, the voice failed to load, the device cannot run it); the caller then speaks it through the
     * platform engine. A cancelled call stops its audio and rethrows.
     */
    suspend fun speak(text: String, locale: Locale): Boolean

    /** Cuts off the reply being spoken. */
    fun stop()

    companion object {
        /** A voice that never speaks: every reply goes to the platform engine. */
        val None: NeuralReplyVoice = object : NeuralReplyVoice {
            override suspend fun speak(text: String, locale: Locale): Boolean = false
            override fun stop() = Unit
        }
    }
}
