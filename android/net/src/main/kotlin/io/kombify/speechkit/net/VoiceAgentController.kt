package io.kombify.speechkit.net

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import okhttp3.OkHttpClient

/**
 * What a Voice Agent conversation looks like to a UI, independent of the
 * wire. Both the in-app screen and the keyboard surface bind to this rather
 * than to frames, so they cannot drift apart in what they render.
 *
 * [userText] and [agentText] are cumulative for the turn in progress; they
 * reset when that side finishes its turn.
 */
data class VoiceAgentUiState(
    val phase: Phase = Phase.Inactive,
    val userText: String = "",
    val agentText: String = "",
    val error: String? = null,
) {
    /**
     * The conversation phases a surface has to render. Deliberately the
     * session's own vocabulary, not the eight orb visual states — the orb
     * maps these, so a visual change never forces a protocol change.
     */
    enum class Phase { Inactive, Connecting, Listening, Processing, Speaking, Ended }

    val isLive: Boolean
        get() = phase == Phase.Connecting ||
            phase == Phase.Listening ||
            phase == Phase.Processing ||
            phase == Phase.Speaking
}

/**
 * Drives one Voice Agent conversation: mints the session, opens the socket,
 * and folds the event stream into [state].
 *
 * The controller deliberately does NOT own microphone capture or playback.
 * Android surfaces differ in who may hold the recorder — the assistant
 * session, the IME, or an activity — and a controller that grabbed the mic
 * itself would fight whichever one already had it. Callers push captured PCM
 * in with [sendAudio] and take agent audio out of [audio].
 */
class VoiceAgentController(
    private val profile: ConnectionProfile,
    private val okHttp: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    private val client: VoiceAgentWsClient = VoiceAgentWsClient(okHttp),
) {

    private val _state = MutableStateFlow(VoiceAgentUiState())
    val state: StateFlow<VoiceAgentUiState> = _state.asStateFlow()

    private var session: VoiceAgentSession? = null

    /**
     * Opens a conversation and returns the event flow the caller collects.
     * Audio frames arrive as [VoiceAgentEvent.Audio]; everything else is
     * already folded into [state] by the time the caller sees it.
     *
     * @throws SpeechKitApiException when the session mint is rejected.
     * @throws IllegalStateException on a profile without a server: a realtime
     *   conversation has no on-device tier to fall back to, unlike dictation.
     */
    suspend fun start(options: VoiceAgentStartFrame = VoiceAgentStartFrame()): Flow<VoiceAgentEvent> {
        val server = profile as? ConnectionProfile.Server
            ?: throw IllegalStateException(
                "The Voice Agent needs a configured SpeechKit server: the realtime " +
                    "conversation has no on-device tier. Pair a server first.",
            )

        _state.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Connecting)
        val mint = SpeechKitServerApi(server, okHttp).createVoiceAgentSession()
        val live = client.connect(mint)
        session = live
        live.start(options)
        return live.events
    }

    /**
     * Folds one event into [state]. Call this for every event the collector
     * receives; audio frames pass through untouched for the caller to play.
     */
    fun accept(event: VoiceAgentEvent) {
        _state.value = when (event) {
            is VoiceAgentEvent.State -> _state.value.copy(phase = event.state.toPhase())

            is VoiceAgentEvent.Transcript -> if (event.input) {
                _state.value.copy(userText = event.text)
            } else {
                _state.value.copy(agentText = event.text)
            }

            // A cut answer is not a partial answer: keep what was said, but do
            // not let the next turn append to it.
            VoiceAgentEvent.Interrupted -> _state.value.copy(agentText = "")

            is VoiceAgentEvent.Failure -> _state.value.copy(error = event.message)

            is VoiceAgentEvent.Closed -> _state.value.copy(
                phase = VoiceAgentUiState.Phase.Ended,
            )

            // Tool calls are host business, and audio is played by the caller.
            is VoiceAgentEvent.ToolCall, is VoiceAgentEvent.Audio -> _state.value
        }
    }

    /** Streams captured microphone PCM to the agent. */
    suspend fun sendAudio(pcm: ByteArray) {
        session?.sendAudio(pcm)
    }

    /**
     * Ends the user's turn — the hold-to-talk release. The agent answers and
     * the conversation continues; it does not end the session.
     */
    suspend fun endTurn() {
        session?.endTurn()
    }

    /** Injects a typed turn instead of speech. */
    suspend fun sendText(text: String) {
        session?.sendText(text)
    }

    /** Answers a tool call the host executed. */
    suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>) {
        session?.respondToTool(id, name, response)
    }

    /** Ends the conversation and releases the socket. */
    suspend fun stop() {
        val live = session ?: return
        session = null
        runCatching { live.close() }
        _state.value = _state.value.copy(phase = VoiceAgentUiState.Phase.Ended)
    }

    private fun String.toPhase(): VoiceAgentUiState.Phase = when (this) {
        VoiceAgentStates.CONNECTING -> VoiceAgentUiState.Phase.Connecting
        VoiceAgentStates.LISTENING -> VoiceAgentUiState.Phase.Listening
        VoiceAgentStates.PROCESSING -> VoiceAgentUiState.Phase.Processing
        VoiceAgentStates.SPEAKING -> VoiceAgentUiState.Phase.Speaking
        // Recovering is a live state, not a dead one: the session is coming
        // back and the surface must not look ended while it does.
        VoiceAgentStates.RECOVERING -> VoiceAgentUiState.Phase.Connecting
        VoiceAgentStates.DEACTIVATING, VoiceAgentStates.INACTIVE ->
            VoiceAgentUiState.Phase.Ended

        else -> _state.value.phase // forward-compatible: unknown states hold
    }
}
