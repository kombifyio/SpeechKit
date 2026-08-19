package io.kombify.speechkit.ime

import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import io.kombify.speechkit.ime.host.VoiceInputHost
import io.kombify.speechkit.ime.host.isDictationBlocked
import io.kombify.speechkit.net.SpeechKitApiException
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import timber.log.Timber

/** Panel FSM states. See [VoicePanelController] for the transition map. */
sealed interface VoicePanelState {
    /** Bound to an editor, mic idle, ready to dictate. */
    data object Idle : VoicePanelState

    /** Password/TYPE_NULL editor: dictation refused, mic disabled. */
    data object Blocked : VoicePanelState

    /** Waiting for the RECORD_AUDIO grant (trampoline in flight). */
    data object NeedsMicPermission : VoicePanelState

    /** Session mint + WS handshake in flight. */
    data object Connecting : VoicePanelState

    /** Mic open, audio streaming; [draft] is the live composing text. */
    data class Listening(val draft: String = "") : VoicePanelState

    /** Mic released; waiting for the segment's final transcript. */
    data object Finishing : VoicePanelState

    /** Final committed to the editor; transient until SegmentDone. */
    data class Committed(val text: String) : VoicePanelState

    /** Recoverable failure; [retryable] gates the one-tap retry chip. */
    data class Error(
        val code: String,
        val message: String,
        val retryable: Boolean = true,
    ) : VoicePanelState
}

/** Cold audio source: recording starts on collect and stops on cancel. */
fun interface AudioCapture {
    fun frames(): Flow<ByteArray>
}

/** Runtime-permission seam; the real gate launches the trampoline activity. */
interface MicPermissionGate {
    fun isGranted(): Boolean

    /** Fire-and-forget; the answer arrives via [VoicePanelController.onMicPermissionResult]. */
    fun request()
}

/**
 * UI-agnostic FSM driving one voice-input panel:
 *
 * ```
 * Idle -> NeedsMicPermission -> Connecting -> Listening(draft) -> Finishing
 *      -> Committed -> Idle            (happy path)
 *      -> Error(retryable) -> Connecting (one-tap retry)
 * Blocked                              (password/TYPE_NULL editors, terminal
 *                                       until the next showPanel)
 * ```
 *
 * InputConnection semantics (the part hosts must not re-implement):
 * - Draft  -> `setComposingText(text, 1)` — visible, never committed.
 * - Final  -> `commitText(text, 1)` — replaces the composing region.
 * - Failure -> `finishComposingText()` — the draft must not linger as
 *   composing text the next keystroke would clobber.
 *
 * One warm [StreamingSttSession] is reused across mic presses while the
 * panel is shown (the controller-side keepalive lives in `:net`); the session
 * is released on [hidePanel]/[shutdown] so it never pins the server's
 * per-identity slot while the keyboard is away.
 */
class VoicePanelController(
    private val scope: CoroutineScope,
    private val sessionFactory: suspend () -> StreamingSttSession,
    private val audioCapture: AudioCapture,
    private val micPermission: MicPermissionGate,
    initialLanguage: String = "de",
) : VoiceInputHost {

    private val _state = MutableStateFlow<VoicePanelState>(VoicePanelState.Idle)
    val state: StateFlow<VoicePanelState> = _state.asStateFlow()

    private val _language = MutableStateFlow(initialLanguage)
    val language: StateFlow<String> = _language.asStateFlow()

    override var listener: VoiceInputHost.Listener? = null

    private var inputConnection: InputConnection? = null
    private var session: StreamingSttSession? = null
    private var eventsJob: Job? = null
    private var captureJob: Job? = null

    // ---- VoiceInputHost ----------------------------------------------------

    override fun showPanel(inputConnection: InputConnection, editorInfo: EditorInfo) {
        // A focus change mid-dictation abandons the old field's segment; its
        // composing text must not leak into the new editor.
        abandonActiveSegment()
        this.inputConnection = inputConnection
        _state.value = if (isDictationBlocked(editorInfo.inputType)) {
            VoicePanelState.Blocked
        } else {
            VoicePanelState.Idle
        }
    }

    override fun hidePanel() {
        abandonActiveSegment()
        inputConnection = null
        releaseSession()
        _state.value = VoicePanelState.Idle
    }

    // ---- UI intents --------------------------------------------------------

    /** The big mic button: starts a segment when idle, finishes it when live. */
    fun toggleMic() {
        when (state.value) {
            is VoicePanelState.Idle,
            is VoicePanelState.Committed,
            is VoicePanelState.Error,
            -> beginDictation()

            is VoicePanelState.Listening -> finishDictation()

            // Blocked stays blocked; transient states debounce the tap.
            is VoicePanelState.Blocked,
            is VoicePanelState.NeedsMicPermission,
            is VoicePanelState.Connecting,
            is VoicePanelState.Finishing,
            -> Unit
        }
    }

    /** One-tap retry from the error chip. */
    fun retry() {
        if (state.value is VoicePanelState.Error) beginDictation()
    }

    /** Language chip / IME subtype switch. Applies from the next segment on. */
    fun setLanguage(language: String) {
        _language.value = language
    }

    /** Result relay from [MicPermissionTrampolineActivity] via the service. */
    fun onMicPermissionResult(granted: Boolean) {
        if (state.value != VoicePanelState.NeedsMicPermission) return
        if (granted) {
            startDictation()
        } else {
            _state.value = VoicePanelState.Error(
                code = ERROR_MIC_DENIED,
                message = "microphone permission denied",
                retryable = true,
            )
        }
    }

    /** Terminal teardown from the service's onDestroy. */
    fun shutdown() {
        captureJob?.cancel()
        captureJob = null
        eventsJob?.cancel()
        eventsJob = null
        inputConnection = null
        releaseSession()
    }

    // ---- FSM internals -----------------------------------------------------

    private fun beginDictation() {
        if (inputConnection == null) return
        if (!micPermission.isGranted()) {
            _state.value = VoicePanelState.NeedsMicPermission
            micPermission.request()
            return
        }
        startDictation()
    }

    private fun startDictation() {
        _state.value = VoicePanelState.Connecting
        scope.launch {
            val open = session ?: runCatching { sessionFactory() }
                .onSuccess { opened ->
                    session = opened
                    eventsJob = scope.launch { collectEvents(opened) }
                }
                .getOrElse { error ->
                    if (error is CancellationException) throw error
                    Timber.w(error, "voice panel: session open failed")
                    _state.value = VoicePanelState.Error(
                        code = (error as? SpeechKitApiException)?.code ?: ERROR_CONNECT_FAILED,
                        message = error.message ?: "session open failed",
                        retryable = true,
                    )
                    return@launch
                }

            // Sequence startSegment -> capture in ONE coroutine: two racing
            // launches could enqueue PCM before the start frame (same rule as
            // the dev screen's pipeline).
            open.startSegment(
                DictationSegmentOptions(language = _language.value),
            )
            _state.value = VoicePanelState.Listening()
            if (open.capturesOwnAudio) {
                // The session records the microphone itself (system
                // SpeechRecognizer tier, B-M2b): running our own AudioRecord
                // would race it for the mic and only feed sendAudio no-ops.
                // The state machine is unchanged — Listening and the capture
                // indicator stay truthful (audio IS being captured, by the
                // session, in this process), just without PCM-level metering.
                return@launch
            }
            captureJob = scope.launch {
                runCatching {
                    audioCapture.frames().collect { pcm -> open.sendAudio(pcm) }
                }.onFailure { error ->
                    if (error is CancellationException) throw error
                    Timber.w(error, "voice panel: audio capture failed")
                    abandonComposingText()
                    _state.value = VoicePanelState.Error(
                        code = ERROR_AUDIO_CAPTURE,
                        message = error.message ?: "audio capture failed",
                        retryable = true,
                    )
                }
            }
        }
    }

    private fun finishDictation() {
        _state.value = VoicePanelState.Finishing
        captureJob?.cancel()
        captureJob = null
        val open = session ?: run {
            _state.value = VoicePanelState.Idle
            return
        }
        scope.launch { runCatching { open.finishSegment() } }
    }

    private suspend fun collectEvents(open: StreamingSttSession) {
        open.events.collect { event ->
            when (event) {
                is TranscriptEvent.SegmentReady -> Unit

                is TranscriptEvent.Draft -> {
                    if (state.value is VoicePanelState.Listening ||
                        state.value is VoicePanelState.Finishing
                    ) {
                        inputConnection?.setComposingText(event.text, 1)
                        listener?.onDraft(event.text)
                        if (state.value is VoicePanelState.Listening) {
                            _state.value = VoicePanelState.Listening(event.text)
                        }
                    }
                }

                is TranscriptEvent.Final -> {
                    // commitText replaces the composing region, so the draft
                    // never doubles up.
                    inputConnection?.commitText(event.text, 1)
                    listener?.onFinal(event.text)
                    _state.value = VoicePanelState.Committed(event.text)
                }

                is TranscriptEvent.SegmentDone -> {
                    if (state.value is VoicePanelState.Committed ||
                        state.value is VoicePanelState.Finishing
                    ) {
                        _state.value = VoicePanelState.Idle
                    }
                }

                is TranscriptEvent.Failure -> {
                    captureJob?.cancel()
                    captureJob = null
                    abandonComposingText()
                    listener?.onError(event.code, event.message)
                    _state.value = VoicePanelState.Error(
                        code = event.code,
                        message = event.message,
                        retryable = true,
                    )
                }

                is TranscriptEvent.Closed -> {
                    session = null
                    captureJob?.cancel()
                    captureJob = null
                    when (state.value) {
                        is VoicePanelState.Connecting,
                        is VoicePanelState.Listening,
                        is VoicePanelState.Finishing,
                        -> {
                            abandonComposingText()
                            _state.value = VoicePanelState.Error(
                                code = ERROR_SESSION_CLOSED,
                                message = event.reason ?: "session closed",
                                retryable = true,
                            )
                        }

                        // Idle/Committed/Error/Blocked: nothing in flight —
                        // the next mic press simply mints a fresh session.
                        else -> Unit
                    }
                }
            }
        }
    }

    private fun abandonActiveSegment() {
        val active = state.value is VoicePanelState.Connecting ||
            state.value is VoicePanelState.Listening ||
            state.value is VoicePanelState.Finishing
        captureJob?.cancel()
        captureJob = null
        if (active) {
            abandonComposingText()
            val open = session
            if (open != null) scope.launch { runCatching { open.finishSegment() } }
        }
    }

    /** A dead draft must not linger as composing text. */
    private fun abandonComposingText() {
        runCatching { inputConnection?.finishComposingText() }
    }

    private fun releaseSession() {
        eventsJob?.cancel()
        eventsJob = null
        val open = session ?: return
        session = null
        scope.launch { runCatching { open.close() } }
    }

    companion object {
        const val ERROR_CONNECT_FAILED = "connect_failed"
        const val ERROR_SESSION_CLOSED = "session_closed"
        const val ERROR_AUDIO_CAPTURE = "audio_capture_failed"
        const val ERROR_MIC_DENIED = "mic_permission_denied"
        const val ERROR_NOT_CONFIGURED = "not_configured"
    }
}
