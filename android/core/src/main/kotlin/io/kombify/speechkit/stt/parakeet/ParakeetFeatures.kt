package io.kombify.speechkit.stt.parakeet

import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.exp
import kotlin.math.ln
import kotlin.math.max
import kotlin.math.pow
import kotlin.math.sin
import kotlin.math.sqrt

/**
 * Log-mel features for NeMo Parakeet transducers, computed exactly as
 * sherpa-onnx v1.13.8 does for a `nemo_transducer` model (kaldi-native-fbank
 * 1.22.3 with the options sherpa sets in
 * `OfflineRecognizerTransducerNeMoImpl::PostInit`), so the on-device tier and
 * the desktop sidecar feed the same model the same input:
 *
 * - 25 ms povey-windowed frames every 10 ms, centred (`snip_edges=false`,
 *   edges reflected), pre-emphasis 0.97, no DC removal and no dither;
 * - 512-point power spectrum, librosa-style Slaney mel banks with Slaney
 *   normalisation between 0 Hz and 7600 Hz (Nyquist minus 400 Hz);
 * - natural log floored at the float epsilon, then NeMo `per_feature`
 *   normalisation (zero mean, unit variance per mel bin over the recording).
 *
 * Arithmetic stays in 32-bit floats where kaldi-native-fbank uses them; the
 * spectrum is computed in double precision. Measured against
 * kaldi-native-fbank on the test fixtures, every value agrees to about 1e-4.
 */
internal class ParakeetFeatures(val numBins: Int = 128) {

    private val window = FloatArray(FRAME_LENGTH) { i ->
        (0.5 - 0.5 * cos(2.0 * PI / (FRAME_LENGTH - 1) * i)).pow(0.85).toFloat()
    }
    private val melBanks: Array<MelBin> = slaneyMelBanks(numBins)
    private val fft = RealFft(FFT_SIZE)

    /**
     * Returns the normalised features of 16 kHz mono [samples] in [-1, 1] as
     * a row-major `frames × numBins` array, or an empty array when the
     * recording is shorter than half a frame shift.
     */
    fun compute(samples: FloatArray): Features {
        val frames = numFrames(samples.size)
        val out = FloatArray(frames * numBins)
        if (frames == 0) return Features(out, 0, numBins)
        val frame = FloatArray(FRAME_LENGTH)
        val power = DoubleArray(FFT_SIZE / 2 + 1)
        val epsilon = Math.ulp(1.0f)
        for (t in 0 until frames) {
            extractFrame(samples, t, frame)
            fft.powerSpectrum(frame, power)
            val row = t * numBins
            for (b in 0 until numBins) {
                val bin = melBanks[b]
                var energy = 0f
                for (k in bin.weights.indices) {
                    energy += bin.weights[k] * power[bin.offset + k].toFloat()
                }
                out[row + b] = ln(max(energy, epsilon))
            }
        }
        normalizePerFeature(out, frames, numBins)
        return Features(out, frames, numBins)
    }

    private fun extractFrame(samples: FloatArray, t: Int, frame: FloatArray) {
        val n = samples.size
        val start = FRAME_SHIFT * t + FRAME_SHIFT / 2 - FRAME_LENGTH / 2
        for (s in 0 until FRAME_LENGTH) {
            var i = start + s
            while (i < 0 || i >= n) {
                i = if (i < 0) -i - 1 else 2 * n - 1 - i
            }
            frame[s] = samples[i]
        }
        for (i in FRAME_LENGTH - 1 downTo 1) {
            frame[i] -= PREEMPHASIS * frame[i - 1]
        }
        frame[0] -= PREEMPHASIS * frame[0]
        for (i in 0 until FRAME_LENGTH) frame[i] *= window[i]
    }

    /** Row-major `frames × bins` features. */
    class Features(val values: FloatArray, val frames: Int, val bins: Int)

    private class MelBin(val offset: Int, val weights: FloatArray)

    companion object {
        const val SAMPLE_RATE = 16_000
        private const val FRAME_LENGTH = 400
        private const val FRAME_SHIFT = 160
        private const val FFT_SIZE = 512
        private const val PREEMPHASIS = 0.97f
        private const val HIGH_FREQ = SAMPLE_RATE / 2f - 400f

        /** Frames kaldi-native-fbank emits for [samples] once input is finished. */
        fun numFrames(samples: Int): Int = (samples + FRAME_SHIFT / 2) / FRAME_SHIFT

        private fun melScaleSlaney(hz: Float): Float =
            if (hz <= 1000f) hz * 3f / 200f else 15f + 14.545078505785561f * ln(hz / 1000f)

        private fun inverseMelScaleSlaney(mel: Float): Float =
            if (mel <= 15f) 200f / 3f * mel else 1000f * exp((mel - 15f) * 0.06875177742094911f)

        private fun slaneyMelBanks(numBins: Int): Array<MelBin> {
            val binWidth = SAMPLE_RATE.toFloat() / FFT_SIZE
            val melLow = melScaleSlaney(0f)
            val melHigh = melScaleSlaney(HIGH_FREQ)
            val delta = (melHigh - melLow) / (numBins + 1)
            return Array(numBins) { bin ->
                val left = inverseMelScaleSlaney(melLow + bin * delta)
                val center = inverseMelScaleSlaney(melLow + (bin + 1) * delta)
                val right = inverseMelScaleSlaney(melLow + (bin + 2) * delta)
                val weights = FloatArray(FFT_SIZE / 2 + 1)
                var first = -1
                var last = -1
                for (i in weights.indices) {
                    val hz = binWidth * i
                    if (hz > left && hz < right) {
                        val w = if (hz <= center) (hz - left) / (center - left) else (right - hz) / (right - center)
                        weights[i] = w * (2f / (right - left))
                        if (first == -1) first = i
                        last = i
                    }
                }
                check(first >= 0) { "mel bin $bin is empty; too many mel bins for a $FFT_SIZE-point FFT" }
                MelBin(first, weights.copyOfRange(first, last + 1))
            }
        }

        /** NeMo `per_feature`: per bin, subtract the mean and divide by (std + 1e-5). */
        private fun normalizePerFeature(x: FloatArray, frames: Int, bins: Int) {
            // Sums run in double: a long recording has thousands of frames,
            // and a float accumulator would drift from Eigen's reduction.
            for (b in 0 until bins) {
                var sum = 0.0
                for (t in 0 until frames) sum += x[t * bins + b]
                val mean = (sum / frames).toFloat()
                var squares = 0.0
                for (t in 0 until frames) {
                    val d = (x[t * bins + b] - mean).toDouble()
                    squares += d * d
                }
                val invStd = 1f / (sqrt(squares / frames).toFloat() + 1e-5f)
                for (t in 0 until frames) {
                    val i = t * bins + b
                    x[i] = (x[i] - mean) * invStd
                }
            }
        }
    }
}

/** Radix-2 real FFT of one fixed size, reused across frames. */
internal class RealFft(private val size: Int) {
    private val bits = Integer.numberOfTrailingZeros(size)
    private val re = DoubleArray(size)
    private val im = DoubleArray(size)
    private val cosTable = DoubleArray(size / 2) { cos(2.0 * PI * it / size) }
    private val sinTable = DoubleArray(size / 2) { -sin(2.0 * PI * it / size) }

    init {
        require(size > 1 && size and (size - 1) == 0) { "FFT size must be a power of two" }
    }

    /** Writes |X[k]|² for k in 0..size/2 of [frame] zero-padded to [size]. */
    fun powerSpectrum(frame: FloatArray, out: DoubleArray) {
        for (i in 0 until size) {
            val j = Integer.reverse(i) ushr (32 - bits)
            re[j] = if (i < frame.size) frame[i].toDouble() else 0.0
            im[j] = 0.0
        }
        var half = 1
        while (half < size) {
            val step = size / (half * 2)
            var start = 0
            while (start < size) {
                for (k in 0 until half) {
                    val wr = cosTable[k * step]
                    val wi = sinTable[k * step]
                    val a = start + k
                    val b = a + half
                    val tr = re[b] * wr - im[b] * wi
                    val ti = re[b] * wi + im[b] * wr
                    re[b] = re[a] - tr
                    im[b] = im[a] - ti
                    re[a] += tr
                    im[a] += ti
                }
                start += half * 2
            }
            half *= 2
        }
        for (k in 0..size / 2) out[k] = re[k] * re[k] + im[k] * im[k]
    }
}
