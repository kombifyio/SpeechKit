package io.kombify.speechkit.dictation

import io.kombify.speechkit.audio.AudioFormat
import io.kombify.speechkit.audio.AudioSession
import io.kombify.speechkit.vad.VadDetector
import io.kombify.speechkit.vad.VadConfig
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import timber.log.Timber
import java.io.ByteArrayOutputStream

/**
 * Default dictation session with VAD-based segmentation.
 *
 * Mirrors: pkg/speechkit/recording_controller.go + dictation_session.go
 * Coordinates audio capture, VAD processing, and segment collection.
 */
class DefaultDictationSession(
    private val audioSession: AudioSession,
    private val vadDetector: VadDetector?,
    private val vadConfig: VadConfig = VadConfig(),
    private val scope: CoroutineScope,
) : DictationSession {

    @Volatile
    override var isActive: Boolean = false
        private set

    private var captureJob: Job? = null
    private val segments = mutableListOf<SegmentBuilder>()
    private var currentSpeech: ByteArrayOutputStream? = null
    private var silenceFrameCount = 0
    private var speechFrameCount = 0
    private var totalFrameOffset = 0

    override suspend fun start() {
        if (isActive) return

        isActive = true
        segments.clear()
        currentSpeech = null
        silenceFrameCount = 0
        speechFrameCount = 0
        totalFrameOffset = 0

        vadDetector?.reset()
        audioSession.start()

        captureJob = scope.launch {
            audioSession.pcmFrames.collect { frame ->
                processFrame(frame)
            }
        }

        Timber.d("DictationSession started")
    }

    override suspend fun stop(): List<AudioSegment> {
        if (!isActive) return emptyList()

        isActive = false
        captureJob?.cancel()
        captureJob = null

        val fullPcm = audioSession.stop()

        // Flush any in-progress speech segment
        finalizeCurrentSegment()

        val result = if (segments.isEmpty() && fullPcm.isNotEmpty()) {
            // No VAD or no segments detected -- treat entire recording as one segment
            listOf(
                AudioSegment(
                    pcmData = fullPcm,
                    durationMs = pcmDurationMs(fullPcm.size),
                    startOffsetMs = 0,
                )
            )
        } else {
            segments.map { it.build() }
        }

        Timber.d("DictationSession stopped: ${result.size} segments")
        return result
    }

    override suspend fun cancel() {
        isActive = false
        captureJob?.cancel()
        captureJob = null
        audioSession.stop()
        segments.clear()
        currentSpeech = null
        Timber.d("DictationSession cancelled")
    }

    private fun processFrame(frame: ByteArray) {
        if (vadDetector == null) return // No VAD -> collect everything in stop()

        val samples = ShortArray(frame.size / 2)
        for (i in samples.indices) {
            samples[i] = ((frame[i * 2].toInt() and 0xFF) or
                (frame[i * 2 + 1].toInt() shl 8)).toShort()
        }

        val probability = vadDetector.processFrame(samples)
        val isSpeech = probability >= vadConfig.speechThreshold
        val frameDurationMs = (frame.size.toLong() * 1000) /
            (AudioFormat.SAMPLE_RATE * AudioFormat.BYTES_PER_SAMPLE)

        if (isSpeech) {
            silenceFrameCount = 0
            speechFrameCount++

            if (currentSpeech == null) {
                currentSpeech = ByteArrayOutputStream()
            }
            currentSpeech?.write(frame)
        } else {
            silenceFrameCount++
            val silenceDurationMs = silenceFrameCount * frameDurationMs

            if (currentSpeech != null) {
                if (silenceDurationMs >= vadConfig.minSilenceDurationMs) {
                    finalizeCurrentSegment()
                } else {
                    // Brief silence within speech -- include in segment
                    currentSpeech?.write(frame)
                }
            }
        }

        totalFrameOffset++
    }

    private fun finalizeCurrentSegment() {
        val speech = currentSpeech ?: return
        val data = speech.toByteArray()
        val duration = pcmDurationMs(data.size)

        if (duration >= vadConfig.minSpeechDurationMs) {
            segments.add(
                SegmentBuilder(
                    data = data,
                    durationMs = duration,
                    startOffsetMs = pcmDurationMs(
                        totalFrameOffset * AudioFormat.FRAME_SIZE_BYTES - data.size,
                    ),
                )
            )
        }

        currentSpeech = null
        speechFrameCount = 0
    }

    private fun pcmDurationMs(bytes: Int): Long =
        (bytes.toLong() * 1000) / (AudioFormat.SAMPLE_RATE * AudioFormat.BYTES_PER_SAMPLE)

    private data class SegmentBuilder(
        val data: ByteArray,
        val durationMs: Long,
        val startOffsetMs: Long,
    ) {
        fun build() = AudioSegment(
            pcmData = data,
            durationMs = durationMs,
            startOffsetMs = startOffsetMs,
        )
    }
}
