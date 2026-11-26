package io.kombify.speechkit.net

import android.content.Context
import android.graphics.Bitmap
import android.util.Base64
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ExecutionFailure
import io.kombify.speechkit.domain.ExecutionFailureKind
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.io.ByteArrayOutputStream
import java.io.OutputStream
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** Side-effect-free text generation against Google's fixed origin. */
class GeminiTextClient(
    private val profile: ConnectionProfile.Byok,
    private val context: Context,
    private val client: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
) {
    private fun encodeImage(image: Bitmap): String {
        val bytes = ByteArrayOutputStream()
        val bounded = object : OutputStream() {
            override fun write(value: Int) {
                require(bytes.size() < MAX_ENCODED_IMAGE_BYTES) { "Image encoding is too large" }
                bytes.write(value)
            }
            override fun write(buffer: ByteArray, offset: Int, length: Int) {
                require(length <= MAX_ENCODED_IMAGE_BYTES - bytes.size()) { "Image encoding is too large" }
                bytes.write(buffer, offset, length)
            }
        }
        require(image.compress(Bitmap.CompressFormat.PNG, 100, bounded)) { "Image encoding failed" }
        val data = bytes.toByteArray()
        return try { Base64.encodeToString(data, Base64.NO_WRAP) } finally { data.fill(0) }
    }

    private companion object { const val MAX_ENCODED_IMAGE_BYTES = 13 * 1024 * 1024 }

    suspend fun generate(prompt: String, image: Bitmap? = null): String {
        image?.let(GenerationImage::validate)
        requireGeminiNetwork(context, profile)
        if (prompt.length > 64 * 1024) throw ExecutionFailure(ExecutionFailureKind.OTHER, "Text request is too large")
        val url = "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite:generateContent".toHttpUrl()
        val parts = JSONArray().put(JSONObject().put("text", prompt))
        if (image != null) {
            val encoded = withContext(Dispatchers.IO) { encodeImage(image) }
            currentCoroutineContext().ensureActive()
            parts.put(JSONObject().put("inline_data", JSONObject().put("mime_type", "image/png").put("data", encoded)))
        }
        val body = JSONObject().put("contents", JSONArray().put(JSONObject().put("parts", parts)))
        val request = Request.Builder().url(url).header("x-goog-api-key", profile.apiKey)
            .post(body.toString().toRequestBody("application/json; charset=utf-8".toMediaType())).build()
        val safeClient = client.newBuilder().followRedirects(false).followSslRedirects(false).build()
        return suspendCancellableCoroutine { continuation ->
            val call = try { StoredConnectionSelection(context).withCurrent(profile) { NetworkUseGuard.create({ safeClient.newCall(request) }, { it.cancel() }) } }
            catch (_: Exception) { continuation.resumeWithException(ExecutionFailure(ExecutionFailureKind.POLICY_DENIED, "Network use is disabled")); return@suspendCancellableCoroutine }
            continuation.invokeOnCancellation { call.cancel(); NetworkUseGuard.untrack(call) }
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) {
                    NetworkUseGuard.untrack(call)
                    if (continuation.isActive) continuation.resumeWithException(ExecutionFailure(ExecutionFailureKind.TRANSIENT, "Gemini text transport failed"))
                }
                override fun onResponse(call: Call, response: Response) {
                    try {
                        response.use {
                            val kind = when (response.code) {
                                401, 403 -> ExecutionFailureKind.AUTHORIZATION
                                429, in 500..599 -> ExecutionFailureKind.TRANSIENT
                                else -> ExecutionFailureKind.OTHER
                            }
                            if (!response.isSuccessful) throw ExecutionFailure(kind, "Gemini text request was rejected")
                            val source = response.body?.source() ?: throw ExecutionFailure(ExecutionFailureKind.OTHER, "Gemini returned an empty response")
                            source.request(256 * 1024L + 1)
                            if (source.buffer.size > 256 * 1024) throw ExecutionFailure(ExecutionFailureKind.OTHER, "Gemini response is too large")
                            val json = JSONObject(source.readUtf8())
                            val feedback = json.optJSONObject("promptFeedback")
                            if (!feedback?.optString("blockReason").isNullOrBlank()) throw ExecutionFailure(ExecutionFailureKind.SAFETY, "Gemini blocked this request")
                            val candidate = json.optJSONArray("candidates")?.optJSONObject(0)
                            if (candidate?.optString("finishReason") in setOf("SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII")) throw ExecutionFailure(ExecutionFailureKind.SAFETY, "Gemini blocked this response")
                            val parts = candidate?.optJSONObject("content")?.optJSONArray("parts")
                            val result = buildString { if (parts != null) for (i in 0 until parts.length()) append(parts.optJSONObject(i)?.optString("text").orEmpty()) }
                            if (result.isBlank()) throw ExecutionFailure(ExecutionFailureKind.OTHER, "Gemini returned no text")
                            if (continuation.isActive) continuation.resume(result)
                        }
                    } catch (failure: Exception) {
                        val safe = failure as? ExecutionFailure ?: ExecutionFailure(ExecutionFailureKind.OTHER, "Gemini returned an invalid response")
                        if (continuation.isActive) continuation.resumeWithException(safe)
                    } finally { NetworkUseGuard.untrack(call) }
                }
            })
        }
    }
}
