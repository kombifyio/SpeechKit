package io.kombify.speechkit.assistant.service

import android.service.voice.VoiceInteractionService
import dagger.hilt.android.AndroidEntryPoint
import timber.log.Timber

/**
 * System-level voice assistant service.
 *
 * This is the entry point when the user activates the assistant via:
 * - Long-pressing the home button
 * - Power button gesture (on supported devices)
 * - "Hey kombify" (future wake word)
 *
 * The service itself is lightweight -- it delegates all session handling
 * to [SpeechKitAssistantSessionService] which manages the actual voice interaction.
 *
 * Android requires both VoiceInteractionService and VoiceInteractionSessionService
 * to be declared in the manifest with BIND_VOICE_INTERACTION permission.
 */
@AndroidEntryPoint
class SpeechKitAssistant : VoiceInteractionService() {

    override fun onReady() {
        super.onReady()
        Timber.d("SpeechKit Assistant ready")
    }

    override fun onShutdown() {
        Timber.d("SpeechKit Assistant shutting down")
        super.onShutdown()
    }
}
