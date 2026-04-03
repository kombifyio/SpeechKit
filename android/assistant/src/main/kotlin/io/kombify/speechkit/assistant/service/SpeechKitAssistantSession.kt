package io.kombify.speechkit.assistant.service

import android.app.assist.AssistContent
import android.app.assist.AssistStructure
import android.content.Context
import android.os.Bundle
import android.service.voice.VoiceInteractionSession
import android.service.voice.VoiceInteractionSessionService
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.assistant.intent.IntentRouter
import io.kombify.speechkit.assistant.intent.AssistantIntent
import io.kombify.speechkit.audio.AudioSession
import io.kombify.speechkit.stt.SttRouter
import io.kombify.speechkit.stt.TranscribeOpts
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import timber.log.Timber
import javax.inject.Inject

/**
 * Session service that creates assistant sessions.
 *
 * Android creates a new VoiceInteractionSession for each assistant activation.
 * We inject our dependencies and pass them to the session.
 */
@AndroidEntryPoint
class SpeechKitAssistantSessionService : VoiceInteractionSessionService() {

    @Inject
    lateinit var audioSession: AudioSession

    @Inject
    lateinit var sttRouter: SttRouter

    override fun onNewSession(args: Bundle?): VoiceInteractionSession {
        return SpeechKitVoiceSession(
            context = this,
            audioSession = audioSession,
            sttRouter = sttRouter,
        )
    }
}

/**
 * Active voice interaction session.
 *
 * Manages the full lifecycle of a single assistant interaction:
 * 1. Show overlay UI
 * 2. Listen for voice input
 * 3. Transcribe via SttRouter
 * 4. Classify intent via IntentRouter
 * 5. Execute action
 * 6. Respond (TTS or visual)
 * 7. Close or continue conversation
 *
 * The session receives context about what the user is currently doing
 * (foreground app, screen content) via onHandleAssist/onHandleScreenshot.
 */
class SpeechKitVoiceSession(
    context: Context,
    private val audioSession: AudioSession,
    private val sttRouter: SttRouter,
) : VoiceInteractionSession(context) {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private val intentRouter = IntentRouter()
    private var listeningJob: Job? = null

    override fun onShow(args: Bundle?, showFlags: Int) {
        super.onShow(args, showFlags)
        Timber.d("Assistant session started (flags=$showFlags)")

        // Show the assistant overlay UI
        setUiEnabled(true)

        // Auto-start listening
        startListening()
    }

    @Deprecated("Framework callback remains deprecated but is still required for assist context delivery.")
    override fun onHandleAssist(
        data: Bundle?,
        structure: AssistStructure?,
        content: AssistContent?,
    ) {
        // Receive context about the current screen
        val packageName = structure?.activityComponent?.packageName
        val webUri = content?.webUri?.toString()

        Timber.d("Assist context: app=$packageName, uri=$webUri")

        // Store context for intent resolution
        intentRouter.setContext(
            foregroundApp = packageName,
            webUri = webUri,
        )
    }

    override fun onHide() {
        listeningJob?.cancel()
        scope.launch {
            audioSession.stop()
        }
        Timber.d("Assistant session hidden")
        super.onHide()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun startListening() {
        listeningJob = scope.launch {
            try {
                Timber.d("Assistant: listening...")
                updateUiState(AssistantUiState.Listening)

                audioSession.start()

                // Collect audio for a reasonable duration (up to 10 seconds)
                // VAD would handle this in production
                kotlinx.coroutines.delay(5000)

                val pcmData = audioSession.stop()
                if (pcmData.isEmpty()) {
                    updateUiState(AssistantUiState.Error("Keine Sprache erkannt"))
                    return@launch
                }

                // Transcribe
                updateUiState(AssistantUiState.Processing)

                val result = sttRouter.route(
                    audio = pcmData,
                    durationSecs = pcmData.size.toDouble() / (16000 * 2),
                    opts = TranscribeOpts(language = "de"),
                )

                Timber.d("Assistant heard: '${result.text}'")
                updateUiState(AssistantUiState.Transcribed(result.text))

                // Classify intent
                val intent = intentRouter.classify(result.text)
                Timber.d("Intent: ${intent.type} (confidence=${intent.confidence})")

                // Execute
                executeIntent(intent)

            } catch (e: Exception) {
                Timber.e(e, "Assistant listening failed")
                updateUiState(AssistantUiState.Error(e.message ?: "Fehler"))
            }
        }
    }

    private suspend fun executeIntent(intent: AssistantIntent) {
        updateUiState(AssistantUiState.Executing(intent.type.displayName))

        val result = intentRouter.execute(context, intent)

        updateUiState(
            if (result.success) {
                AssistantUiState.Result(result.responseText)
            } else {
                AssistantUiState.Error(result.errorMessage ?: "Aktion fehlgeschlagen")
            }
        )

        // Auto-close after showing result
        if (result.success && !result.keepOpen) {
            kotlinx.coroutines.delay(2000)
            hide()
        }
    }

    private fun updateUiState(state: AssistantUiState) {
        // In production, this updates the Compose overlay UI.
        // For now, just log.
        Timber.d("UI State: $state")
    }
}

/** UI state for the assistant overlay. */
sealed interface AssistantUiState {
    data object Idle : AssistantUiState
    data object Listening : AssistantUiState
    data object Processing : AssistantUiState
    data class Transcribed(val text: String) : AssistantUiState
    data class Executing(val actionName: String) : AssistantUiState
    data class Result(val text: String) : AssistantUiState
    data class Error(val message: String) : AssistantUiState
}
