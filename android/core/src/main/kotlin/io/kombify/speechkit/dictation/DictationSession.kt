package io.kombify.speechkit.dictation

import io.kombify.speechkit.audio.AudioSession
import io.kombify.speechkit.vad.VadDetector

/**
 * Manages a single dictation recording session with VAD-based segmentation.
 *
 * Mirrors: pkg/speechkit/dictation_session.go + recording_controller.go concepts.
 * Coordinates audio capture, VAD processing, and segment collection.
 */
interface DictationSession {
    /** Start a new dictation session. */
    suspend fun start()

    /** Stop the session and return collected audio segments. */
    suspend fun stop(): List<AudioSegment>

    /** Cancel the session without producing output. */
    suspend fun cancel()

    /** Whether the session is active. */
    val isActive: Boolean
}

/** A segment of audio identified by VAD as containing speech. */
data class AudioSegment(
    val pcmData: ByteArray,
    val durationMs: Long,
    val startOffsetMs: Long,
) {
    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (other !is AudioSegment) return false
        return pcmData.contentEquals(other.pcmData) && durationMs == other.durationMs && startOffsetMs == other.startOffsetMs
    }

    override fun hashCode(): Int {
        var result = pcmData.contentHashCode()
        result = 31 * result + durationMs.hashCode()
        result = 31 * result + startOffsetMs.hashCode()
        return result
    }
}

/**
 * Collects audio frames and splits them into segments based on VAD.
 *
 * Mirrors: pkg/speechkit/recording_controller.go SegmentCollector interface.
 */
interface SegmentCollector {
    /** Feed a PCM frame for VAD processing. */
    fun feedPcm(frame: ByteArray)

    /** Collect segments from the complete recording. */
    fun collectSegments(fullPcm: ByteArray): List<AudioSegment>

    /** Reset for a new session. */
    fun reset()
}
