package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ExecutionMode
import io.kombify.speechkit.domain.restrictTo
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.describe
import io.kombify.speechkit.domain.storageScope
import kotlinx.coroutines.currentCoroutineContext
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
    /** Last transcript event, independent of subsequent listening/processing phases. */
    val latestSpeaker: Speaker? = null,
) {
    /**
     * The conversation phases a surface has to render. Deliberately the
     * session's own vocabulary, not the eight orb visual states — the orb
     * maps these, so a visual change never forces a protocol change.
     */
    enum class Phase { Inactive, Connecting, Listening, Processing, Speaking, Ended }
    enum class Speaker { USER, AGENT }

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
    suspend fun sendText(text: String)
    suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>)
    suspend fun advanceStep(reason: String? = null)
    suspend fun cancelReply()
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
    private val target: RegisteredAgentTarget? = null,
    private val context: Context? = null,
    private val localHandoff: (() -> Unit)? = null,
    private val localReady: suspend () -> Boolean = { false },
    private val policyOverride: io.kombify.speechkit.domain.ExecutionPolicy? = null,
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
        if (profile is ConnectionProfile.Server) context?.let { StoredConnectionSelection(it).requireCurrent(profile) }
        val policy = (context?.let { StoredExecutionPolicy(it).read(profile) }
            ?: io.kombify.speechkit.domain.ExecutionPolicy.fromLegacy(profile)).restrictTo(policyOverride)
        if (policy.mode == ExecutionMode.DEVICE_ONLY || !policy.allowNetwork) {
            localHandoff?.invoke()
            throw localVoiceHandoffFailure()
        }
        if (policy.mode == ExecutionMode.PREFER_DEVICE && localHandoff != null && localReady()) {
            localHandoff.invoke()
            throw localVoiceHandoffFailure()
        }
        val server = profile as? ConnectionProfile.Server
            ?: throw IllegalStateException("No selected SpeechKit endpoint")
        require(target == null || context != null) { "Registered voice sessions require durable receipt storage" }
        if (target != null) {
            val job = checkNotNull(currentCoroutineContext()[Job])
            check(StoredServerProfile.withActiveProfile(requireNotNull(context), server) {
                ScopedRemoteJobs.register(server.storageScope(), job)
            }) { "Account session changed" }
        }
        VoiceLog.i(VoiceLog.AGENT, "start ${profile.describe()}")
        cancelRenewal()
        _state.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Connecting)
        val mint = try {
            SpeechKitServerApi(server, okHttp, context = context).createVoiceAgentSession(target)
        } catch (failure: java.io.IOException) {
            if (failure is SpeechKitApiException && (failure.denial != null || failure.httpStatus < 500)) throw failure
            if (policy.mode == ExecutionMode.PREFER_ENDPOINT && localHandoff != null && localReady()) {
                localHandoff.invoke()
                throw localVoiceHandoffFailure()
            }
            throw failure
        }
        require(target == null || mint.aiSessionId == target.aiSessionId) { "Voice session does not match the selected chat" }
        require(sameVoiceOrigin(server.normalizedBaseUrl, mint.wsUrl)) { "Voice session URL does not match the selected origin" }
        if (target != null) {
            val store = io.kombify.speechkit.store.RoomStore(requireNotNull(context))
            try {
                // Persist before opening media: recovery can list server-owned turns
                // even if the socket closes before its first receipt reaches Room.
                store.saveScopedOperation(io.kombify.speechkit.store.OperationReceipt(server.storageScope(), "session:${mint.sessionId}", "remote_session", "created", mint.sessionId, System.currentTimeMillis()))
            } finally { store.close() }
        }
        val live = client.connect(mint, context?.let { StoredConnectionSelection(it) }, server.takeIf { context != null })
        session = live
        try {
            live.start(if (target != null) options.copy(turnReceipts = true) else options)
        } catch (failure: Throwable) {
            session = null
            runCatching { live.close() }
            if (failure is VoiceAgentSetupException && failure.code in setOf(
                    VoiceAgentWsClient.SETUP_TIMEOUT_CODE, VoiceAgentWsClient.SEND_FAILURE_CODE,
                ) && policy.mode == ExecutionMode.PREFER_ENDPOINT && localHandoff != null && localReady()) {
                localHandoff.invoke()
                throw localVoiceHandoffFailure()
            }
            throw failure
        }
        return flow {
            val renewing = if (session === live) scheduleRenewal(server, mint) else null
            try {
                live.events.collect { event ->
                    if (event is VoiceAgentEvent.TurnReceipt && target != null) {
                        val turn = event.turn
                        require(turn.sessionId == mint.sessionId && turn.turnId.startsWith("${mint.sessionId}:") && turn.status in setOf("unknown", "dispatched", "completed"))
                        val store = context?.let { io.kombify.speechkit.store.RoomStore(it) }
                        try {
                            store?.saveRemoteTurnReceipt(server.storageScope(), turn.sessionId, turn.turnId, turn.status)
                        } finally { store?.close() }
                    }
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
                val api = SpeechKitServerApi(currentServer, okHttp, context = context)
                val renewed = try {
                    // The deadline bounds the attempt: past it there is nothing left to extend.
                    withTimeoutOrNull((deadlineMs - System.currentTimeMillis()).coerceAtLeast(1L)) {
                        api.renewVoiceAgentSession(sessionId, target)
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
        _state.value = reduceVoiceAgentState(_state.value, event)
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
        val live = session ?: return
        live.endTurn()
    }

    /** Injects a typed turn instead of speech. */
    override suspend fun sendText(text: String) {
        val live = session ?: return
        live.sendText(text)
    }

    /** Answers a tool call the host executed. */
    override suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>) {
        session?.respondToTool(id, name, response)
    }

    /**
     * Tap-to-interrupt: stops the agent reply that is playing right now. The
     * server acknowledges with an interrupted event, which drives the phase
     * back to listening.
     */
    override suspend fun cancelReply() {
        session?.cancelReply()
    }

    /** Moves a running sequence to its next step. */
    override suspend fun advanceStep(reason: String?) {
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

    private companion object {
        /** How long before the budget deadline a renewal is requested, at most. */
        const val RENEW_LEAD_MS = 60_000L
    }
}

private fun sameVoiceOrigin(baseUrl: String, wsUrl: String): Boolean = try {
    val base = java.net.URI(baseUrl)
    val ws = java.net.URI(wsUrl)
    val expectedScheme = when (base.scheme?.lowercase()) { "https" -> "wss"; "http" -> "ws"; else -> "" }
    fun port(uri: java.net.URI): Int = if (uri.port >= 0) uri.port else if (uri.scheme.equals("https", true) || uri.scheme.equals("wss", true)) 443 else 80
    ws.scheme.equals(expectedScheme, true) && base.host != null && ws.host.equals(base.host, true) &&
        ws.userInfo == null && ws.fragment == null && port(ws) == port(base)
} catch (_: Exception) { false }

internal fun reduceVoiceAgentState(state: VoiceAgentUiState, event: VoiceAgentEvent): VoiceAgentUiState = when (event) {
            is VoiceAgentEvent.State -> state.copy(phase = event.state.toPhase(state.phase))

            is VoiceAgentEvent.Transcript -> if (event.input) {
                state.copy(userText = event.text, latestSpeaker = VoiceAgentUiState.Speaker.USER)
            } else {
                state.copy(agentText = event.text, latestSpeaker = VoiceAgentUiState.Speaker.AGENT)
            }

            // A cut answer is not a partial answer: keep what was said, but do
            // not let the next turn append to it.
            VoiceAgentEvent.Interrupted -> state.copy(agentText = "", latestSpeaker = null)

            is VoiceAgentEvent.Failure -> {
                VoiceLog.e(VoiceLog.AGENT, "session failure")
                state.copy(
                    error = event.message,
                    errorCode = event.code,
                    errorClass = event.errorClass,
                )
            }

            is VoiceAgentEvent.Closed -> state.copy(phase = VoiceAgentUiState.Phase.Ended)

            // Tool calls are host business, and audio is played by the caller.
            is VoiceAgentEvent.ToolCall, is VoiceAgentEvent.Audio, is VoiceAgentEvent.TurnReceipt -> state
        }

private fun String.toPhase(current: VoiceAgentUiState.Phase): VoiceAgentUiState.Phase = when (this) {
    VoiceAgentStates.CONNECTING, VoiceAgentStates.RECOVERING -> VoiceAgentUiState.Phase.Connecting
    VoiceAgentStates.LISTENING -> VoiceAgentUiState.Phase.Listening
    VoiceAgentStates.PROCESSING -> VoiceAgentUiState.Phase.Processing
    VoiceAgentStates.SPEAKING -> VoiceAgentUiState.Phase.Speaking
    VoiceAgentStates.DEACTIVATING, VoiceAgentStates.INACTIVE -> VoiceAgentUiState.Phase.Ended
    else -> current
}

internal fun localVoiceHandoffFailure() = io.kombify.speechkit.domain.ExecutionFailure(
    io.kombify.speechkit.domain.ExecutionFailureKind.UNAVAILABLE, "Continue with local voice in SpeechKit",
)
