package io.kombify.speechkit.ime

import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentUiState
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * Voice Agent mode for the input method: a spoken conversation inside the
 * keyboard window.
 *
 * The hard rule that separates it from dictation: **nothing is ever written
 * into the editor**. Dictation is a keyboard replacement and commits what you
 * said; the agent is a conversation you have while the editor waits
 * untouched. Mixing the two would put the agent's answers into whatever field
 * happened to have focus.
 *
 * The panel renders this with its own visual language rather than the
 * assistant app's orb: `:ime` does not depend on `:assistant`, and adding
 * that dependency to reuse a drawing would couple the keyboard to the
 * assistant application — and would have to be undone once the shared Compose
 * voice-UI artifact lands and gives both surfaces the same orb.
 */
class ImeVoiceAgentController(
    private val scope: CoroutineScope,
    private val controllerFactory: () -> VoiceAgentController,
    private val audioCapture: AudioCapture,
    private val micPermission: MicPermissionGate,
) {

    private val _state = MutableStateFlow(VoiceAgentUiState())
    val state: StateFlow<VoiceAgentUiState> = _state.asStateFlow()

    /** Agent audio for the host to play; the controller never plays it itself. */
    private val _audio = MutableStateFlow<ByteArray?>(null)
    val audio: StateFlow<ByteArray?> = _audio.asStateFlow()

    private var controller: VoiceAgentController? = null
    private var eventsJob: Job? = null
    private var captureJob: Job? = null

    val isLive: Boolean get() = controller != null

    /** Opens a conversation. No-op while one is already live. */
    fun start() {
        if (controller != null) return
        if (!micPermission.isGranted()) {
            micPermission.request()
            return
        }
        val live = controllerFactory()
        controller = live
        eventsJob = scope.launch {
            runCatching {
                val events = live.start(VoiceAgentStartFrame())
                events.collect { event ->
                    live.accept(event)
                    _state.value = live.state.value
                    if (event is VoiceAgentEvent.Audio) _audio.value = event.pcm
                }
            }.onFailure { error ->
                _state.value = _state.value.copy(
                    phase = VoiceAgentUiState.Phase.Ended,
                    error = error.message,
                )
                stop()
            }
        }
    }

    /** Streams the microphone while the user holds the talk control. */
    fun beginTurn() {
        val live = controller ?: return
        captureJob?.cancel()
        captureJob = scope.launch {
            audioCapture.frames().collect { frame -> live.sendAudio(frame) }
        }
    }

    /** Release: the user's turn ends, the agent answers, the session stays. */
    fun endTurn() {
        captureJob?.cancel()
        captureJob = null
        val live = controller ?: return
        scope.launch { runCatching { live.endTurn() } }
    }

    /** Ends the conversation and releases the socket. */
    fun stop() {
        captureJob?.cancel()
        captureJob = null
        eventsJob?.cancel()
        eventsJob = null
        val live = controller ?: return
        controller = null
        scope.launch { runCatching { live.stop() } }
        _state.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Ended)
    }

    /** Clears the audio hand-off after the host has played it. */
    fun consumeAudio() {
        _audio.value = null
    }
}
