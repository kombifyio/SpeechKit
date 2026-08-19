package io.kombify.speechkit.vad

/**
 * Voice Activity Detection interface.
 *
 * Mirrors: internal/vad/silero.go Detector interface.
 * Processes 512-byte PCM frames (16kHz, 16-bit signed mono) and returns
 * speech probability [0.0, 1.0].
 *
 * Note: This runs on the audio thread -- implementations must be non-blocking.
 */
interface VadDetector {
    /**
     * Process a single audio frame and return speech probability.
     * @param pcmFrame 512 bytes of 16-bit signed PCM at 16kHz mono (256 samples)
     * @return speech probability in [0.0, 1.0]
     */
    fun processFrame(pcmFrame: ShortArray): Float

    /** Reset internal state for a new recording session. */
    fun reset()

    /** Release ONNX resources. */
    fun close()
}

/** Configuration for VAD-based audio segmentation. */
data class VadConfig(
    /** Speech probability threshold to consider a frame as speech. */
    val speechThreshold: Float = 0.5f,
    /** Minimum silence duration (ms) to trigger segment end. */
    val minSilenceDurationMs: Int = 700,
    /** Minimum speech duration (ms) to consider a valid segment. */
    val minSpeechDurationMs: Int = 250,
)
