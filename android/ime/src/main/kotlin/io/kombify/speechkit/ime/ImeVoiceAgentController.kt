package io.kombify.speechkit.ime

import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.audio.PcmPlaybackException
import io.kombify.speechkit.audio.PcmPlaybackQueue
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentSessionDriver
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.net.VoiceAgentAudio
import io.kombify.speechkit.net.VoiceAgentSetupException
import io.kombify.speechkit.net.VoiceAgentWsClient
import io.kombify.speechkit.net.safeVoiceAgentCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
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
 * The panel draws the shared `:voice-ui-compose` orb. Session phases map
 * through `toAuraState()` in this module so `:voice-ui-compose` never depends
 * on `:net`. `:ime` still must not depend on `:assistant`.
 */
class ImeVoiceAgentController(
    private val scope: CoroutineScope,
    private val controllerFactory: () -> VoiceAgentSessionDriver,
    private val audioCapture: AudioCapture,
    private val micPermission: MicPermissionGate,
) {

    private val _state = MutableStateFlow(VoiceAgentUiState())
    val state: StateFlow<VoiceAgentUiState> = _state.asStateFlow()

    // Controls never wait for the loudspeaker. The shared bound includes active playback.
    private val playback = PcmPlaybackQueue(VoiceAgentAudio.SERVER_SAMPLE_RATE)
    private var playbackFlush: (() -> Unit)? = null

    private var controller: VoiceAgentSessionDriver? = null
    private var eventsJob: Job? = null
    private var captureJob: Job? = null
    private var ready = false

    // The permission answer comes back asynchronously through a trampoline
    // activity, so the provider the user picked before the dialog appeared has
    // to survive until the grant arrives.
    private var awaitingPermission = false
    private var pendingProvider: String? = null

    val isLive: Boolean get() = controller != null

    /** One host consumer; playback runs in the queue's cancelable per-frame lifetime. */
    suspend fun consumeAudio(flush: () -> Unit = {}, play: suspend (ByteArray) -> Unit) {
        playbackFlush = flush
        try {
            playback.consume(play)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: PcmPlaybackException) {
            fail(safeVoiceAgentCode(failure.code, "playback_failed"), "Voice playback failed")
        } finally {
            if (playbackFlush === flush) {
                playbackFlush = null
                stop()
                flush()
            }
        }
    }

    /**
     * Opens a conversation on [provider] — one of the realtime backend names
     * the server registers ("deepgram", "assemblyai", "openai", "gemini", …), or null
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
        if (!ready) return
        captureJob?.cancel()
        captureJob = scope.launch {
            runCatching {
                audioCapture.frames().collect { frame -> live.sendAudio(frame) }
            }.onFailure { error ->
                if (error is CancellationException) throw error
                if (controller !== live) return@launch
                VoiceLog.e(VoiceLog.AUDIO, "ime voice agent capture failed")
                fail("capture_failed", "Voice capture failed")
            }
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
        ready = false
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
        val phase = if (_state.value.errorCode == ERROR_NO_SERVER) {
            VoiceAgentUiState.Phase.Inactive
        } else {
            VoiceAgentUiState.Phase.Ended
        }
        _state.value = _state.value.copy(phase = phase)
    }

    /**
     * Cancels active and queued speech, then flushes the registered host player.
     */
    fun discardPendingAudio() {
        playback.clear()
        playbackFlush?.invoke()
    }

    private fun fail(code: String, message: String) {
        _state.value = _state.value.copy(phase = VoiceAgentUiState.Phase.Ended,
            error = message, errorCode = code)
        stop()
    }

    private fun open(provider: String?) {
        // A new conversation starts from nothing: transcripts, queued speech
        // and the error of the previous one must not surface while this one
        // connects.
        discardPendingAudio()
        _state.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Connecting)
        val live = controllerFactory()
        controller = live
        ready = false
        val collecting = scope.launch(start = CoroutineStart.LAZY) {
            runCatching {
                val events = live.start(VoiceAgentStartFrame(provider = provider))
                if (controller !== live) return@launch
                ready = true
                events.collect { event ->
                    if (controller !== live) return@collect
                    live.accept(event)
                    _state.value = live.state.value
                    when (event) {
                        is VoiceAgentEvent.Audio -> if (!playback.offer(event.pcm))
                            fail(VoiceAgentWsClient.OVERFLOW_CODE, "Voice playback capacity exceeded")
                        VoiceAgentEvent.Interrupted -> discardPendingAudio()
                        is VoiceAgentEvent.Closed -> stop()
                        is VoiceAgentEvent.Failure -> if (event.fatal) stop()
                        else -> Unit
                    }
                }
            }.onFailure { error ->
                // Ending a conversation cancels this job, and a cancellation is
                // not a failure to report: without this the panel's error line
                // showed "StandaloneCoroutine was cancelled" in red as the last
                // thing the user saw. Rethrowing rather than returning also
                // keeps the coroutine's cancellation contract, which
                // runCatching would otherwise swallow.
                if (error is CancellationException) throw error
                if (controller !== live) return@launch
                VoiceLog.e(VoiceLog.AGENT, "ime conversation failed")
                val setup = error as? VoiceAgentSetupException
                val noServer = setup == null && error is IllegalStateException
                _state.value = _state.value.copy(
                    // Nothing finished: a missing server is a setup gap, not
                    // the end of a conversation. "Ended" reads as "Beendet".
                    phase = if (noServer) {
                        VoiceAgentUiState.Phase.Inactive
                    } else {
                        VoiceAgentUiState.Phase.Ended
                    },
                    error = if (noServer) "Voice Agent needs a configured server" else "Voice conversation failed",
                    // A profile without a server throws before a single frame
                    // moves; with no code the panel could only show the raw
                    // exception text, which reads like a server outage.
                    errorCode = if (setup != null) {
                        safeVoiceAgentCode(setup.code, VoiceAgentWsClient.SETUP_FAILURE_CODE)
                    } else if (noServer) {
                        ERROR_NO_SERVER
                    } else {
                        "voice_session_failed"
                    },
                )
                stop()
            }
        }
        eventsJob = collecting
        collecting.start()
    }

    companion object {
        /** The conversation never opened: no server is paired. */
        const val ERROR_NO_SERVER = "no_server"

        /** Same refusal as dictation's, so both panels can share one label. */
        const val ERROR_MIC_DENIED = VoicePanelController.ERROR_MIC_DENIED
    }
}
