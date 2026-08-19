package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
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
@JsonClass(generateAdapter = true)
data class CreateVoiceAgentSessionResponse(
    @Json(name = "session_id") val sessionId: String,
    @Json(name = "ws_url") val wsUrl: String,
    @Json(name = "ws_subprotocol") val wsSubprotocol: String? = null,
    val ticket: String = "",
    @Json(name = "expires_at") val expiresAt: String = "",
)

/** Server-side capability flags returned at session-create time. */
@JsonClass(generateAdapter = true)
data class StreamCapabilities(
    /** False → skip the WS round trip and use the batch endpoint. */
    val streaming: Boolean = false,
    val emulation: String = "off",
)

/** Response of POST /v1/dictation/transcribe (subset the client consumes). */
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

@JsonClass(generateAdapter = true)
internal data class ApiErrorEnvelope(val error: ApiErrorBody? = null)

@JsonClass(generateAdapter = true)
internal data class ApiErrorBody(val code: String = "", val message: String = "")

/** Structured failure from the speechkit-server REST surface. */
class SpeechKitApiException(
    val httpStatus: Int,
    val code: String,
    message: String,
) : IOException("$code (HTTP $httpStatus): $message")

/**
 * Minimal REST client for the speechkit-server dictation surface. Pure
 * OkHttp + Moshi; calls suspend on Dispatchers.IO.
 */
class SpeechKitServerApi(
    private val profile: ConnectionProfile.Server,
    private val client: OkHttpClient = defaultOkHttpClient(),
    moshi: Moshi = DictationStreamCodec.defaultMoshi(),
) {
    private val sessionAdapter = moshi.adapter(CreateStreamSessionResponse::class.java)
    private val voiceAgentSessionAdapter =
        moshi.adapter(CreateVoiceAgentSessionResponse::class.java)
    private val batchAdapter = moshi.adapter(BatchTranscribeResponse::class.java)
    private val errorAdapter = moshi.adapter(ApiErrorEnvelope::class.java)

    /** Mints a streaming session ticket. Requires auth per server config. */
    suspend fun createDictationStreamSession(): CreateStreamSessionResponse =
        withContext(Dispatchers.IO) {
            val request = authorized(
                Request.Builder()
                    .url("${profile.normalizedBaseUrl}/v1/dictation/stream/sessions")
                    .post(EMPTY_JSON.toRequestBody(JSON_MEDIA_TYPE)),
            ).build()
            client.newCall(request).execute().use { response ->
                val body = requireSuccess(response)
                sessionAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(response.code, "invalid_response", "empty session response")
            }
        }

    /** Mints a realtime Voice Agent session ticket. */
    suspend fun createVoiceAgentSession(): CreateVoiceAgentSessionResponse =
        withContext(Dispatchers.IO) {
            val request = authorized(
                Request.Builder()
                    .url("${profile.normalizedBaseUrl}/v1/voiceagent/sessions")
                    .post(EMPTY_JSON.toRequestBody(JSON_MEDIA_TYPE)),
            ).build()
            client.newCall(request).execute().use { response ->
                val body = requireSuccess(response)
                voiceAgentSessionAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(
                        response.code,
                        "invalid_response",
                        "empty voice agent session response",
                    )
            }
        }

    /** Tears a streaming session down early. Best-effort. */
    suspend fun deleteDictationStreamSession(sessionId: String): Unit =
        withContext(Dispatchers.IO) {
            val request = authorized(
                Request.Builder()
                    .url("${profile.normalizedBaseUrl}/v1/dictation/stream/sessions/$sessionId")
                    .delete(),
            ).build()
            client.newCall(request).execute().use { requireSuccess(it) }
        }

    /** Batch transcription fallback: one WAV in, one transcript out. */
    suspend fun transcribe(
        wav: ByteArray,
        language: String? = null,
        model: String? = null,
        prompt: String? = null,
    ): BatchTranscribeResponse =
        withContext(Dispatchers.IO) {
            val multipart = MultipartBody.Builder()
                .setType(MultipartBody.FORM)
                .addFormDataPart(
                    "audio",
                    "dictation.wav",
                    wav.toRequestBody(WAV_MEDIA_TYPE),
                )
                .apply {
                    language?.takeIf { it.isNotBlank() }?.let { addFormDataPart("language", it) }
                    model?.takeIf { it.isNotBlank() }?.let { addFormDataPart("model", it) }
                    prompt?.takeIf { it.isNotBlank() }?.let { addFormDataPart("prompt", it) }
                }
                .build()
            val request = authorized(
                Request.Builder()
                    .url("${profile.normalizedBaseUrl}/v1/dictation/transcribe")
                    .post(multipart),
            ).build()
            client.newCall(request).execute().use { response ->
                val body = requireSuccess(response)
                batchAdapter.fromJson(body)
                    ?: throw SpeechKitApiException(response.code, "invalid_response", "empty transcribe response")
            }
        }

    /** True when GET /healthz answers 2xx. Connection-test convenience. */
    suspend fun healthy(): Boolean = withContext(Dispatchers.IO) {
        runCatching {
            val request = Request.Builder()
                .url("${profile.normalizedBaseUrl}/healthz")
                .get()
                .build()
            client.newCall(request).execute().use { it.isSuccessful }
        }.getOrDefault(false)
    }

    private fun authorized(builder: Request.Builder): Request.Builder {
        profile.bearerToken?.takeIf { it.isNotBlank() }?.let {
            builder.header("Authorization", "Bearer $it")
        }
        return builder
    }

    /** Returns the response body on 2xx; throws [SpeechKitApiException] otherwise. */
    private fun requireSuccess(response: Response): String {
        val body = response.body?.string().orEmpty()
        if (response.isSuccessful) return body
        val parsed = runCatching { errorAdapter.fromJson(body)?.error }.getOrNull()
        throw SpeechKitApiException(
            httpStatus = response.code,
            code = parsed?.code ?: "http_${response.code}",
            message = parsed?.message ?: body.take(200).ifEmpty { response.message },
        )
    }

    companion object {
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
