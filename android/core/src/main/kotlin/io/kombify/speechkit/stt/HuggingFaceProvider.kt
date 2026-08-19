package io.kombify.speechkit.stt

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import com.squareup.moshi.Moshi
import com.squareup.moshi.JsonClass
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import timber.log.Timber
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlin.time.Duration
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.measureTimedValue

/**
 * HuggingFace Inference API STT provider.
 *
 * Mirrors: internal/stt/huggingface.go HuggingFaceProvider.
 * Uses the OpenAI-compatible /v1/audio/transcriptions endpoint via HF routers.
 */
class HuggingFaceProvider(
    private val token: String,
    private val model: String = DEFAULT_MODEL,
    private val baseUrl: String = DEFAULT_BASE_URL,
) : SttProvider {

    override val name: String = "huggingface"

    private val client = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .build()

    private val moshi = Moshi.Builder().build()
    private val responseAdapter = moshi.adapter(TranscriptionResponse::class.java)

    override suspend fun transcribe(audio: ByteArray, opts: TranscribeOpts): Result =
        withContext(Dispatchers.IO) {
            val resolvedModel = opts.model ?: model
            val url = "$baseUrl/v1/audio/transcriptions"

            val wavData = pcmToWav(audio)

            val body = MultipartBody.Builder()
                .setType(MultipartBody.FORM)
                .addFormDataPart(
                    "file",
                    "audio.wav",
                    wavData.toRequestBody("audio/wav".toMediaType()),
                )
                .addFormDataPart("model", resolvedModel)
                .addFormDataPart("language", opts.language)
                .addFormDataPart("response_format", "json")
                .build()

            val request = Request.Builder()
                .url(url)
                .header("Authorization", "Bearer $token")
                .post(body)
                .build()

            val (result, elapsed) = measureTimedValue {
                val response = client.newCall(request).execute()
                if (!response.isSuccessful) {
                    val errorBody = response.body?.string() ?: "unknown error"
                    throw IOException("HuggingFace API error ${response.code}: $errorBody")
                }

                val json = response.body?.string() ?: throw IOException("Empty response body")
                val parsed = responseAdapter.fromJson(json)
                    ?: throw IOException("Failed to parse response")

                parsed
            }

            val audioDuration = (audio.size.toLong() * 1000 /
                (io.kombify.speechkit.audio.AudioFormat.SAMPLE_RATE *
                    io.kombify.speechkit.audio.AudioFormat.BYTES_PER_SAMPLE)).milliseconds

            Timber.d("HF transcription: ${result.text.length} chars in ${elapsed.inWholeMilliseconds}ms")

            Result(
                text = result.text.trim(),
                language = opts.language,
                duration = audioDuration,
                provider = name,
                model = resolvedModel,
                confidence = 0.0,
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
            throw IOException("HuggingFace health check failed: ${response.code}")
        }
        response.close()
    }

    companion object {
        const val DEFAULT_MODEL = "openai/whisper-large-v3"
        const val DEFAULT_BASE_URL = "https://router.huggingface.co"
    }
}

@JsonClass(generateAdapter = true)
internal data class TranscriptionResponse(
    val text: String,
)

/**
 * Convert raw PCM 16-bit signed mono 16kHz to WAV format.
 * HuggingFace API requires a proper audio file, not raw PCM.
 */
internal fun pcmToWav(pcm: ByteArray): ByteArray {
    val sampleRate = io.kombify.speechkit.audio.AudioFormat.SAMPLE_RATE
    val channels = io.kombify.speechkit.audio.AudioFormat.CHANNELS
    val bitsPerSample = io.kombify.speechkit.audio.AudioFormat.BITS_PER_SAMPLE
    val byteRate = sampleRate * channels * bitsPerSample / 8
    val blockAlign = channels * bitsPerSample / 8
    val dataSize = pcm.size
    val fileSize = 36 + dataSize

    val header = ByteArray(44)
    // RIFF header
    header[0] = 'R'.code.toByte(); header[1] = 'I'.code.toByte()
    header[2] = 'F'.code.toByte(); header[3] = 'F'.code.toByte()
    writeInt32LE(header, 4, fileSize)
    header[8] = 'W'.code.toByte(); header[9] = 'A'.code.toByte()
    header[10] = 'V'.code.toByte(); header[11] = 'E'.code.toByte()
    // fmt chunk
    header[12] = 'f'.code.toByte(); header[13] = 'm'.code.toByte()
    header[14] = 't'.code.toByte(); header[15] = ' '.code.toByte()
    writeInt32LE(header, 16, 16) // chunk size
    writeInt16LE(header, 20, 1)  // PCM format
    writeInt16LE(header, 22, channels)
    writeInt32LE(header, 24, sampleRate)
    writeInt32LE(header, 28, byteRate)
    writeInt16LE(header, 32, blockAlign)
    writeInt16LE(header, 34, bitsPerSample)
    // data chunk
    header[36] = 'd'.code.toByte(); header[37] = 'a'.code.toByte()
    header[38] = 't'.code.toByte(); header[39] = 'a'.code.toByte()
    writeInt32LE(header, 40, dataSize)

    return header + pcm
}

private fun writeInt32LE(buf: ByteArray, offset: Int, value: Int) {
    buf[offset] = (value and 0xFF).toByte()
    buf[offset + 1] = (value shr 8 and 0xFF).toByte()
    buf[offset + 2] = (value shr 16 and 0xFF).toByte()
    buf[offset + 3] = (value shr 24 and 0xFF).toByte()
}

private fun writeInt16LE(buf: ByteArray, offset: Int, value: Int) {
    buf[offset] = (value and 0xFF).toByte()
    buf[offset + 1] = (value shr 8 and 0xFF).toByte()
}
