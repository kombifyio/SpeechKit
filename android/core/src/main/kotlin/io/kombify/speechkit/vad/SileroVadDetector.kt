package io.kombify.speechkit.vad

import ai.onnxruntime.OnnxTensor
import ai.onnxruntime.OrtEnvironment
import ai.onnxruntime.OrtSession
import android.content.Context
import timber.log.Timber
import java.io.File
import java.nio.FloatBuffer

/**
 * Silero VAD implementation via ONNX Runtime.
 *
 * Mirrors: internal/vad/silero.go SileroDetector.
 * Processes 512-byte PCM frames (256 samples at 16kHz) and returns
 * speech probability in [0.0, 1.0].
 *
 * Model: silero_vad.onnx (~2MB, <1ms per frame on most devices)
 * Expected in assets/models/silero_vad.onnx or app files directory.
 */
class SileroVadDetector(
    context: Context,
) : VadDetector {

    private val env: OrtEnvironment = OrtEnvironment.getEnvironment()
    private val session: OrtSession

    // Silero VAD internal state (LSTM hidden/cell states)
    private var h = FloatArray(2 * 1 * 64) // [2, 1, 64]
    private var c = FloatArray(2 * 1 * 64)
    private val sr = longArrayOf(16000L) // Sample rate tensor

    init {
        val modelPath = resolveModelPath(context)
        val options = OrtSession.SessionOptions().apply {
            setIntraOpNumThreads(1) // VAD is lightweight, single thread is enough
            setOptimizationLevel(OrtSession.SessionOptions.OptLevel.ALL_OPT)
        }
        session = env.createSession(modelPath, options)
        Timber.d("Silero VAD loaded from: $modelPath")
    }

    /**
     * Process a single audio frame and return speech probability.
     *
     * @param pcmFrame 256 samples of 16-bit signed PCM at 16kHz
     * @return speech probability in [0.0, 1.0]
     */
    override fun processFrame(pcmFrame: ShortArray): Float {
        require(pcmFrame.size == FRAME_SAMPLES) {
            "Expected $FRAME_SAMPLES samples, got ${pcmFrame.size}"
        }

        // Convert to float [-1.0, 1.0]
        val floatFrame = FloatArray(pcmFrame.size) { pcmFrame[it].toFloat() / 32768f }

        // Create input tensors
        val inputTensor = OnnxTensor.createTensor(
            env,
            FloatBuffer.wrap(floatFrame),
            longArrayOf(1, floatFrame.size.toLong()),
        )
        val hTensor = OnnxTensor.createTensor(
            env,
            FloatBuffer.wrap(h),
            longArrayOf(2, 1, 64),
        )
        val cTensor = OnnxTensor.createTensor(
            env,
            FloatBuffer.wrap(c),
            longArrayOf(2, 1, 64),
        )
        val srTensor = OnnxTensor.createTensor(env, sr)

        val inputs = mapOf(
            "input" to inputTensor,
            "h" to hTensor,
            "c" to cTensor,
            "sr" to srTensor,
        )

        val outputs = session.run(inputs)

        // Output: [probability, new_h, new_c]
        val outputTensor = outputs[0] as OnnxTensor
        val probability = outputTensor.floatBuffer.get(0)

        // Update LSTM states for next frame
        val newH = outputs[1] as OnnxTensor
        val newC = outputs[2] as OnnxTensor
        newH.floatBuffer.get(h)
        newC.floatBuffer.get(c)

        // Cleanup
        inputTensor.close()
        hTensor.close()
        cTensor.close()
        srTensor.close()
        outputs.close()

        return probability
    }

    override fun reset() {
        h.fill(0f)
        c.fill(0f)
    }

    override fun close() {
        session.close()
        env.close()
    }

    private fun resolveModelPath(context: Context): String {
        // Check files directory first (downloaded models)
        val filesModel = File(context.filesDir, "models/silero_vad.onnx")
        if (filesModel.exists()) return filesModel.absolutePath

        // Copy from assets if available
        val targetFile = filesModel
        targetFile.parentFile?.mkdirs()
        try {
            context.assets.open("models/silero_vad.onnx").use { input ->
                targetFile.outputStream().use { output ->
                    input.copyTo(output)
                }
            }
            return targetFile.absolutePath
        } catch (e: Exception) {
            throw IllegalStateException(
                "Silero VAD model not found. Place silero_vad.onnx in " +
                    "assets/models/ or ${context.filesDir}/models/",
                e,
            )
        }
    }

    companion object {
        /** Number of samples per frame (256 samples = 512 bytes at 16-bit). */
        const val FRAME_SAMPLES = 256
    }
}
