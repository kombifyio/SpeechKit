package io.kombify.speechkit.ime

import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentUiState
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
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

    /**
     * Agent audio for the host to play, in arrival order; the controller never
     * plays it itself.
     *
     * A queue and not a `StateFlow<ByteArray?>`: a StateFlow conflates, so
     * every frame that arrived before the host had observed the previous one
     * was dropped without a trace and the answer reached the speaker with
     * holes in it.
     *
     * Unbounded, because the producer is the same single collector that feeds
     * [state]: back-pressure here suspends that collector, so phase changes,
     * transcript text and Interrupted/Error/Closed would all queue behind the
     * loudspeaker and then arrive in a burst. A bound does not buy memory
     * safety either — the socket's own queue upstream is already unlimited and
     * fed with `trySend` from OkHttp's callback thread, so a bound here only
     * relocates the backlog. And it fills in normal use: the server
     * synthesises faster than real time, so a player draining at exactly real
     * time still fills any bounded queue part way into an ordinary
     * multi-sentence answer. Nothing is dropped silently to pay for that; the
     * queue is emptied only by [discardPendingAudio], on barge-in, on [stop]
     * and before a new conversation opens.
     */
    private val _audio = Channel<ByteArray>(Channel.UNLIMITED)
    val audio: Flow<ByteArray> = _audio.receiveAsFlow()

    private var controller: VoiceAgentController? = null
    private var eventsJob: Job? = null
    private var captureJob: Job? = null

    // The permission answer comes back asynchronously through a trampoline
    // activity, so the provider the user picked before the dialog appeared has
    // to survive until the grant arrives.
    private var awaitingPermission = false
    private var pendingProvider: String? = null

    val isLive: Boolean get() = controller != null

    /**
     * Opens a conversation on [provider] — one of the realtime backend names
     * the server registers ("deepgram", "assemblyai", "gemini", …), or null
     * for whatever that server configured as its default. No-op while a
     * conversation is already live.
     */
    fun start(provider: String? = null) {
        if (controller != null) return
        if (!micPermission.isGranted()) {
            awaitingPermission = true
            pendingProvider = provider
            micPermission.request()
            return
        }
        awaitingPermission = false
        open(provider)
    }

    /**
     * Result relay from [MicPermissionTrampolineActivity] via the host, so the
     * conversation the user asked for actually opens once the microphone is
     * granted instead of dead-ending at the permission prompt.
     */
    fun onMicPermissionResult(granted: Boolean) {
        // The host feeds every result to both controllers; only the one that
        // asked may act on it.
        if (!awaitingPermission) return
        awaitingPermission = false
        val provider = pendingProvider
        pendingProvider = null
        if (granted) {
            open(provider)
        } else {
            _state.value = _state.value.copy(
                phase = VoiceAgentUiState.Phase.Ended,
                error = "microphone permission denied",
                errorCode = ERROR_MIC_DENIED,
            )
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
        awaitingPermission = false
        pendingProvider = null
        discardPendingAudio()
        val live = controller ?: return
        controller = null
        scope.launch { runCatching { live.stop() } }
        // Keep whatever error the state already carries: a start that failed
        // calls stop() to clean up, and resetting to a fresh state here would
        // swallow the only explanation the panel has to show.
        _state.value = _state.value.copy(phase = VoiceAgentUiState.Phase.Ended)
    }

    /**
     * Throws away agent speech that has been received but not yet played.
     *
     * Barge-in has two halves: the host cuts what the speaker is already
     * reading out, and this cuts what is queued behind it. Flushing the track
     * alone would only pause the abandoned answer, because the queue would
     * feed it straight back in.
     */
    fun discardPendingAudio() {
        var dropped = _audio.tryReceive()
        while (dropped.isSuccess) {
            dropped = _audio.tryReceive()
        }
    }

    private fun open(provider: String?) {
        // A new conversation starts from nothing: transcripts, queued speech
        // and the error of the previous one must not surface while this one
        // connects.
        discardPendingAudio()
        _state.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Connecting)
        val live = controllerFactory()
        controller = live
        eventsJob = scope.launch {
            runCatching {
                val events = live.start(VoiceAgentStartFrame(provider = provider))
                events.collect { event ->
                    live.accept(event)
                    _state.value = live.state.value
                    if (event is VoiceAgentEvent.Audio) _audio.send(event.pcm)
                }
            }.onFailure { error ->
                // Ending a conversation cancels this job, and a cancellation is
                // not a failure to report: without this the panel's error line
                // showed "StandaloneCoroutine was cancelled" in red as the last
                // thing the user saw. Rethrowing rather than returning also
                // keeps the coroutine's cancellation contract, which
                // runCatching would otherwise swallow.
                if (error is CancellationException) throw error
                _state.value = _state.value.copy(
                    phase = VoiceAgentUiState.Phase.Ended,
                    error = error.message,
                    // A profile without a server throws before a single frame
                    // moves; with no code the panel could only show the raw
                    // exception text, which reads like a server outage.
                    errorCode = if (error is IllegalStateException) {
                        ERROR_NO_SERVER
                    } else {
                        _state.value.errorCode
                    },
                )
                stop()
            }
        }
    }

    companion object {
        /** The conversation never opened: no server is paired. */
        const val ERROR_NO_SERVER = "no_server"

        /** Same refusal as dictation's, so both panels can share one label. */
        const val ERROR_MIC_DENIED = VoicePanelController.ERROR_MIC_DENIED
    }
}
