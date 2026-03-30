package io.kombify.speechkit.ai

import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import timber.log.Timber
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * HuggingFace Inference API LLM provider.
 *
 * Uses the text-generation endpoint for AI text actions.
 * Supports any HF-hosted model with the text-generation pipeline.
 *
 * Default model: meta-llama/Llama-3.2-3B-Instruct (good balance of quality/speed)
 */
class HuggingFaceLlmProvider(
    private val token: String,
    private val model: String = DEFAULT_MODEL,
    private val baseUrl: String = "https://router.huggingface.co",
) : LlmProvider {

    override val name: String = "huggingface-llm"

    private val client = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .build()

    private val moshi = Moshi.Builder().build()
    private val requestAdapter = moshi.adapter(ChatRequest::class.java)
    private val responseAdapter = moshi.adapter(ChatResponse::class.java)

    override suspend fun generate(prompt: String, opts: GenerateOpts): GenerateResult =
        withContext(Dispatchers.IO) {
            val startTime = System.currentTimeMillis()

            val messages = buildList {
                opts.systemPrompt?.let {
                    add(ChatMessage(role = "system", content = it))
                }
                add(ChatMessage(role = "user", content = prompt))
            }

            val requestBody = ChatRequest(
                model = model,
                messages = messages,
                max_tokens = opts.maxTokens,
                temperature = opts.temperature,
                stream = false,
            )

            val json = requestAdapter.toJson(requestBody)
            val request = Request.Builder()
                .url("$baseUrl/v1/chat/completions")
                .header("Authorization", "Bearer $token")
                .header("Content-Type", "application/json")
                .post(json.toRequestBody("application/json".toMediaType()))
                .build()

            val response = client.newCall(request).execute()
            if (!response.isSuccessful) {
                val error = response.body?.string() ?: "unknown"
                throw IOException("HF LLM error ${response.code}: $error")
            }

            val body = response.body?.string() ?: throw IOException("Empty response")
            val parsed = responseAdapter.fromJson(body)
                ?: throw IOException("Failed to parse chat response")

            val text = parsed.choices.firstOrNull()?.message?.content ?: ""
            val latency = System.currentTimeMillis() - startTime
            val tokensUsed = parsed.usage?.total_tokens ?: 0

            Timber.d("HF LLM: ${text.length} chars, ${tokensUsed} tokens in ${latency}ms")

            GenerateResult(
                text = text,
                provider = name,
                tokensUsed = tokensUsed,
                latencyMs = latency,
            )
        }

    override suspend fun health() = withContext(Dispatchers.IO) {
        val request = Request.Builder()
            .url("$baseUrl/api/models/$model")
            .header("Authorization", "Bearer $token")
            .get()
            .build()

        val response = client.newCall(request).execute()
        if (!response.isSuccessful) {
            throw IOException("HF LLM health check failed: ${response.code}")
        }
        response.close()
    }

    companion object {
        const val DEFAULT_MODEL = "meta-llama/Llama-3.2-3B-Instruct"
    }
}

// --- API Types ---

@JsonClass(generateAdapter = true)
internal data class ChatRequest(
    val model: String,
    val messages: List<ChatMessage>,
    val max_tokens: Int,
    val temperature: Float,
    val stream: Boolean,
)

@JsonClass(generateAdapter = true)
internal data class ChatMessage(
    val role: String,
    val content: String,
)

@JsonClass(generateAdapter = true)
internal data class ChatResponse(
    val choices: List<ChatChoice>,
    val usage: ChatUsage? = null,
)

@JsonClass(generateAdapter = true)
internal data class ChatChoice(
    val message: ChatMessage,
)

@JsonClass(generateAdapter = true)
internal data class ChatUsage(
    val total_tokens: Int,
)
