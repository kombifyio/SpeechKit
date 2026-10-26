package io.kombify.speechkit.stt.parakeet

import ai.onnxruntime.OnnxTensor
import ai.onnxruntime.OrtEnvironment
import ai.onnxruntime.OrtSession
import io.kombify.speechkit.onnx.LocalOrt
import java.io.IOException
import java.nio.FloatBuffer
import java.nio.IntBuffer
import java.nio.LongBuffer

/**
 * On-device NVIDIA Parakeet TDT 0.6B speech-to-text through ONNX Runtime: the
 * same sherpa-onnx int8 export and the same decoding as the desktop sidecar
 * `speechkit-stt-sherpa`, without a second native runtime in the app.
 *
 * Pipeline, as sherpa-onnx v1.13.8 runs a `nemo_transducer`: [ParakeetFeatures]
 * → encoder → greedy TDT search over the joiner (token and duration heads)
 * with the LSTM prediction network as decoder. Recordings longer than about
 * 30 seconds are cut at pauses first ([PauseSplitter]).
 *
 * Loading reads about 670 MB and needs about 1 GB of memory; create one
 * instance per process and reuse it. [transcribe] is synchronized and
 * CPU-bound: call it off the main thread. [close] releases the sessions.
 */
class ParakeetRecognizer(
    files: ParakeetModelFiles,
    threads: Int = defaultThreads(),
) : OnDeviceRecognizer {

    private val env: OrtEnvironment = LocalOrt.environment()
    private val encoder: OrtSession
    private val decoder: OrtSession
    private val joiner: OrtSession
    private val tokens: Array<String>
    private val blankId: Int
    private val tdt: Boolean
    private val predLayers: Int
    private val predHidden: Int
    private val features: ParakeetFeatures
    private val decoderInputs: List<String>
    private val encoderInputs: List<String>
    private val joinerInputs: List<String>

    @Volatile
    private var closed = false

    init {
        val options = OrtSession.SessionOptions().apply {
            setIntraOpNumThreads(threads.coerceAtLeast(1))
            setOptimizationLevel(OrtSession.SessionOptions.OptLevel.ALL_OPT)
        }
        val sessions = mutableListOf<OrtSession>()
        try {
            encoder = env.createSession(files.encoder.path, options).also(sessions::add)
            decoder = env.createSession(files.decoder.path, options).also(sessions::add)
            joiner = env.createSession(files.joiner.path, options).also(sessions::add)

            val meta = encoder.metadata.customMetadata
            fun intMeta(key: String): Int = meta[key]?.toIntOrNull()
                ?: throw IOException("Parakeet encoder metadata has no $key; not a sherpa-onnx nemo_transducer export")
            val vocabSize = intMeta("vocab_size") + 1 // NeMo's vocab_size excludes the blank
            predLayers = intMeta("pred_rnn_layers")
            predHidden = intMeta("pred_hidden")
            features = ParakeetFeatures(meta["feat_dim"]?.toIntOrNull() ?: 128)
            val normalize = meta["normalize_type"].orEmpty()
            if (normalize != "per_feature") {
                throw IOException("Parakeet encoder normalize_type is '$normalize'; only per_feature is supported")
            }
            // sherpa-onnx's own test: the export's url names a TDT model.
            tdt = meta["url"].orEmpty().contains("tdt")

            tokens = readTokens(files, vocabSize)
            blankId = vocabSize - 1

            encoderInputs = encoder.inputNames.toList()
            decoderInputs = decoder.inputNames.toList()
            joinerInputs = joiner.inputNames.toList()
            require(encoderInputs.size == 2 && decoderInputs.size == 4 && joinerInputs.size == 2) {
                "unexpected Parakeet ONNX signature: encoder $encoderInputs, decoder $decoderInputs, joiner $joinerInputs"
            }
        } catch (e: Throwable) {
            sessions.forEach { runCatching { it.close() } }
            options.close()
            throw e
        }
        options.close()
    }

    /**
     * Transcribes 16 kHz mono [samples] in [-1, 1]. Returns the text, empty
     * for silence or audio too short to produce a frame.
     */
    @Synchronized
    override fun transcribe(samples: FloatArray): String {
        check(!closed) { "ParakeetRecognizer is closed" }
        val texts = PauseSplitter.split(samples).mapNotNull { span ->
            decodeSpan(samples.copyOfRange(span.start, span.end)).takeIf { it.isNotEmpty() }
        }
        return texts.joinToString(" ")
    }

    @Synchronized
    override fun close() {
        if (closed) return
        closed = true
        encoder.close()
        decoder.close()
        joiner.close()
    }

    private fun decodeSpan(samples: FloatArray): String {
        val feats = features.compute(samples)
        if (feats.frames == 0) return ""
        val (encoded, frames) = runEncoder(feats)
        val ids = if (tdt) greedyTdt(encoded, frames) else greedyRnnt(encoded, frames)
        return detokenize(ids)
    }

    /** Returns the encoder output as one vector per output frame, and the frame count. */
    private fun runEncoder(feats: ParakeetFeatures.Features): Pair<Array<FloatArray>, Int> {
        // (T, C) row-major → (1, C, T)
        val t = feats.frames
        val c = feats.bins
        val transposed = FloatArray(t * c)
        for (i in 0 until t) {
            for (j in 0 until c) transposed[j * t + i] = feats.values[i * c + j]
        }
        OnnxTensor.createTensor(env, FloatBuffer.wrap(transposed), longArrayOf(1, c.toLong(), t.toLong())).use { x ->
            OnnxTensor.createTensor(env, LongBuffer.wrap(longArrayOf(t.toLong())), longArrayOf(1)).use { len ->
                encoder.run(mapOf(encoderInputs[0] to x, encoderInputs[1] to len)).use { out ->
                    val output = out.get(0) as OnnxTensor
                    val shape = output.info.shape // (1, D, T')
                    val dim = shape[1].toInt()
                    val total = shape[2].toInt()
                    val frames = when (val lengths = (out.get(1) as OnnxTensor).value) {
                        is LongArray -> lengths[0].toInt()
                        is IntArray -> lengths[0]
                        else -> total
                    }.coerceAtMost(total)
                    val flat = output.floatBuffer
                    val rows = Array(frames) { FloatArray(dim) }
                    for (d in 0 until dim) {
                        val base = d * total
                        for (f in 0 until frames) rows[f][d] = flat.get(base + f)
                    }
                    return rows to frames
                }
            }
        }
    }

    /** Prediction network state: output vector and the two LSTM states. */
    private class DecoderState(val output: FloatArray, val h: FloatArray, val c: FloatArray)

    private fun runDecoder(token: Int, h: FloatArray, c: FloatArray): DecoderState {
        val stateShape = longArrayOf(predLayers.toLong(), 1, predHidden.toLong())
        OnnxTensor.createTensor(env, IntBuffer.wrap(intArrayOf(token)), longArrayOf(1, 1)).use { targets ->
            OnnxTensor.createTensor(env, IntBuffer.wrap(intArrayOf(1)), longArrayOf(1)).use { targetLength ->
                OnnxTensor.createTensor(env, FloatBuffer.wrap(h), stateShape).use { hIn ->
                    OnnxTensor.createTensor(env, FloatBuffer.wrap(c), stateShape).use { cIn ->
                        val inputs = mapOf(
                            decoderInputs[0] to targets,
                            decoderInputs[1] to targetLength,
                            decoderInputs[2] to hIn,
                            decoderInputs[3] to cIn,
                        )
                        decoder.run(inputs).use { out ->
                            // outputs: decoder output (1, H, 1), lengths, next states
                            return DecoderState(
                                output = (out.get(0) as OnnxTensor).floatBuffer.toArray(),
                                h = (out.get(2) as OnnxTensor).floatBuffer.toArray(),
                                c = (out.get(3) as OnnxTensor).floatBuffer.toArray(),
                            )
                        }
                    }
                }
            }
        }
    }

    private fun runJoiner(frame: FloatArray, decoderOutput: FloatArray): FloatArray {
        OnnxTensor.createTensor(env, FloatBuffer.wrap(frame), longArrayOf(1, frame.size.toLong(), 1)).use { enc ->
            OnnxTensor.createTensor(env, FloatBuffer.wrap(decoderOutput), longArrayOf(1, decoderOutput.size.toLong(), 1)).use { dec ->
                joiner.run(mapOf(joinerInputs[0] to enc, joinerInputs[1] to dec)).use { out ->
                    return (out.get(0) as OnnxTensor).floatBuffer.toArray()
                }
            }
        }
    }

    private fun initialState(): DecoderState {
        val size = predLayers * predHidden
        return runDecoder(blankId, FloatArray(size), FloatArray(size))
    }

    /** sherpa-onnx `DecodeOneTDT`: the duration head says how many frames to advance. */
    private fun greedyTdt(encoded: Array<FloatArray>, frames: Int): List<Int> {
        val vocab = tokens.size
        val out = mutableListOf<Int>()
        var state = initialState()
        var tokensThisFrame = 0
        var t = 0
        while (t < frames) {
            val logits = runJoiner(encoded[t], state.output)
            val y = argmax(logits, 0, vocab)
            var skip = argmax(logits, vocab, logits.size) - vocab
            if (y != blankId) {
                out += y
                state = runDecoder(y, state.h, state.c)
                tokensThisFrame += 1
            }
            if (skip > 0) tokensThisFrame = 0
            if (tokensThisFrame >= MAX_TOKENS_PER_FRAME) {
                tokensThisFrame = 0
                skip = 1
            }
            if (y == blankId && skip == 0) {
                tokensThisFrame = 0
                skip = 1
            }
            t += skip
        }
        return out
    }

    /** sherpa-onnx `DecodeOne`: plain RNN-T, up to ten symbols per frame. */
    private fun greedyRnnt(encoded: Array<FloatArray>, frames: Int): List<Int> {
        val out = mutableListOf<Int>()
        var state = initialState()
        for (t in 0 until frames) {
            var symbols = 0
            while (symbols < MAX_SYMBOLS_PER_FRAME) {
                val logits = runJoiner(encoded[t], state.output)
                val y = argmax(logits, 0, tokens.size)
                if (y == blankId) break
                out += y
                state = runDecoder(y, state.h, state.c)
                symbols += 1
            }
        }
        return out
    }

    private fun detokenize(ids: List<Int>): String =
        ids.joinToString("") { tokens[it] }.replace(WORD_BOUNDARY, ' ').trim()

    companion object {
        private const val MAX_TOKENS_PER_FRAME = 5
        private const val MAX_SYMBOLS_PER_FRAME = 10
        private const val WORD_BOUNDARY = '▁'

        /** Up to four threads: the encoder scales little beyond that on phones. */
        fun defaultThreads(): Int = Runtime.getRuntime().availableProcessors().coerceIn(1, 4)

        private fun readTokens(files: ParakeetModelFiles, vocabSize: Int): Array<String> {
            val table = arrayOfNulls<String>(vocabSize)
            files.tokens.forEachLine(Charsets.UTF_8) { line ->
                val split = line.trimEnd().lastIndexOf(' ')
                if (split <= 0) return@forEachLine
                val id = line.substring(split + 1).trim().toIntOrNull() ?: return@forEachLine
                if (id in 0 until vocabSize) table[id] = line.substring(0, split)
            }
            val missing = table.indexOfFirst { it == null }
            if (missing >= 0) {
                throw IOException("Parakeet tokens.txt has no token $missing of $vocabSize; it does not match the model")
            }
            if (table[vocabSize - 1] != "<blk>") {
                throw IOException("Parakeet tokens.txt does not end with the blank token <blk>")
            }
            return table.requireNoNulls()
        }

        private fun argmax(v: FloatArray, from: Int, to: Int): Int {
            var best = from
            for (i in from + 1 until to) if (v[i] > v[best]) best = i
            return best
        }

        private fun FloatBuffer.toArray(): FloatArray {
            val array = FloatArray(remaining())
            get(array)
            return array
        }
    }
}
