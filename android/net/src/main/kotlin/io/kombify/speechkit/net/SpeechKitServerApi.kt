package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile
import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi
import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.IOException
import java.util.concurrent.TimeUnit

/** Response of POST /v1/dictation/stream/sessions. */
@JsonClass(generateAdapter = true)
data class CreateStreamSessionResponse(
    @Json(name = "session_id") val sessionId: String,
    @Json(name = "ws_url") val wsUrl: String,
    @Json(name = "ws_subprotocol") val wsSubprotocol: String? = null,
    val ticket: String = "",
    @Json(name = "expires_at") val expiresAt: String = "",
    val capabilities: StreamCapabilities = StreamCapabilities(),
)

/**
 * Response of POST /v1/voiceagent/sessions. Same ticket-then-upgrade shape as
 * the dictation stream, plus the optional LiveKit room for hosts that move
 * audio off the WebSocket (this client keeps audio on the socket).
 */
/** Registered target and its authorized conversation. */
data class RegisteredAgentTarget(
    val agentId: String,
    val aiSessionId: String,
    val harnessId: String,
    val composerSurface: String,
) {
    init {
        require(agentId.isNotBlank() && aiSessionId.isNotBlank() && harnessId.isNotBlank() && composerSurface.isNotBlank())
    }
}

@JsonClass(generateAdapter = true)
data class VoiceAgentSessionRequest(
    val provider: String? = null,
    @Json(name = "target_agent_id") val targetAgentId: String? = null,
    @Json(name = "ai_session_id") val aiSessionId: String? = null,
    @Json(name = "harness_id") val harnessId: String? = null,
    @Json(name = "composer_surface") val composerSurface: String? = null,
)

@JsonClass(generateAdapter = true)
data class CreateAgentChatMetadata(val harnessId: String, val agentId: String)
@JsonClass(generateAdapter = true)
data class CreateAgentChatRequest(val title: String, val metadata: CreateAgentChatMetadata)
@JsonClass(generateAdapter = true)
data class AgentChatResponse(val id: String = "")

@JsonClass(generateAdapter = true)
data class VoiceHarnessDispatch(val agentId: String = "")
@JsonClass(generateAdapter = true)
data class VoiceHarnessAvailability(val allowed: Boolean = false, val state: String = "")
@JsonClass(generateAdapter = true)
data class VoiceHarnessVoice(val voiceAgentEntry: String = "unavailable")
@JsonClass(generateAdapter = true)
data class VoiceHarness(val id: String = "", val dispatch: VoiceHarnessDispatch = VoiceHarnessDispatch(), val availability: VoiceHarnessAvailability = VoiceHarnessAvailability(), val voice: VoiceHarnessVoice = VoiceHarnessVoice())
@JsonClass(generateAdapter = true)
data class VoiceHarnessCatalog(val harnesses: List<VoiceHarness> = emptyList())

@JsonClass(generateAdapter = true)
data class CreateVoiceAgentSessionResponse(
    @Json(name = "session_id") val sessionId: String,
    @Json(name = "ai_session_id") val aiSessionId: String? = null,
    @Json(name = "ws_url") val wsUrl: String,
    @Json(name = "ws_subprotocol") val wsSubprotocol: String? = null,
    val ticket: String = "",
    @Json(name = "expires_at") val expiresAt: String = "",
    /**
     * RFC 3339 deadline of the Voice budget reserved for this session; empty
     * when the server reserved none (self-hosted). Distinct from [expiresAt],
     * which is the WebSocket ticket expiry.
     */
    @Json(name = "budget_expires_at") val budgetExpiresAt: String = "",
)

/** Response of POST /v1/voiceagent/sessions/{id}/renew. */
@JsonClass(generateAdapter = true)
data class RenewVoiceAgentSessionResponse(
    @Json(name = "session_id") val sessionId: String,
    @Json(name = "budget_expires_at") val budgetExpiresAt: String,
)

/** Server-side capability flags returned at session-create time. */
@JsonClass(generateAdapter = true)
data class StreamCapabilities(
    /** False → skip the WS round trip and use the batch endpoint. */
    val streaming: Boolean = false,
    val emulation: String = "off",
)

/** Request of POST /v1/assist/process (subset this client sends). */
@JsonClass(generateAdapter = true)
data class AssistProcessRequest(
    val text: String,
    val locale: String? = null,
    val selection: String? = null,
    val tts: Boolean = false,
)

@JsonClass(generateAdapter = true)
data class AssistProcessResponse(
    val text: String = "",
    @Json(name = "speak_text") val speakText: String = "",
)

@JsonClass(generateAdapter = true)
data class BatchTranscribeResponse(
    val text: String = "",
    val language: String? = null,
    @Json(name = "duration_ms") val durationMs: Long = 0,
    @Json(name = "latency_ms") val latencyMs: Long = 0,
    val provider: String? = null,
    val model: String? = null,
    val confidence: Double? = null,
)

/**
 * Structured failure from the speechkit-server REST surface.
 *
 * @param denial set when the Gateway refused the request because of the user's
 *   kombify plan; surfaces turn it into one localized message with
 *   [PlanDenial.text].
 */
class SpeechKitApiException(
    val httpStatus: Int,
    val code: String,
    message: String,
    val denial: PlanDenial? = null,
    /** The reason class SpeechKit served; surfaces render their text from it. */
    val errorClass: VoiceErrorClass? = null,
) : IOException("$code (HTTP $httpStatus): $message")

/**
 * Minimal REST client for the speechkit-server dictation surface. Pure
 * OkHttp + Moshi; calls suspend on Dispatchers.IO.
 */
class SpeechKitServerApi(
    private val profile: ConnectionProfile.Server,
    private val client: OkHttpClient = defaultOkHttpClient(),
    moshi: Moshi = DictationStreamCodec.defaultMoshi(),
    context: Context? = null,
) {
    private val selection = context?.let { StoredConnectionSelection(it) }
    init {
        require(profile.credentialAvailable) { "Endpoint credentials are unavailable. Open connection settings." }
    }
    private val sessionAdapter = moshi.adapter(CreateStreamSessionResponse::class.java)
    private val createAgentChatAdapter = moshi.adapter(CreateAgentChatRequest::class.java)
    private val agentChatResponseAdapter = moshi.adapter(AgentChatResponse::class.java)
    private val voiceTurnReceiptListAdapter = moshi.adapter(VoiceTurnReceiptList::class.java)
    private val voiceTurnReceiptAdapter = moshi.adapter(VoiceTurnReceipt::class.java)
    private val voiceHarnessCatalogAdapter = moshi.adapter(VoiceHarnessCatalog::class.java)
    private val voiceAgentProfileCatalogAdapter = moshi.adapter(VoiceAgentProfileCatalog::class.java)
    private val voiceAgentReadinessCatalogAdapter = moshi.adapter(VoiceAgentReadinessCatalog::class.java)
    private val voiceAgentRequestAdapter = moshi.adapter(VoiceAgentSessionRequest::class.java)
    private val voiceAgentSessionAdapter =
        moshi.adapter(CreateVoiceAgentSessionResponse::class.java)
    private val renewVoiceAgentSessionAdapter =
        moshi.adapter(RenewVoiceAgentSessionResponse::class.java)
    private val batchAdapter = moshi.adapter(BatchTranscribeResponse::class.java)
    private val assistRequestAdapter = moshi.adapter(AssistProcessRequest::class.java)
    private val assistResponseAdapter = moshi.adapter(AssistProcessResponse::class.java)

    /** Mints a streaming session ticket. Requires auth per server config. */
    suspend fun createDictationStreamSession(): CreateStreamSessionResponse =
        withContext(Dispatchers.IO) {
            val request = authorized(
                Request.Builder()
                    .url(profile.endpoint("/v1/dictation/stream/sessions"))
                    .post(EMPTY_JSON.toRequestBody(JSON_MEDIA_TYPE)),
                mintsSession = true,
            ).build()
            awaitParsed(client.newCall(request)) { response ->
                val body = requireSuccess(response)
                sessionAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(response.code, "invalid_response", "empty session response")
            }
        }

    /** Discover available voice harnesses through the same authenticated Gateway origin. */
    suspend fun discoverVoiceHarnesses(surface: String): List<VoiceHarness> = withContext(Dispatchers.IO) {
        require(Regex("[a-z0-9_-]{1,32}").matches(surface))
        require(profile.normalizedBaseUrl.endsWith("/v1/speechkit")) { "Agent discovery requires a Gateway profile" }
        val origin = profile.normalizedBaseUrl.removeSuffix("/v1/speechkit")
        val request = authorized(Request.Builder().url("$origin/v1/agents/harnesses?surface=$surface").get()).build()
        awaitParsed(client.newCall(request)) { response ->
            val catalog = voiceHarnessCatalogAdapter.fromJson(requireSuccess(response))
                ?: throw SpeechKitApiException(response.code, "invalid_response", "empty agent catalog")
            catalog.harnesses.filter { it.availability.allowed && it.availability.state == "available" && it.voice.voiceAgentEntry != "unavailable" && it.dispatch.agentId.isNotBlank() }
        }
    }

    /** Read the canonical Voice Agent profile catalog from this endpoint. */
    suspend fun voiceAgentProfiles(): List<VoiceAgentProfile> = withContext(Dispatchers.IO) {
        val request = authorized(
            Request.Builder().url(profile.endpoint("/v1/catalog/profiles?mode=voice_agent")).get(),
        ).build()
        awaitParsed(client.newCall(request)) { response ->
            val catalog = voiceAgentProfileCatalogAdapter.fromJson(requireSuccess(response))
                ?: throw SpeechKitApiException(response.code, "invalid_response", "empty Voice Agent profile catalog")
            catalog.profiles.filter { it.mode == "voice_agent" }
        }
    }

    /** Read endpoint-specific runtime readiness for profiles from the catalog. */
    suspend fun voiceAgentReadiness(): List<VoiceAgentReadiness> = withContext(Dispatchers.IO) {
        val request = authorized(
            Request.Builder().url(profile.endpoint("/v1/catalog/readiness")).get(),
        ).build()
        awaitParsed(client.newCall(request)) { response ->
            val catalog = voiceAgentReadinessCatalogAdapter.fromJson(requireSuccess(response))
                ?: throw SpeechKitApiException(response.code, "invalid_response", "empty Voice Agent readiness catalog")
            catalog.readiness.filter { it.mode == "voice_agent" }
        }
    }

    /** Create the selected harness's durable chat. An uncertain create is never replayed. */
    suspend fun createVoiceAgentChat(harness: VoiceHarness, surface: String): RegisteredAgentTarget = withContext(Dispatchers.IO) {
        require(harness.availability.allowed && harness.availability.state == "available" && harness.voice.voiceAgentEntry != "unavailable")
        require(Regex("[a-z0-9][a-z0-9-]{0,127}").matches(harness.id))
        require(harness.dispatch.agentId.isNotBlank())
        require(profile.normalizedBaseUrl.endsWith("/v1/speechkit")) { "Agent chat requires a Gateway profile" }
        val origin = profile.normalizedBaseUrl.removeSuffix("/v1/speechkit")
        val json = createAgentChatAdapter.toJson(CreateAgentChatRequest("SpeechKit", CreateAgentChatMetadata(harness.id, harness.dispatch.agentId)))
        val builder = Request.Builder().url("$origin/v1/ai/sessions").post(json.toRequestBody(JSON_MEDIA_TYPE))
            .header("Idempotency-Key", java.util.UUID.randomUUID().toString())
        val request = authorized(builder, mintsSession = true).build()
        val chatClient = client.newBuilder().retryOnConnectionFailure(false).followRedirects(false).followSslRedirects(false).build()
        awaitParsed(chatClient.newCall(request)) { response ->
                val session = agentChatResponseAdapter.fromJson(requireSuccess(response))
                    ?: throw SpeechKitApiException(response.code, "invalid_response", "empty chat response")
                require(Regex("[A-Za-z0-9_-]{1,128}").matches(session.id))
                RegisteredAgentTarget(harness.dispatch.agentId, session.id, harness.id, surface)
            }
    }

    /** Reconcile server-owned turns even when their original socket event was lost. */
    suspend fun listVoiceTurnReceipts(sessionId: String): List<VoiceTurnReceipt> = withContext(Dispatchers.IO) {
        require(isVoiceSessionId(sessionId))
        val request = authorized(Request.Builder().url(profile.endpoint("/v1/voiceagent/sessions/$sessionId/turns")).get()).build()
        awaitParsed(client.newCall(request)) { response ->
            val result = voiceTurnReceiptListAdapter.fromJson(requireSuccess(response))
                ?: throw SpeechKitApiException(response.code, "invalid_response", "empty turn list")
            require(result.sessionId == sessionId && result.turns.size <= 256)
            result.turns.forEach { turn ->
                require(turn.sessionId == sessionId && turn.turnId.startsWith("$sessionId:") && Regex("[A-Za-z0-9._:-]{1,256}").matches(turn.turnId) && turn.status in setOf("unknown", "dispatched", "completed"))
            }
            result.turns
        }
    }

    /** Fetch a previously observed server turn; an unknown result is never replayed. */
    suspend fun getVoiceTurnReceipt(sessionId: String, turnId: String): VoiceTurnReceipt = withContext(Dispatchers.IO) {
        require(isVoiceSessionId(sessionId) && turnId.startsWith("$sessionId:") && Regex("[A-Za-z0-9._:-]{1,256}").matches(turnId))
        val request = authorized(Request.Builder().url(profile.endpoint("/v1/voiceagent/sessions/$sessionId/turns/$turnId")).get()).build()
        awaitParsed(client.newCall(request)) { response ->
            val receipt = voiceTurnReceiptAdapter.fromJson(requireSuccess(response))
                ?: throw SpeechKitApiException(response.code, "invalid_response", "empty turn receipt")
            require(receipt.sessionId == sessionId && receipt.turnId == turnId && receipt.status in setOf("unknown", "dispatched", "completed"))
            receipt
        }
    }

    /** Mints a realtime Voice Agent session ticket. */
    suspend fun createVoiceAgentSession(target: RegisteredAgentTarget? = null): CreateVoiceAgentSessionResponse =
        withContext(Dispatchers.IO) {
            val payload = if (target == null) EMPTY_JSON else voiceAgentRequestAdapter.toJson(
                VoiceAgentSessionRequest("kombify-agent", target.agentId, target.aiSessionId, target.harnessId, target.composerSurface),
            )
            val builder = Request.Builder()
                .url(profile.endpoint("/v1/voiceagent/sessions"))
                .post(payload.toRequestBody(JSON_MEDIA_TYPE))
            val request = authorized(builder, mintsSession = true).build()
            awaitParsed(client.newCall(request)) { response ->
                val body = requireSuccess(response)
                voiceAgentSessionAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(
                        response.code,
                        "invalid_response",
                        "empty voice agent session response",
                    )
            }
        }

    /**
     * Asks the server to extend a live hosted Voice Agent session with a newly
     * reserved Voice budget. Not idempotent: the server may have adopted the
     * reservation even when the answer never arrived, so callers must not
     * replay a request whose outcome is unknown.
     *
     * Cancelling the caller aborts the in-flight call.
     */
    suspend fun renewVoiceAgentSession(sessionId: String, target: RegisteredAgentTarget? = null): RenewVoiceAgentSessionResponse {
        require(isVoiceSessionId(sessionId)) { "invalid voice agent session id" }
        return withContext(Dispatchers.IO) {
            val request = authorized(
                Request.Builder()
                    .url(profile.endpoint("/v1/voiceagent/sessions/$sessionId/renew"))
                    .post((if (target == null) EMPTY_JSON else voiceAgentRequestAdapter.toJson(VoiceAgentSessionRequest(provider = "kombify-agent"))).toRequestBody(JSON_MEDIA_TYPE)),
            ).build()
            val renewalClient = client.newBuilder().retryOnConnectionFailure(false)
                .followRedirects(false).followSslRedirects(false).build()
            awaitParsed(renewalClient.newCall(request)) { response ->
                val body = requireSuccess(response)
                renewVoiceAgentSessionAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(response.code, "invalid_response", "empty voice agent renewal response")
            }
        }
    }

    /** Tears a streaming session down early. Best-effort. */
    suspend fun deleteDictationStreamSession(sessionId: String): Unit =
        withContext(Dispatchers.IO) {
            val request = authorized(
                Request.Builder()
                    .url(profile.endpoint("/v1/dictation/stream/sessions/$sessionId"))
                    .delete(),
            ).build()
            awaitParsed(client.newCall(request)) { requireSuccess(it) }
        }

    /** Batch transcription: one audio file in, one transcript out. */
    suspend fun transcribe(
        wav: ByteArray,
        language: String? = null,
        model: String? = null,
        prompt: String? = null,
        keyterms: List<String> = emptyList(),
        filename: String = "dictation.wav",
        mediaType: String = "audio/wav",
    ): BatchTranscribeResponse =
        withContext(Dispatchers.IO) {
            val multipart = MultipartBody.Builder()
                .setType(MultipartBody.FORM)
                .addFormDataPart(
                    "audio",
                    filename,
                    wav.toRequestBody(mediaType.toMediaType()),
                )
                .apply {
                    language?.takeIf { it.isNotBlank() }?.let { addFormDataPart("language", it) }
                    model?.takeIf { it.isNotBlank() }?.let { addFormDataPart("model", it) }
                    prompt?.takeIf { it.isNotBlank() }?.let { addFormDataPart("prompt", it) }
                    // The server reads keyterms as one comma-separated field.
                    keyterms.map { it.replace(",", " ").trim() }.filter { it.isNotEmpty() }
                        .takeIf { it.isNotEmpty() }
                        ?.let { addFormDataPart("keyterms", it.joinToString(",")) }
                }
                .build()
            val request = authorized(
                Request.Builder()
                    .url(profile.endpoint("/v1/dictation/transcribe"))
                    .post(multipart),
            ).build()
            awaitParsed(client.newCall(request)) { response ->
                val body = requireSuccess(response)
                batchAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(response.code, "invalid_response", "empty transcribe response")
            }
        }

    /** One-shot Assist turn: spoken (or typed) text in, answer text out. */
    suspend fun processAssist(
        text: String,
        locale: String? = null,
        selection: String? = null,
    ): AssistProcessResponse =
        withContext(Dispatchers.IO) {
            val payload = AssistProcessRequest(
                text = text,
                locale = locale,
                selection = selection,
                tts = false,
            )
            val json = assistRequestAdapter.toJson(payload)
            val request = authorized(
                Request.Builder()
                    .url(profile.endpoint("/v1/assist/process"))
                    .post(json.toRequestBody(JSON_MEDIA_TYPE)),
            ).build()
            awaitParsed(client.newCall(request)) { response ->
                val body = requireSuccess(response)
                assistResponseAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(
                        response.code,
                        "invalid_response",
                        "empty assist response",
                    )
            }
        }

    /** True when GET /healthz answers 2xx. Connection-test convenience. */
    suspend fun healthy(): Boolean = withContext(Dispatchers.IO) {
        try {
            val request = Request.Builder().url(profile.endpoint("/healthz")).get().build()
            awaitParsed(client.newCall(request)) { it.isSuccessful }
        } catch (cancelled: kotlinx.coroutines.CancellationException) { throw cancelled }
        catch (_: Exception) { false }
    }

    /** Cancelling the coroutine cancels the underlying HTTP request and body read. */
    private suspend fun <T> awaitParsed(call: Call, parse: (Response) -> T): T =
        suspendCancellableCoroutine { continuation ->
            val admitted = try {
                val track = { NetworkUseGuard.track(call) { call.cancel() } }
                selection?.withCurrent(profile, track) ?: track()
            } catch (error: Exception) {
                continuation.resumeWith(Result.failure(error))
                return@suspendCancellableCoroutine
            }
            if (!admitted) {
                continuation.resumeWith(Result.failure(IllegalStateException("Network destination is not permitted")))
                return@suspendCancellableCoroutine
            }
            continuation.invokeOnCancellation { call.cancel(); NetworkUseGuard.untrack(call) }
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) {
                    NetworkUseGuard.untrack(call)
                    if (continuation.isActive) continuation.resumeWith(Result.failure(e))
                }
                override fun onResponse(call: Call, response: Response) {
                    val result = runCatching { response.use(parse) }
                    NetworkUseGuard.untrack(call)
                    if (continuation.isActive) continuation.resumeWith(result)
                }
            })
        }

    /**
     * Adds the bearer. A cloud-session bearer close to expiry is renewed first
     * (see [CloudBearer]); any other bearer is sent as the profile has it.
     *
     * @param mintsSession true for the calls that open a hosted voice
     *   session, using [CloudBearer.freshForSession] to avoid sending a
     *   bearer close to expiry.
     */
    private suspend fun authorized(
        builder: Request.Builder,
        mintsSession: Boolean = false,
    ): Request.Builder {
        val job = checkNotNull(kotlinx.coroutines.currentCoroutineContext()[kotlinx.coroutines.Job])
        val register = { NetworkUseGuard.listenIfAllowed { job.cancel(); listOf(job) } }
        val listener = (if (selection != null) selection.withCurrent(profile, register) else register())
            ?: throw IllegalStateException("Network destination is not permitted")
        try {
            val bearer = if (mintsSession) {
                CloudBearer.freshForSession(profile.bearerToken, profile.cloudSessionGeneration)
            } else {
                CloudBearer.fresh(profile.bearerToken, profile.cloudSessionGeneration)
            }
            if (profile.cloudSessionGeneration != null && bearer == null) {
                throw SpeechKitApiException(401, "cloud_session_changed", "The Cloud session is no longer current")
            }
            bearer?.takeIf { it.isNotBlank() }?.let { builder.header("Authorization", "Bearer $it") }
            return builder
        } finally { listener.close() }
    }

    /** Returns the response body on 2xx; throws [SpeechKitApiException] otherwise. */
    private fun requireSuccess(response: Response): String {
        val body = response.body?.string().orEmpty()
        if (response.isSuccessful) return body
        val parsed = parseApiError(body, response.code)
        VoiceLog.w(
            VoiceLog.NET,
            "http ${response.code} code=${parsed.code} host=${profile.normalizedBaseUrl}",
        )
        throw SpeechKitApiException(
            httpStatus = response.code,
            code = parsed.code,
            message = parsed.message ?: body.take(200).ifEmpty { response.message },
            denial = parsed.denial,
            errorClass = parsed.errorClass,
        )
    }

    companion object {
        private val VOICE_SESSION_ID = Regex("[A-Za-z0-9_-]{1,128}")

        /** True for an id that is safe to place in a request path. */
        internal fun isVoiceSessionId(id: String): Boolean = VOICE_SESSION_ID.matches(id)

        private const val EMPTY_JSON = "{}"
        private val JSON_MEDIA_TYPE = "application/json".toMediaType()
        private val WAV_MEDIA_TYPE = "audio/wav".toMediaType()

        /** Shared client: WS ping keepalive + sane timeouts for streaming. */
        fun defaultOkHttpClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(10, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            .pingInterval(20, TimeUnit.SECONDS)
            .build()
    }
}
