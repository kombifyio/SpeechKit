package io.kombify.speechkit.stt.parakeet

import kotlin.math.min

/**
 * Cuts recordings longer than one decoder pass at pauses, exactly like
 * `SplitAtPauses` of the Go package `pkg/speechkit/x/stt/sherpa`. Parakeet's
 * int8 export transcribes up to about 30 seconds reliably in one pass; longer
 * passes cost quadratic attention memory and drop words. Each segment ends at
 * the quietest 200 ms window between 15 and 28 seconds.
 */
internal object PauseSplitter {
    private const val SAMPLE_RATE = ParakeetFeatures.SAMPLE_RATE
    private const val MAX_SEGMENT_SECONDS = 28
    private const val MIN_SEGMENT_SECONDS = 15
    private const val FRAME_MILLIS = 20
    private const val QUIET_WINDOW_FRAMES = 10

    /** Half-open sample range [start, end). */
    data class Span(val start: Int, val end: Int)

    fun split(samples: FloatArray): List<Span> {
        val maxLen = MAX_SEGMENT_SECONDS * SAMPLE_RATE
        if (samples.size <= maxLen) return listOf(Span(0, samples.size))
        val frameLen = SAMPLE_RATE * FRAME_MILLIS / 1000
        val energy = frameEnergy(samples, frameLen)
        val spans = mutableListOf<Span>()
        var start = 0
        while (samples.size - start > maxLen) {
            val cut = quietestCut(energy, frameLen, start + MIN_SEGMENT_SECONDS * SAMPLE_RATE, start + maxLen)
            spans += Span(start, cut)
            start = cut
        }
        spans += Span(start, samples.size)
        return spans
    }

    private fun frameEnergy(samples: FloatArray, frameLen: Int): DoubleArray {
        val n = (samples.size + frameLen - 1) / frameLen
        return DoubleArray(n) { f ->
            val lo = f * frameLen
            val hi = min((f + 1) * frameLen, samples.size)
            var sum = 0.0
            for (i in lo until hi) sum += samples[i].toDouble() * samples[i]
            sum / (hi - lo)
        }
    }

    private fun quietestCut(energy: DoubleArray, frameLen: Int, lo: Int, hi: Int): Int {
        val firstFrame = lo / frameLen
        val lastFrame = min(hi / frameLen, energy.size) - QUIET_WINDOW_FRAMES
        var best = -1
        var bestEnergy = Double.POSITIVE_INFINITY
        for (f in firstFrame..lastFrame) {
            var sum = 0.0
            for (i in f until f + QUIET_WINDOW_FRAMES) sum += energy[i]
            if (sum < bestEnergy) {
                best = f
                bestEnergy = sum
            }
        }
        if (best < 0) return hi
        return min((best + QUIET_WINDOW_FRAMES / 2) * frameLen, hi)
    }
}
