package io.kombify.speechkit.assistant.service

import android.app.assist.AssistContent
import android.app.assist.AssistStructure
import android.content.Context
import android.os.Bundle
import android.service.voice.VoiceInteractionSession
import android.service.voice.VoiceInteractionSessionService
import android.speech.tts.TextToSpeech
import android.view.View
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.ComposeView
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.assistant.intent.AssistantIntent
import io.kombify.speechkit.assistant.intent.GeneralQueryExecutor
import io.kombify.speechkit.assistant.intent.IntentRouter
import io.kombify.speechkit.assistant.ui.AssistantOverlay
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.domain.describe
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.DictationController
import io.kombify.speechkit.net.SpeechKitApiException
import io.kombify.speechkit.ui.ServiceWindowOwner
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Session service that creates assistant sessions.
 *
 * Android creates a new VoiceInteractionSession per activation. The runtime
 * dependencies are injected here, into the service, and handed to the session
 * through its constructor — a [VoiceInteractionSession] is constructed by us,
 * not by the framework, so it needs no injection entry point of its own.
 */
@AndroidEntryPoint
class SpeechKitAssistantSessionService : VoiceInteractionSessionService() {

    @Inject lateinit var profileSource: ConnectionProfileSource

    override fun onNewSession(args: Bundle?): VoiceInteractionSession =
        SpeechKitVoiceSession(this, profileSource)
}

/**
 * Active voice interaction session.
 *
 * 1. Show overlay UI
 * 2. Transcribe via the same [DictationController] the keyboard uses
 * 3. Classify intent via [IntentRouter]
 * 4. Execute action
 * 5. Respond
 * 6. Close or continue conversation
 *
 * The session receives context about what the user is currently doing
 * (foreground app, screen content) via [onHandleAssist].
 */
class SpeechKitVoiceSession(
    context: Context,
    private val profileSource: ConnectionProfileSource,
) : VoiceInteractionSession(context) {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private val intentRouter = IntentRouter(GeneralQueryExecutor(profileSource))
    private var listeningJob: Job? = null
    private var tts: TextToSpeech? = null

    private val _uiState = MutableStateFlow<AssistantUiState>(AssistantUiState.Idle)
    val uiState: StateFlow<AssistantUiState> = _uiState.asStateFlow()

    /**
     * Compose needs lifecycle, ViewModel-store and saved-state owners from the
     * view tree. Nobody attaches them in a service-owned window, so the same
     * helper the voice IME uses supplies them here.
     */
    private var windowOwner: ServiceWindowOwner? = null

    override fun onCreateContentView(): View {
        val owner = ServiceWindowOwner()
        windowOwner = owner
        return ComposeView(context).also { view ->
            owner.attachTo(view)
            view.setContent {
                val state by uiState.collectAsState()
                AssistantOverlay(
                    state = state,
                    onRetry = ::startListening,
                    onDismiss = ::hide,
                )
            }
        }
    }

    override fun onShow(args: Bundle?, showFlags: Int) {
        super.onShow(args, showFlags)
        VoiceLog.i(VoiceLog.ASSIST, "session started flags=$showFlags")

        windowOwner?.onResume()
        setUiEnabled(true)
        if (tts == null) {
            tts = TextToSpeech(context) { status ->
                if (status != TextToSpeech.SUCCESS) {
                    VoiceLog.w(VoiceLog.ASSIST, "system TTS unavailable status=$status")
                }
            }
        }
        startListening()
    }

    override fun onHandleAssist(
        data: Bundle?,
        structure: AssistStructure?,
        content: AssistContent?,
    ) {
        val packageName = structure?.activityComponent?.packageName
        val webUri = content?.webUri?.toString()

        VoiceLog.i(VoiceLog.ASSIST, "context app=$packageName")

        intentRouter.setContext(
            foregroundApp = packageName,
            webUri = webUri,
        )
    }

    override fun onHide() {
        listeningJob?.cancel()
        windowOwner?.onPause()
        _uiState.value = AssistantUiState.Idle
        VoiceLog.i(VoiceLog.ASSIST, "session hidden")
        super.onHide()
    }

    override fun onDestroy() {
        scope.cancel()
        tts?.stop()
        tts?.shutdown()
        tts = null
        windowOwner?.onDestroy()
        windowOwner = null
        super.onDestroy()
    }

    private fun startListening() {
        listeningJob?.cancel()
        listeningJob = scope.launch {
            try {
                val profile = profileSource.currentProfile()
                VoiceLog.i(VoiceLog.ASSIST, "listen ${profile.describe()}")
                _uiState.value = AssistantUiState.Listening()

                val outcome = UtteranceTranscriber(
                    sessionFactory = {
                        DictationController(profile, context = context).openSession()
                    },
                    audioCapture = MicAudioCapture(),
                    onLevel = { level ->
                        _uiState.value = AssistantUiState.Listening(level)
                    },
                ).transcribe()

                if (outcome.reason != UtteranceResult.Reason.HEARD) {
                    _uiState.value = AssistantUiState.Error(messageFor(outcome))
                    return@launch
                }
                _uiState.value = AssistantUiState.Transcribed(outcome.text)

                val intent = intentRouter.classify(outcome.text)
                VoiceLog.i(VoiceLog.ASSIST, "intent ${intent.type} confidence=${intent.confidence}")
                executeIntent(intent)
            } catch (e: SpeechKitApiException) {
                VoiceLog.e(
                    VoiceLog.ASSIST,
                    "listen mint failed http=${e.httpStatus} code=${e.code}",
                    e,
                )
                _uiState.value = AssistantUiState.Error("Server ${e.httpStatus} (${e.code})")
            } catch (e: Exception) {
                VoiceLog.e(VoiceLog.ASSIST, "listen failed", e)
                _uiState.value = AssistantUiState.Error(e.message ?: GENERIC_ERROR_MESSAGE)
            }
        }
    }

    private suspend fun executeIntent(intent: AssistantIntent) {
        _uiState.value = AssistantUiState.Executing(intent.type.displayName)

        val result = intentRouter.execute(context, intent)

        _uiState.value = if (result.success) {
            AssistantUiState.Result(result.responseText)
        } else {
            VoiceLog.w(VoiceLog.ASSIST, "intent failed ${intent.type}")
            AssistantUiState.Error(result.errorMessage ?: ACTION_FAILED_MESSAGE)
        }

        val spoken = result.responseText.trim()
        if (result.success && spoken.isNotEmpty()) {
            tts?.speak(spoken, TextToSpeech.QUEUE_FLUSH, null, "assist-reply")
        }

        if (result.success && !result.keepOpen) {
            delay(RESULT_LINGER_MILLIS)
            hide()
        }
    }

    private companion object {
        const val RESULT_LINGER_MILLIS = 2_000L

        // TODO(i18n): replace with sk.voice.* message IDs once the assistant
        // adopts the shared catalog; locales/*.json is the parity source.
        const val NO_SPEECH_MESSAGE = "Keine Sprache erkannt"
        const val TIMEOUT_MESSAGE = "Transkript kam nicht rechtzeitig"
        const val CLOSED_MESSAGE = "Verbindung beendet"
        const val STREAM_FAILED_MESSAGE = "Spracherkennung fehlgeschlagen"
        const val ACTION_FAILED_MESSAGE = "Aktion fehlgeschlagen"
        const val GENERIC_ERROR_MESSAGE = "Fehler"

        fun messageFor(outcome: UtteranceResult): String = when (outcome.reason) {
            UtteranceResult.Reason.HEARD -> outcome.text
            UtteranceResult.Reason.EMPTY_FINAL,
            UtteranceResult.Reason.NO_SPEECH,
            -> NO_SPEECH_MESSAGE
            UtteranceResult.Reason.TIMEOUT -> TIMEOUT_MESSAGE
            UtteranceResult.Reason.CLOSED -> CLOSED_MESSAGE
            UtteranceResult.Reason.STREAM_FAILED ->
                outcome.detail?.takeIf { it.isNotBlank() }?.let { "$STREAM_FAILED_MESSAGE ($it)" }
                    ?: STREAM_FAILED_MESSAGE
        }
    }
}

/** UI state for the assistant overlay. */
sealed interface AssistantUiState {
    data object Idle : AssistantUiState

    /** @param level smoothed 0..1 microphone level driving the orb's halo. */
    data class Listening(val level: Float = 0f) : AssistantUiState
    data object Processing : AssistantUiState
    data class Transcribed(val text: String) : AssistantUiState
    data class Executing(val actionName: String) : AssistantUiState
    data class Result(val text: String) : AssistantUiState
    data class Error(val message: String) : AssistantUiState
}
