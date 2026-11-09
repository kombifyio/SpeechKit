package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.describe
import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.OkHttpClient
import java.time.OffsetDateTime

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
    /**
     * Stable code behind [error] (see [VoiceAgentErrorCodes]). The message is
     * server prose in the server's language; the code is what a surface can
     * turn into its own localized explanation, so a rejected provider does not
     * look like an outage.
     */
    val errorCode: String? = null,
    /** Server-supplied reason class for localized presentation. */
    val errorClass: VoiceErrorClass? = null,
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
 *
 * A hosted session is minted with a Voice budget that has its own deadline
 * (`budget_expires_at`). While the conversation is live the controller asks the
 * server to extend that budget shortly before it runs out. The server owns the
 * deadline: a renewal that fails or whose outcome is unknown is never retried
 * and never ends the conversation; the session then simply ends at the
 * deadline the server holds.
 */
/**
 * The IME and in-app surfaces drive a conversation through this, so tests
 * can complete one turn without a live socket.
 */
interface VoiceAgentSessionDriver {
    val state: StateFlow<VoiceAgentUiState>
    suspend fun start(options: VoiceAgentStartFrame = VoiceAgentStartFrame()): Flow<VoiceAgentEvent>
    fun accept(event: VoiceAgentEvent)
    suspend fun sendAudio(pcm: ByteArray)
    suspend fun endTurn()
    suspend fun stop()
}

class VoiceAgentController(
    private val profile: ConnectionProfile,
    private val okHttp: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    private val client: VoiceAgentWsClient = VoiceAgentWsClient(okHttp),
    /**
     * Resolves the profile to renew the budget with, called off the main
     * thread for each renewal. A bearer that the host provisions (rather than
     * one this device signs in to and refreshes itself) is only current in the
     * host's own profile source. Renewal stops if that source no longer
     * provides the same server as [profile].
     */
    private val renewalProfile: (suspend () -> ConnectionProfile)? = null,
    /**
     * Claim namespace of the renewal bearers, see [sameVoiceRenewalAccount].
     * Required, so each caller chooses the issuer's claims explicitly.
     */
    private val renewalClaimNamespace: String,
) : VoiceAgentSessionDriver {

    private val _state = MutableStateFlow(VoiceAgentUiState())
    override val state: StateFlow<VoiceAgentUiState> = _state.asStateFlow()

    private var session: VoiceAgentSession? = null

    private val renewalScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    @Volatile private var renewal: Job? = null

    /**
     * Opens a conversation and returns the event flow the caller collects.
     * Audio frames arrive as [VoiceAgentEvent.Audio]; everything else is
     * already folded into [state] by the time the caller sees it.
     *
     * @throws SpeechKitApiException when the session mint is rejected.
     * @throws IllegalStateException on a profile without a server: a realtime
     *   conversation has no on-device tier to fall back to, unlike dictation.
     */
    override suspend fun start(options: VoiceAgentStartFrame): Flow<VoiceAgentEvent> {
        val server = profile as? ConnectionProfile.Server
            ?: run {
                VoiceLog.w(VoiceLog.AGENT, "no server ${profile.describe()}")
                throw IllegalStateException(
                    "The Voice Agent needs a configured SpeechKit server: the realtime " +
                        "conversation has no on-device tier. Pair a server first.",
                )
            }

        VoiceLog.i(VoiceLog.AGENT, "start ${profile.describe()}")
        cancelRenewal()
        _state.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Connecting)
        val mint = SpeechKitServerApi(server, okHttp).createVoiceAgentSession()
        val live = client.connect(mint)
        session = live
        live.start(options)
        return flow {
            val renewing = if (session === live) scheduleRenewal(server, mint) else null
            try {
                live.events.collect { event ->
                    if (event is VoiceAgentEvent.Closed) cancelRenewal(renewing)
                    emit(event)
                }
            } finally {
                cancelRenewal(renewing)
            }
        }
    }

    /**
     * Keeps the session's Voice budget ahead of its deadline. Only a session
     * the server gave a budget deadline for is renewed; one without (a
     * self-hosted server reserves none) runs on its ticket alone.
     *
     * The renewal request is not idempotent, so any failure ends the renewal
     * for this session instead of repeating it: the server may already have
     * adopted a reservation whose answer never arrived, and it keeps enforcing
     * its own deadline either way. The conversation itself is never touched.
     */
    private fun scheduleRenewal(server: ConnectionProfile.Server, mint: CreateVoiceAgentSessionResponse): Job? {
        val sessionId = mint.sessionId
        if (!SpeechKitServerApi.isVoiceSessionId(sessionId)) return null
        var deadlineMs = parseDeadlineMs(mint.budgetExpiresAt) ?: return null
        val job = renewalScope.launch {
            while (true) {
                val remaining = deadlineMs - System.currentTimeMillis()
                if (remaining <= 0) return@launch
                delay(remaining - minOf(RENEW_LEAD_MS, remaining / 2))

                val currentServer = renewalServer(server) ?: return@launch
                val api = SpeechKitServerApi(currentServer, okHttp)
                val renewed = try {
                    // The deadline bounds the attempt: past it there is nothing left to extend.
                    withTimeoutOrNull((deadlineMs - System.currentTimeMillis()).coerceAtLeast(1L)) {
                        api.renewVoiceAgentSession(sessionId)
                    }
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (failure: Exception) {
                    val http = (failure as? SpeechKitApiException)?.httpStatus
                    VoiceLog.w(VoiceLog.AGENT, "budget renewal failed http=${http ?: "-"}")
                    return@launch
                }
                val next = renewed?.takeIf { it.sessionId == sessionId }
                    ?.let { parseDeadlineMs(it.budgetExpiresAt) }
                if (next == null || next <= deadlineMs) {
                    VoiceLog.w(VoiceLog.AGENT, "budget renewal not confirmed")
                    return@launch
                }
                deadlineMs = next
            }
        }
        renewal = job
        return job
    }

    private suspend fun renewalServer(fallback: ConnectionProfile.Server): ConnectionProfile.Server? {
        val source = renewalProfile ?: return fallback
        val resolved = try {
            source()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            null
        }
        return (resolved as? ConnectionProfile.Server)
            ?.takeIf {
                it.normalizedBaseUrl == fallback.normalizedBaseUrl &&
                    it.cloudSessionGeneration == fallback.cloudSessionGeneration &&
                    sameVoiceRenewalAccount(fallback.bearerToken, it.bearerToken, renewalClaimNamespace)
            }
    }

    private fun cancelRenewal(job: Job? = renewal) {
        job?.cancel()
        if (renewal === job) renewal = null
    }

    private fun parseDeadlineMs(value: String): Long? =
        value.takeIf { it.isNotBlank() }
            ?.let { runCatching { OffsetDateTime.parse(it).toInstant().toEpochMilli() }.getOrNull() }

    /**
     * Folds one event into [state]. Call this for every event the collector
     * receives; audio frames pass through untouched for the caller to play.
     */
    override fun accept(event: VoiceAgentEvent) {
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

            is VoiceAgentEvent.Failure -> {
                VoiceLog.e(VoiceLog.AGENT, "session failure")
                _state.value.copy(
                    error = event.message,
                    errorCode = event.code,
                    errorClass = event.errorClass,
                )
            }

            is VoiceAgentEvent.Closed -> _state.value.copy(phase = VoiceAgentUiState.Phase.Ended)

            // Tool calls are host business, and audio is played by the caller.
            is VoiceAgentEvent.ToolCall, is VoiceAgentEvent.Audio -> _state.value
        }
    }

    /** Streams captured microphone PCM to the agent. */
    override suspend fun sendAudio(pcm: ByteArray) {
        session?.sendAudio(pcm)
    }

    /**
     * Ends the user's turn — the hold-to-talk release. The agent answers and
     * the conversation continues; it does not end the session.
     */
    override suspend fun endTurn() {
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

    /**
     * Tap-to-interrupt: stops the agent reply that is playing right now. The
     * server acknowledges with an interrupted event, which drives the phase
     * back to listening.
     */
    suspend fun cancelReply() {
        session?.cancelReply()
    }

    /** Moves a running sequence to its next step. */
    suspend fun advanceStep(reason: String? = null) {
        session?.advanceStep(reason)
    }

    /** Ends the conversation and releases the socket. */
    override suspend fun stop() {
        cancelRenewal()
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

    private companion object {
        /** How long before the budget deadline a renewal is requested, at most. */
        const val RENEW_LEAD_MS = 60_000L
    }
}
