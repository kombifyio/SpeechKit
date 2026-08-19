package io.kombify.speechkit.assistant.service

import android.app.assist.AssistContent
import android.app.assist.AssistStructure
import android.content.Context
import android.os.Bundle
import android.service.voice.VoiceInteractionSession
import android.service.voice.VoiceInteractionSessionService
import android.view.View
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.ComposeView
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.assistant.intent.AssistantIntent
import io.kombify.speechkit.assistant.intent.IntentRouter
import io.kombify.speechkit.assistant.ui.AssistantOverlay
import io.kombify.speechkit.audio.AudioFormat
import io.kombify.speechkit.audio.AudioSession
import io.kombify.speechkit.audio.MicLevelMeter
import io.kombify.speechkit.audio.frameDurationMillis
import io.kombify.speechkit.audio.toPcm16Samples
import io.kombify.speechkit.stt.SttRouter
import io.kombify.speechkit.stt.TranscribeOpts
import io.kombify.speechkit.ui.ServiceWindowOwner
import io.kombify.speechkit.vad.LevelVadDetector
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.takeWhile
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import timber.log.Timber
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

    @Inject lateinit var audioSession: AudioSession

    @Inject lateinit var sttRouter: SttRouter

    override fun onNewSession(args: Bundle?): VoiceInteractionSession =
        SpeechKitVoiceSession(this, audioSession, sttRouter)
}

/**
 * Active voice interaction session.
 *
 * Manages the full lifecycle of a single assistant interaction:
 * 1. Show overlay UI
 * 2. Listen for voice input until the speaker stops
 * 3. Transcribe via [SttRouter]
 * 4. Classify intent via [IntentRouter]
 * 5. Execute action
 * 6. Respond
 * 7. Close or continue conversation
 *
 * The session receives context about what the user is currently doing
 * (foreground app, screen content) via [onHandleAssist].
 */
class SpeechKitVoiceSession(
    context: Context,
    private val audioSession: AudioSession,
    private val sttRouter: SttRouter,
) : VoiceInteractionSession(context) {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private val intentRouter = IntentRouter()
    private var listeningJob: Job? = null

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
        Timber.d("Assistant session started (flags=$showFlags)")

        windowOwner?.onResume()
        setUiEnabled(true)
        startListening()
    }

    override fun onHandleAssist(
        data: Bundle?,
        structure: AssistStructure?,
        content: AssistContent?,
    ) {
        val packageName = structure?.activityComponent?.packageName
        val webUri = content?.webUri?.toString()

        Timber.d("Assist context: app=$packageName, uri=$webUri")

        intentRouter.setContext(
            foregroundApp = packageName,
            webUri = webUri,
        )
    }

    override fun onHide() {
        listeningJob?.cancel()
        scope.launch { audioSession.stop() }
        windowOwner?.onPause()
        _uiState.value = AssistantUiState.Idle
        Timber.d("Assistant session hidden")
        super.onHide()
    }

    override fun onDestroy() {
        scope.cancel()
        windowOwner?.onDestroy()
        windowOwner = null
        super.onDestroy()
    }

    private fun startListening() {
        listeningJob?.cancel()
        listeningJob = scope.launch {
            try {
                Timber.d("Assistant: listening...")
                _uiState.value = AssistantUiState.Listening()

                audioSession.start()
                val heardSpeech = captureUntilEndpoint()
                val pcmData = audioSession.stop()

                if (!heardSpeech || pcmData.isEmpty()) {
                    _uiState.value = AssistantUiState.Error(NO_SPEECH_MESSAGE)
                    return@launch
                }

                _uiState.value = AssistantUiState.Processing

                val result = sttRouter.route(
                    audio = pcmData,
                    durationSecs = pcmData.size.toDouble() /
                        (AudioFormat.SAMPLE_RATE * AudioFormat.BYTES_PER_SAMPLE),
                    // No language is pinned: SpeechKit stays multilanguage so the
                    // speaker may switch language mid-session. See TranscribeOpts.
                    opts = TranscribeOpts(),
                )

                Timber.d("Assistant heard: '${result.text}'")
                if (result.text.isBlank()) {
                    // An empty final is a real outcome, not a non-event: it must
                    // never vanish silently.
                    Timber.w("Assistant received an empty transcript from ${result.provider}")
                    _uiState.value = AssistantUiState.Error(NO_SPEECH_MESSAGE)
                    return@launch
                }
                _uiState.value = AssistantUiState.Transcribed(result.text)

                val intent = intentRouter.classify(result.text)
                Timber.d("Intent: ${intent.type} (confidence=${intent.confidence})")

                executeIntent(intent)
            } catch (e: Exception) {
                Timber.e(e, "Assistant listening failed")
                _uiState.value = AssistantUiState.Error(e.message ?: GENERIC_ERROR_MESSAGE)
            }
        }
    }

    /**
     * Captures until the speaker stops, and reports whether speech was heard.
     *
     * Endpointing runs on [LevelVadDetector] rather than the Silero binding on
     * purpose: model weights are never bundled into a release, so a
     * model-backed detector throws on a fresh install — which is exactly the
     * path the system assistant takes on first use. The level detector needs
     * no model.
     *
     * One collector owns the frame flow. The capture session reads a single
     * [android.media.AudioRecord], so a second collector would race it for the
     * microphone; level metering and endpointing therefore share this loop.
     */
    private suspend fun captureUntilEndpoint(): Boolean {
        val meter = MicLevelMeter()
        val vad = LevelVadDetector()
        var lastFrameAt = System.nanoTime()
        var lastPublishAt = 0L
        var speechMillis = 0L
        var silenceMillis = 0L
        var sawSpeech = false
        var endpointReached = false

        withTimeoutOrNull(MAX_CAPTURE_MILLIS) {
            audioSession.pcmFrames
                .onEach { frame ->
                    val now = System.nanoTime()
                    val elapsedMillis = (now - lastFrameAt) / 1_000_000
                    lastFrameAt = now

                    // Every frame advances the envelope so attack and release
                    // stay accurate, but the overlay is only refreshed at
                    // LEVEL_PUBLISH_INTERVAL_MS: at 16 kHz a frame arrives
                    // roughly every 16 ms and recomposing that often buys
                    // nothing the eye can see. The desktop prompter throttles
                    // for the same reason.
                    val level = meter.accept(frame, elapsedMillis)
                    if ((now - lastPublishAt) / 1_000_000 >= LEVEL_PUBLISH_INTERVAL_MS) {
                        lastPublishAt = now
                        _uiState.value = AssistantUiState.Listening(level)
                    }

                    val frameMillis = frame.frameDurationMillis()
                    if (vad.processFrame(frame.toPcm16Samples()) >= SPEECH_PROBABILITY) {
                        speechMillis += frameMillis
                        silenceMillis = 0
                        if (speechMillis >= MIN_SPEECH_MILLIS) sawSpeech = true
                    } else {
                        silenceMillis += frameMillis
                        // Only a trailing silence ends the turn. Leading
                        // silence must not, or the assistant would give up
                        // before the user starts speaking.
                        if (sawSpeech && silenceMillis >= MIN_SILENCE_MILLIS) {
                            endpointReached = true
                        }
                    }
                }
                .takeWhile { !endpointReached }
                .collect()
        }
        return sawSpeech
    }

    private suspend fun executeIntent(intent: AssistantIntent) {
        _uiState.value = AssistantUiState.Executing(intent.type.displayName)

        val result = intentRouter.execute(context, intent)

        _uiState.value = if (result.success) {
            AssistantUiState.Result(result.responseText)
        } else {
            AssistantUiState.Error(result.errorMessage ?: ACTION_FAILED_MESSAGE)
        }

        if (result.success && !result.keepOpen) {
            delay(RESULT_LINGER_MILLIS)
            hide()
        }
    }

    private companion object {
        /** ~20 Hz, matching the kit's browser meter interval. */
        const val LEVEL_PUBLISH_INTERVAL_MS = 50L

        /** Frame-level speech verdict, matching the Silero consumer threshold. */
        const val SPEECH_PROBABILITY = 0.5f

        /** Trailing silence that ends a turn, mirroring VadConfig's default. */
        const val MIN_SILENCE_MILLIS = 700L

        /** Speech needed before a turn can be endpointed at all. */
        const val MIN_SPEECH_MILLIS = 250L

        /** Hard cap so a stuck capture cannot hold the microphone open. */
        const val MAX_CAPTURE_MILLIS = 30_000L

        /** How long a successful result stays on screen before auto-close. */
        const val RESULT_LINGER_MILLIS = 2_000L

        // TODO(i18n): replace with sk.voice.* message IDs once the assistant
        // adopts the shared catalog; locales/*.json is the parity source.
        const val NO_SPEECH_MESSAGE = "Keine Sprache erkannt"
        const val ACTION_FAILED_MESSAGE = "Aktion fehlgeschlagen"
        const val GENERIC_ERROR_MESSAGE = "Fehler"
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
