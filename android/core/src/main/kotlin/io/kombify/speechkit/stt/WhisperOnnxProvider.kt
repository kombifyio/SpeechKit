package io.kombify.speechkit.stt

import ai.onnxruntime.OnnxTensor
import ai.onnxruntime.OrtEnvironment
import ai.onnxruntime.OrtSession
import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import timber.log.Timber
import java.io.File
import java.nio.FloatBuffer
import kotlin.time.Duration
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.measureTimedValue

/**
 * On-device Whisper STT via ONNX Runtime.
 *
 * Runs whisper-tiny or whisper-base quantized models directly on the device.
 * No network required -- fully offline capable.
 *
 * Model files are expected in the app's assets or files directory:
 * - encoder: whisper-tiny-encoder.onnx (or whisper-base-encoder.onnx)
 * - decoder: whisper-tiny-decoder.onnx (or whisper-base-decoder.onnx)
 *
 * Audio input: PCM 16kHz 16-bit signed mono (same as all other providers).
 * The provider converts PCM to log-mel spectrogram before inference.
 */
class WhisperOnnxProvider(
    private val context: Context,
    private val modelName: String = "whisper-tiny",
) : SttProvider {

    override val name: String = "local-whisper-onnx"

    private var ortEnv: OrtEnvironment? = null
    private var encoderSession: OrtSession? = null
    private var decoderSession: OrtSession? = null
    private var isLoaded = false

    /**
     * Load ONNX model files. Call before first transcription.
     * Models are loaded lazily on first transcribe() if not called explicitly.
     */
    suspend fun loadModel() = withContext(Dispatchers.IO) {
        if (isLoaded) return@withContext

        val env = OrtEnvironment.getEnvironment()
        ortEnv = env

        val modelDir = File(context.filesDir, "models/$modelName")
        val encoderPath = File(modelDir, "encoder.onnx")
        val decoderPath = File(modelDir, "decoder.onnx")

        if (!encoderPath.exists() || !decoderPath.exists()) {
            // Try to copy from assets
            copyModelFromAssets(modelDir)
        }

        require(encoderPath.exists()) {
            "Whisper encoder model not found at ${encoderPath.absolutePath}. " +
                "Download the model or place it in assets/models/$modelName/"
        }
        require(decoderPath.exists()) {
            "Whisper decoder model not found at ${decoderPath.absolutePath}."
        }

        val sessionOptions = OrtSession.SessionOptions().apply {
            setIntraOpNumThreads(Runtime.getRuntime().availableProcessors().coerceAtMost(4))
            setOptimizationLevel(OrtSession.SessionOptions.OptLevel.ALL_OPT)
        }

        encoderSession = env.createSession(encoderPath.absolutePath, sessionOptions)
        decoderSession = env.createSession(decoderPath.absolutePath, sessionOptions)
        isLoaded = true

        Timber.d("Whisper ONNX model loaded: $modelName (encoder + decoder)")
    }

    override suspend fun transcribe(audio: ByteArray, opts: TranscribeOpts): Result =
        withContext(Dispatchers.IO) {
            if (!isLoaded) loadModel()

            val encoder = encoderSession ?: error("Encoder not loaded")
            val decoder = decoderSession ?: error("Decoder not loaded")

            val (text, elapsed) = measureTimedValue {
                // 1. Convert PCM to float samples [-1.0, 1.0]
                val floatSamples = pcmToFloat(audio)

                // 2. Compute log-mel spectrogram (80 mel bins, 16kHz)
                val melSpec = computeLogMelSpectrogram(floatSamples)

                // 3. Run encoder
                val env = ortEnv ?: error("ORT environment not initialized")
                val melTensor = OnnxTensor.createTensor(
                    env,
                    FloatBuffer.wrap(melSpec),
                    longArrayOf(1, 80, melSpec.size.toLong() / 80),
                )

                val encoderOutputs = encoder.run(mapOf("mel" to melTensor))
                val encoderOut = encoderOutputs[0] as OnnxTensor

                // 4. Run decoder (greedy decoding)
                val decodedTokens = greedyDecode(decoder, encoderOut, env, opts.language)

                // 5. Detokenize
                detokenize(decodedTokens)
            }

            val audioDurationMs = (audio.size.toLong() * 1000) /
                (io.kombify.speechkit.audio.AudioFormat.SAMPLE_RATE *
                    io.kombify.speechkit.audio.AudioFormat.BYTES_PER_SAMPLE)

            Timber.d("Whisper ONNX: '${text.take(50)}...' in ${elapsed.inWholeMilliseconds}ms " +
                "(${audioDurationMs}ms audio, ${elapsed.inWholeMilliseconds.toFloat() / audioDurationMs}x realtime)")

            Result(
                text = text.trim(),
                language = opts.language,
                duration = audioDurationMs.milliseconds,
                provider = name,
                model = modelName,
                confidence = 0.0,
            )
        }

    override suspend fun health() {
        if (!isLoaded) loadModel()
        requireNotNull(encoderSession) { "Encoder session not available" }
        requireNotNull(decoderSession) { "Decoder session not available" }
    }

    fun release() {
        encoderSession?.close()
        decoderSession?.close()
        ortEnv?.close()
        encoderSession = null
        decoderSession = null
        ortEnv = null
        isLoaded = false
    }

    // --- Audio Processing ---

    private fun pcmToFloat(pcm: ByteArray): FloatArray {
        val samples = FloatArray(pcm.size / 2)
        for (i in samples.indices) {
            val sample = ((pcm[i * 2].toInt() and 0xFF) or
                (pcm[i * 2 + 1].toInt() shl 8)).toShort()
            samples[i] = sample.toFloat() / 32768f
        }
        return samples
    }

    /**
     * Compute 80-bin log-mel spectrogram from audio samples.
     *
     * Whisper uses:
     * - 400-sample window (25ms at 16kHz)
     * - 160-sample hop (10ms)
     * - 80 mel filter banks
     * - Log scaling with floor at 1e-10
     */
    private fun computeLogMelSpectrogram(samples: FloatArray): FloatArray {
        val windowSize = 400
        val hopSize = 160
        val nMels = 80
        val nFft = 512

        val numFrames = (samples.size - windowSize) / hopSize + 1
        if (numFrames <= 0) return FloatArray(nMels) // Too short

        val melSpec = FloatArray(nMels * numFrames)

        for (frame in 0 until numFrames) {
            val offset = frame * hopSize

            // Apply Hann window and compute magnitude spectrum
            val magnitudes = FloatArray(nFft / 2 + 1)
            val windowed = FloatArray(nFft)
            for (i in 0 until windowSize) {
                val hannWindow = 0.5f * (1f - kotlin.math.cos(2f * Math.PI.toFloat() * i / windowSize))
                windowed[i] = if (offset + i < samples.size) samples[offset + i] * hannWindow else 0f
            }

            // Simple DFT for magnitude (production would use FFT)
            for (k in magnitudes.indices) {
                var real = 0f
                var imag = 0f
                for (n in windowed.indices) {
                    val angle = -2f * Math.PI.toFloat() * k * n / nFft
                    real += windowed[n] * kotlin.math.cos(angle)
                    imag += windowed[n] * kotlin.math.sin(angle)
                }
                magnitudes[k] = real * real + imag * imag
            }

            // Apply mel filterbank (simplified linear spacing)
            for (mel in 0 until nMels) {
                var energy = 0f
                val melLow = mel * (magnitudes.size - 1) / nMels
                val melHigh = (mel + 1) * (magnitudes.size - 1) / nMels
                for (k in melLow..melHigh.coerceAtMost(magnitudes.size - 1)) {
                    energy += magnitudes[k]
                }
                melSpec[mel * numFrames + frame] = kotlin.math.ln(maxOf(energy, 1e-10f))
            }
        }

        return melSpec
    }

    private fun greedyDecode(
        decoder: OrtSession,
        encoderOut: OnnxTensor,
        env: OrtEnvironment,
        language: String,
    ): List<Int> {
        val maxTokens = 224 // Whisper max sequence length
        val tokens = mutableListOf(SOT_TOKEN)

        // Add language token
        tokens.add(languageToken(language))
        tokens.add(TRANSCRIBE_TOKEN)

        for (step in 0 until maxTokens) {
            val tokenTensor = OnnxTensor.createTensor(
                env,
                arrayOf(tokens.map { it.toLong() }.toLongArray()),
            )

            val outputs = decoder.run(mapOf(
                "encoder_hidden_states" to encoderOut,
                "input_ids" to tokenTensor,
            ))

            val logits = outputs[0] as OnnxTensor
            val logitsData = logits.floatBuffer
            val vocabSize = logitsData.remaining() / tokens.size

            // Get logits for last token position
            val lastTokenLogits = FloatArray(vocabSize)
            logitsData.position((tokens.size - 1) * vocabSize)
            logitsData.get(lastTokenLogits)

            // Argmax
            val nextToken = lastTokenLogits.indices.maxByOrNull { lastTokenLogits[it] } ?: break

            if (nextToken == EOT_TOKEN) break
            tokens.add(nextToken)

            tokenTensor.close()
            outputs.close()
        }

        return tokens.drop(3) // Remove SOT, language, transcribe tokens
    }

    private fun detokenize(tokens: List<Int>): String {
        // Simplified detokenization -- production uses the full Whisper tokenizer
        // For now, map tokens to their byte-pair decoded strings
        // This is a placeholder that returns token IDs as a joined string
        // Real implementation needs the whisper tokenizer vocabulary file
        return tokens.joinToString("") { token ->
            BASIC_VOCAB.getOrElse(token) { "[${token}]" }
        }
    }

    private fun languageToken(language: String): Int = when (language) {
        "de" -> 50261
        "en" -> 50259
        "fr" -> 50265
        "es" -> 50262
        "it" -> 50274
        "pt" -> 50267
        "nl" -> 50271
        else -> 50259 // Default to English
    }

    private fun copyModelFromAssets(targetDir: File) {
        targetDir.mkdirs()
        listOf("encoder.onnx", "decoder.onnx").forEach { filename ->
            try {
                context.assets.open("models/$modelName/$filename").use { input ->
                    File(targetDir, filename).outputStream().use { output ->
                        input.copyTo(output)
                    }
                }
            } catch (e: Exception) {
                Timber.d("Model asset not found: models/$modelName/$filename")
            }
        }
    }

    companion object {
        private const val SOT_TOKEN = 50258
        private const val EOT_TOKEN = 50257
        private const val TRANSCRIBE_TOKEN = 50359

        // Minimal vocab for testing -- real implementation loads from tokenizer.json
        private val BASIC_VOCAB = mapOf<Int, String>()
    }
}
