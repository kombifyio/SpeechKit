package io.kombify.speechkit.ime

import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.audio.PcmPlaybackException
import io.kombify.speechkit.audio.PcmPlaybackQueue
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.SpeechKitApiException
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
    private val _inputLevel = MutableStateFlow(0f)
    val inputLevel: StateFlow<Float> = _inputLevel.asStateFlow()
    private val _outputLevel = MutableStateFlow(0f)
    val outputLevel: StateFlow<Float> = _outputLevel.asStateFlow()

    // Controls never wait for the loudspeaker. The shared bound includes active playback.
    private val playback = PcmPlaybackQueue(VoiceAgentAudio.SERVER_SAMPLE_RATE)
    private var playbackFlush: (() -> Unit)? = null

    private var controller: VoiceAgentSessionDriver? = null
    private var eventsJob: Job? = null
    private var captureJob: Job? = null
    private var policyListener: java.io.Closeable? = null
    private var ready = false
    private var holdRequested = false

    // The permission answer comes back asynchronously through a trampoline
    // activity, so the provider the user picked before the dialog appeared has
    // to survive until the grant arrives.
    private var awaitingPermission = false
    private var pendingProvider: String? = null
    private var pendingModel: String? = null

    val isLive: Boolean get() = controller != null

    /** One host consumer; playback runs in the queue's cancelable per-frame lifetime. */
    suspend fun consumeAudio(flush: () -> Unit = {}, play: suspend (ByteArray) -> Unit) {
        playbackFlush = flush
        try {
            playback.consume { pcm ->
                _outputLevel.value = pcmLevel(pcm)
                try { play(pcm) } finally { _outputLevel.value = 0f }
            }
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
    fun start(provider: String? = null, model: String? = null) {
        if (controller != null) return
        policyListener?.close()
        policyListener = io.kombify.speechkit.net.NetworkUseGuard.listen {
            val jobs = listOfNotNull(captureJob, eventsJob)
            stop()
            jobs
        }
        if (!micPermission.isGranted()) {
            awaitingPermission = true
            pendingProvider = provider
            pendingModel = model
            micPermission.request()
            return
        }
        awaitingPermission = false
        open(provider, model)
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
        val model = pendingModel
        pendingProvider = null
        pendingModel = null
        if (granted) {
            open(provider, model)
        } else {
            _state.value = _state.value.copy(
                phase = VoiceAgentUiState.Phase.Ended,
                error = "microphone permission denied",
                errorCode = ERROR_MIC_DENIED,
                errorClass = null,
            )
        }
    }

    /** Streams the microphone while the user holds the talk control. */
    fun beginTurn() {
        holdRequested = true
        val live = controller ?: return
        if (!ready) return
        captureJob?.cancel()
        captureJob = scope.launch {
            runCatching {
                if (_state.value.phase == VoiceAgentUiState.Phase.Speaking) live.cancelReply()
                audioCapture.frames().collect { frame ->
                    _inputLevel.value = pcmLevel(frame)
                    live.sendAudio(frame)
                }
            }.onFailure { error ->
                if (error is CancellationException) throw error
                if (controller !== live) return@launch
                VoiceLog.e(VoiceLog.AUDIO, "ime voice agent capture failed")
                fail("capture_failed", "Voice capture failed")
            }
            _inputLevel.value = 0f
        }
    }

    /** A held gesture cannot resume automatically after its permission dialog. */
    fun cancelPendingHoldPermission() {
        awaitingPermission = false
        pendingProvider = null
        pendingModel = null
        holdRequested = false
    }

    /** Release: the user's turn ends, the agent answers, the session stays. */
    fun endTurn() {
        holdRequested = false
        captureJob?.cancel()
        captureJob = null
        _inputLevel.value = 0f
        val live = controller ?: return
        scope.launch { runCatching { live.endTurn() } }
    }

    /** Ends the conversation and releases the socket. */
    fun stop() {
        policyListener?.close(); policyListener = null
        ready = false
        holdRequested = false
        _inputLevel.value = 0f
        captureJob?.cancel()
        captureJob = null
        eventsJob?.cancel()
        eventsJob = null
        awaitingPermission = false
        pendingProvider = null
        pendingModel = null
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
        _outputLevel.value = 0f
        playback.clear()
        playbackFlush?.invoke()
    }

    private fun fail(code: String, message: String) {
        _state.value = _state.value.copy(phase = VoiceAgentUiState.Phase.Ended,
            error = message, errorCode = code, errorClass = null)
        stop()
    }

    private fun open(provider: String?, model: String?) {
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
                val events = live.start(VoiceAgentStartFrame(provider = provider, model = model))
                if (controller !== live) return@launch
                ready = true
                if (holdRequested) beginTurn()
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
                val planCode = VoicePanelController.planDenialCode(error)
                _state.value = _state.value.copy(
                    // Nothing finished: a missing server is a setup gap, not
                    // the end of a conversation. "Ended" reads as "Beendet".
                    phase = if (noServer) {
                        VoiceAgentUiState.Phase.Inactive
                    } else {
                        VoiceAgentUiState.Phase.Ended
                    },
                    error = if (noServer) "Voice Agent needs a configured server" else "Voice conversation failed",
                    errorClass = if (planCode == null) setup?.errorClass ?: (error as? SpeechKitApiException)?.errorClass else null,
                    // A profile without a server throws before a single frame
                    // moves; with no code the panel could only show the raw
                    // exception text, which reads like a server outage.
                    errorCode = if (setup != null) {
                        safeVoiceAgentCode(setup.code, VoiceAgentWsClient.SETUP_FAILURE_CODE)
                    } else if (noServer) {
                        ERROR_NO_SERVER
                    } else {
                        planCode ?: "voice_session_failed"
                    },
                )
                stop()
            }
        }
        eventsJob = collecting
        collecting.start()
    }

    companion object {
        /** PCM16 little-endian RMS; silence stays zero and no activity is synthesized. */
        private fun pcmLevel(pcm: ByteArray): Float {
            if (pcm.size < 2) return 0f
            var sum = 0.0
            for (i in 0 until pcm.size - 1 step 2) {
                val sample = ((pcm[i].toInt() and 255) or (pcm[i + 1].toInt() shl 8)).toShort().toDouble() / 32768.0
                sum += sample * sample
            }
            return kotlin.math.sqrt(sum / (pcm.size / 2)).toFloat().coerceIn(0f, 1f)
        }

        /** The conversation never opened: no server is paired. */
        const val ERROR_NO_SERVER = "no_server"

        /** Same refusal as dictation's, so both panels can share one label. */
        const val ERROR_MIC_DENIED = VoicePanelController.ERROR_MIC_DENIED

        /** The mint was refused for plan reasons, same codes as dictation. */
        const val ERROR_PLAN_DENIED = VoicePanelController.ERROR_PLAN_DENIED
        const val ERROR_PLAN_BUDGET = VoicePanelController.ERROR_PLAN_BUDGET
    }
}
