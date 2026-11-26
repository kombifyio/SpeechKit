package io.kombify.speechkit.net

import android.content.Context
import android.util.Base64
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ExecutionMode
import io.kombify.speechkit.domain.restrictTo
import io.kombify.speechkit.domain.ExecutionFailure
import io.kombify.speechkit.domain.ExecutionFailureKind
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

internal fun requireGeminiNetwork(context: Context, profile: ConnectionProfile.Byok) {
    if (!profile.hasUsableKey) throw ExecutionFailure(ExecutionFailureKind.AUTHORIZATION, "Gemini API key unavailable; reconnect your key")
    StoredConnectionSelection(context).requireCurrent(profile)
    val policy = StoredExecutionPolicy(context).read(profile)
    if (!policy.allowNetwork || policy.mode == ExecutionMode.DEVICE_ONLY) {
        throw ExecutionFailure(ExecutionFailureKind.POLICY_DENIED, "Network use is disabled")
    }
    NetworkUseGuard.checkAllowed()
}

/** Direct provider session; no managed ticket, budget, or remote action is created. */
class GeminiDirectVoiceAgentDriver(
    private val profile: ConnectionProfile.Byok,
    private val context: Context,
    private val localHandoff: (() -> Unit)? = null,
    private val client: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    private val localReady: suspend () -> Boolean = { false },
    private val policyOverride: io.kombify.speechkit.domain.ExecutionPolicy? = null,
) : VoiceAgentSessionDriver {
    private val mutableState = MutableStateFlow(VoiceAgentUiState())
    override val state: StateFlow<VoiceAgentUiState> = mutableState.asStateFlow()
    private var session: GeminiLiveSession? = null

    override suspend fun start(options: VoiceAgentStartFrame): Flow<VoiceAgentEvent> {
        val policy = StoredExecutionPolicy(context).read(profile).restrictTo(policyOverride)
        if (policy.mode == ExecutionMode.DEVICE_ONLY || !policy.allowNetwork) {
            localHandoff?.invoke()
            throw localVoiceHandoffFailure()
        }
        if (policy.mode == ExecutionMode.PREFER_DEVICE && localHandoff != null && localReady()) {
            localHandoff.invoke()
            throw localVoiceHandoffFailure()
        }
        requireGeminiNetwork(context, profile)
        check(options.sequenceId == null && options.personaId == null && options.roleId == null) { "Managed agent options are unavailable for direct Gemini" }
        check(session == null) { "Voice session already started" }
        mutableState.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Connecting)
        val live = GeminiLiveSession(profile, context, client)
        session = live
        try { live.start(options) } catch (failure: Throwable) {
            session = null
            runCatching { live.close() }
            if (failure is VoiceAgentSetupException && failure.errorClass == VoiceErrorClass.OUTAGE &&
                failure.code in setOf("gemini_transport", "gemini_send_failed", "gemini_reconnect_required") &&
                policy.mode == ExecutionMode.PREFER_ENDPOINT && localHandoff != null && localReady()) {
                localHandoff.invoke()
                throw localVoiceHandoffFailure()
            }
            if (failure is VoiceAgentSetupException && failure.code == "gemini_setup_timeout" &&
                policy.mode == ExecutionMode.PREFER_ENDPOINT && localHandoff != null && localReady()) {
                localHandoff.invoke()
                throw localVoiceHandoffFailure()
            }
            throw failure
        }
        return live.events
    }
    override fun accept(event: VoiceAgentEvent) { mutableState.value = reduceVoiceAgentState(mutableState.value, event) }
    override suspend fun sendAudio(pcm: ByteArray) { session?.sendAudio(pcm) }
    override suspend fun endTurn() { session?.endTurn() }
    override suspend fun sendText(text: String) { session?.sendText(text) }
    override suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>) { session?.respondToTool(id, name, response) }
    override suspend fun advanceStep(reason: String?) { throw UnsupportedOperationException("Sequences are unavailable for direct Gemini") }
    override suspend fun cancelReply() { session?.cancelReply() }
    override suspend fun stop() {
        val live = session; session = null; live?.close()
        mutableState.value = mutableState.value.copy(phase = VoiceAgentUiState.Phase.Ended)
    }
}

/** A real setup handshake without microphone capture or a generated turn. */
suspend fun testGeminiLiveConnection(profile: ConnectionProfile.Byok, context: Context) {
    val driver = GeminiDirectVoiceAgentDriver(profile, context)
    try { driver.start() } finally { driver.stop() }
}

internal class GeminiLiveSession(private val profile: ConnectionProfile.Byok, context: Context, client: OkHttpClient) : VoiceAgentSession {
    private val queue = VoiceAgentEventQueue()
    override val events: Flow<VoiceAgentEvent> = queue.events
    private val ready = CompletableDeferred<Unit>()
    private val active = AtomicBoolean(false)
    private val openedSocket = AtomicReference<WebSocket?>()
    private val lock = Any()
    private var holding = false
    private var input = ""
    private var output = ""
    private val socket: WebSocket

    init {
        // The provider documents a query key for this API; never expose the URL in diagnostics.
        val url = "https://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
            .toHttpUrl().newBuilder().addQueryParameter("key", profile.apiKey).build()
        val safeClient = client.newBuilder().followRedirects(false).followSslRedirects(false).build()
        val listener = object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                openedSocket.set(webSocket)
                val setup = JSONObject().put("model", "models/${profile.model}")
                    .put("generationConfig", JSONObject().put("responseModalities", org.json.JSONArray().put("AUDIO"))
                        .put("speechConfig", JSONObject().put("voiceConfig", JSONObject().put("prebuiltVoiceConfig", JSONObject().put("voiceName", profile.voice)))))
                    .put("inputAudioTranscription", JSONObject()).put("outputAudioTranscription", JSONObject())
                    .put("realtimeInputConfig", JSONObject().put("automaticActivityDetection", JSONObject().put("disabled", true))
                        .put("activityHandling", "START_OF_ACTIVITY_INTERRUPTS"))
                if (!webSocket.send(JSONObject().put("setup", setup).toString())) fail("gemini_send_failed", VoiceErrorClass.OUTAGE)
            }
            override fun onMessage(webSocket: WebSocket, text: String) {
                if (text.length > MAX_MESSAGE_BYTES || text.toByteArray(Charsets.UTF_8).size > MAX_MESSAGE_BYTES) {
                    fail("gemini_overflow", VoiceErrorClass.OUTAGE); return
                }
                handleMessage(webSocket, text)
            }
            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                if (bytes.size > MAX_MESSAGE_BYTES) { fail("gemini_overflow", VoiceErrorClass.OUTAGE); return }
                handleMessage(webSocket, bytes.utf8())
            }
            private fun handleMessage(webSocket: WebSocket, text: String) {
                try {
                    val message = JSONObject(text)
                    if (message.has("setupComplete")) {
                        active.set(true); ready.complete(Unit)
                        emit(VoiceAgentEvent.State(VoiceAgentStates.LISTENING, "session_ready"))
                    }
                    if (message.has("goAway") || message.has("sessionResumptionUpdate")) {
                        // No resumption was negotiated. A GoAway requires an explicit new session.
                        if (message.has("goAway")) { fail("gemini_reconnect_required", VoiceErrorClass.OUTAGE); webSocket.cancel(); return }
                    }
                    if (message.has("error")) {
                        val code = message.optJSONObject("error")?.optInt("code")
                        when (code) {
                            401, 403 -> fail("gemini_authorization", VoiceErrorClass.RIGHT)
                            429 -> fail("gemini_limit", VoiceErrorClass.LIMIT)
                            else -> fail("gemini_provider_error", VoiceErrorClass.OUTAGE)
                        }
                        webSocket.cancel(); return
                    }
                    val content = message.optJSONObject("serverContent") ?: return
                    if (content.optString("finishReason") in setOf("SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII")) {
                        fail("gemini_safety", VoiceErrorClass.RIGHT); webSocket.cancel(); return
                    }
                    if (content.optBoolean("interrupted")) { output = ""; emit(VoiceAgentEvent.Interrupted) }
                    fun transcript(key: String, isInput: Boolean) {
                        val fragment = content.optJSONObject(key)?.optString("text")?.takeIf { it.isNotEmpty() } ?: return
                        val accumulated = accumulateVoiceAgentTranscript(if (isInput) input else output, fragment).takeLast(16 * 1024)
                        if (isInput) input = accumulated else output = accumulated
                        emit(VoiceAgentEvent.Transcript(isInput, accumulated, false))
                    }
                    transcript("inputTranscription", true); transcript("outputTranscription", false)
                    val parts = content.optJSONObject("modelTurn")?.optJSONArray("parts")
                    if (parts != null) for (i in 0 until parts.length()) {
                        val inline = parts.optJSONObject(i)?.optJSONObject("inlineData") ?: continue
                        if (!inline.optString("mimeType").startsWith("audio/pcm")) continue
                        val pcm = Base64.decode(inline.getString("data"), Base64.DEFAULT)
                        emit(VoiceAgentEvent.State(VoiceAgentStates.SPEAKING))
                        emit(VoiceAgentEvent.Audio(pcm))
                    }
                    if (content.optBoolean("turnComplete")) {
                        if (input.isNotEmpty()) emit(VoiceAgentEvent.Transcript(true, input, true))
                        if (output.isNotEmpty()) emit(VoiceAgentEvent.Transcript(false, output, true))
                        input = ""; output = ""; emit(VoiceAgentEvent.State(VoiceAgentStates.LISTENING))
                    }
                } catch (_: Exception) { fail("gemini_protocol_error", VoiceErrorClass.OUTAGE); webSocket.cancel() }
            }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                NetworkUseGuard.untrack(webSocket)
                when (response?.code) {
                    401, 403 -> fail("gemini_authorization", VoiceErrorClass.RIGHT)
                    429 -> fail("gemini_limit", VoiceErrorClass.LIMIT)
                    else -> fail("gemini_transport", VoiceErrorClass.OUTAGE)
                }
            }
            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                if (code == 1008) fail("gemini_denied", VoiceErrorClass.RIGHT)
                webSocket.close(1000, "closed")
            }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                NetworkUseGuard.untrack(webSocket)
                if (!ready.isCompleted) ready.completeExceptionally(VoiceAgentSetupException("gemini_setup_failed"))
                active.set(false)
                if (code != 1000 && code != 1001) fail("gemini_transport", VoiceErrorClass.OUTAGE)
                else queue.finish(VoiceAgentEndReasons.CLIENT)
            }
        }
        socket = StoredConnectionSelection(context).withCurrent(profile) {
            NetworkUseGuard.create({ safeClient.newWebSocket(Request.Builder().url(url).build(), listener) }, { it.cancel() })
        }
    }
    private companion object { const val MAX_MESSAGE_BYTES = 256 * 1024 }

    private fun emit(event: VoiceAgentEvent) { if (!queue.offer(event)) fail("gemini_overflow", VoiceErrorClass.OUTAGE) }
    private fun fail(code: String, errorClass: VoiceErrorClass) {
        active.set(false); ready.completeExceptionally(VoiceAgentSetupException(code, errorClass))
        queue.finish(VoiceAgentEndReasons.ERROR, VoiceAgentEvent.Failure(code, "Gemini voice session could not continue", fatal = true, errorClass = errorClass))
        openedSocket.get()?.cancel()
    }
    override suspend fun start(options: VoiceAgentStartFrame) {
        try {
            if (withTimeoutOrNull(20_000) { ready.await(); true } == null) throw VoiceAgentSetupException("gemini_setup_timeout")
        } catch (failure: Throwable) { socket.cancel(); NetworkUseGuard.untrack(socket); throw failure }
    }
    private fun send(value: JSONObject): Boolean = synchronized(lock) {
        if (!active.get() || socket.queueSize() > 128_000 || !socket.send(value.toString())) {
            fail("gemini_send_failed", VoiceErrorClass.OUTAGE); socket.cancel(); false
        } else true
    }
    override suspend fun sendAudio(pcm: ByteArray) {
        if (pcm.size > 64_000 || pcm.size % 2 != 0) { fail("gemini_overflow", VoiceErrorClass.OUTAGE); socket.cancel(); return }
        synchronized(lock) {
            if (!holding) {
                if (!send(JSONObject().put("realtimeInput", JSONObject().put("activityStart", JSONObject())))) return
                holding = true
            }
            send(JSONObject().put("realtimeInput", JSONObject().put("audio", JSONObject()
                .put("data", Base64.encodeToString(pcm, Base64.NO_WRAP)).put("mimeType", "audio/pcm;rate=16000"))))
        }
    }
    override suspend fun endTurn() { synchronized(lock) { if (holding) { holding = false; send(JSONObject().put("realtimeInput", JSONObject().put("activityEnd", JSONObject()))); emit(VoiceAgentEvent.State(VoiceAgentStates.PROCESSING)) } } }
    override suspend fun sendText(text: String) {
        if (text.length > 16_384) throw IllegalArgumentException("Text is too long")
        send(JSONObject().put("clientContent", JSONObject().put("turns", org.json.JSONArray().put(JSONObject().put("role", "user").put("parts", org.json.JSONArray().put(JSONObject().put("text", text))))).put("turnComplete", true)))
    }
    override suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>) {
        send(JSONObject().put("toolResponse", JSONObject().put("functionResponses", org.json.JSONArray().put(JSONObject().put("id", id).put("name", name).put("response", JSONObject(response))))))
    }
    override suspend fun cancelReply() {
        synchronized(lock) {
            if (!holding && queue.interrupt()) {
                if (send(JSONObject().put("realtimeInput", JSONObject().put("activityStart", JSONObject())))) holding = true
            }
        }
    }
    override suspend fun advanceStep(reason: String?) { throw UnsupportedOperationException("Sequences are unavailable for direct Gemini") }
    override suspend fun keepAlive(): Boolean = active.get()
    override suspend fun close() {
        active.set(false); ready.completeExceptionally(VoiceAgentSetupException("gemini_closed")); queue.finish(VoiceAgentEndReasons.CLIENT)
        if (!socket.close(1000, "client")) socket.cancel()
        NetworkUseGuard.untrack(socket)
    }
}
