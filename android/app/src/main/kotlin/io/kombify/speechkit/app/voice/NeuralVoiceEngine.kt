package io.kombify.speechkit.app.voice

import android.content.ComponentCallbacks2
import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import android.os.Build
import android.os.SystemClock
import com.k2fsa.sherpa.onnx.OfflineTts
import com.k2fsa.sherpa.onnx.OfflineTtsConfig
import com.k2fsa.sherpa.onnx.OfflineTtsModelConfig
import com.k2fsa.sherpa.onnx.OfflineTtsVitsModelConfig
import io.kombify.speechkit.assistant.service.NeuralReplyVoice
import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.job
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.io.File
import java.util.Locale
import java.util.concurrent.atomic.AtomicLong

/**
 * Speaks with a downloaded neural voice (Piper VITS on sherpa-onnx), on the device and without a network.
 *
 * [speak] declines (returns false, no sound) whenever the voice cannot be used, so the caller can fall back to the
 * platform engine: no voice for the language, voice not downloaded, runtime not loadable on this device, the model
 * failing to load, or synthesis failing before the first sound. A voice that failed to load is not retried until it
 * is downloaded again.
 *
 * One reply plays at a time; a new one, or [stop], cuts the current one off. Audio is streamed to the output while
 * the next sentence is still being synthesized.
 */
internal class NeuralVoiceEngine private constructor(context: Context) : NeuralReplyVoice {
    private val store = NeuralVoiceStore.get(context)

    /** Bumped by every new reply and by [stop]; synthesis and playback of an older generation end at once. */
    private val generation = AtomicLong()
    private val speaking = Mutex()

    @Volatile
    private var track: AudioTrack? = null

    private class Loaded(val voiceId: String, val tts: OfflineTts)

    private val loadLock = Any()
    private var loaded: Loaded? = null
    private val failedToLoad = HashSet<String>()

    override suspend fun speak(text: String, locale: Locale): Boolean {
        val voice = NeuralVoices.forLocale(locale)
        if (voice == null) {
            VoiceLog.i(VoiceLog.AUDIO, "neural voice: none for locale=${locale.language}, platform voice")
            return false
        }
        return speak(voice, text)
    }

    /** Speaks [text] with [voice]; the Settings sample button calls this directly. */
    suspend fun speak(voice: NeuralVoice, text: String): Boolean {
        if (text.isBlank()) return false
        if (!supported()) return false
        if (!store.isInstalled(voice.model)) {
            VoiceLog.i(VoiceLog.AUDIO, "neural voice: ${voice.id} not downloaded, platform voice")
            return false
        }
        val mine = generation.incrementAndGet()
        stopAudio()
        return withContext(Dispatchers.Default) {
            val job = coroutineContext.job
            speaking.withLock { synthesize(voice, text, mine, job) }
        }
    }

    override fun stop() {
        generation.incrementAndGet()
        stopAudio()
    }

    /** Lets go of [voice]: after a delete, or after a download replaced its files. */
    suspend fun forget(voice: NeuralVoice) {
        stop()
        speaking.withLock {
            synchronized(loadLock) {
                failedToLoad.remove(voice.id)
                if (loaded?.voiceId == voice.id) unload()
            }
        }
    }

    /** Frees the loaded voice (about a hundred megabytes) when the app goes to the background and nothing speaks. */
    fun onTrimMemory(level: Int) {
        if (level < ComponentCallbacks2.TRIM_MEMORY_BACKGROUND) return
        if (!speaking.tryLock()) return
        try {
            synchronized(loadLock) { unload() }
        } finally {
            speaking.unlock()
        }
    }

    private fun synthesize(voice: NeuralVoice, text: String, mine: Long, job: Job): Boolean {
        val tts = load(voice) ?: return false
        val started = SystemClock.elapsedRealtime()
        var firstSoundMs = -1L
        var frames = 0L
        var out: AudioTrack? = null
        val current = { generation.get() == mine && job.isActive }
        try {
            // The callback is a Java class: sherpa's JNI looks its method up by the signature
            // invoke([F)Ljava/lang/Integer;, which a Kotlin lambda does not have.
            val handler = NeuralVoiceCallback.Handler { samples ->
                if (!current()) {
                    0
                } else {
                    val sink = out ?: openTrack(tts.sampleRate()).also {
                        out = it
                        track = it
                        firstSoundMs = SystemClock.elapsedRealtime() - started
                        it.play()
                    }
                    var offset = 0
                    var failed = false
                    while (offset < samples.size) {
                        // A stop pauses and flushes the track; a blocking write would then wait for
                        // a drain that never comes while this thread holds `speaking`.
                        if (!current()) {
                            failed = true
                            break
                        }
                        val n = sink.write(samples, offset, samples.size - offset, AudioTrack.WRITE_NON_BLOCKING)
                        if (n < 0) {
                            failed = true
                            break
                        }
                        if (n == 0) Thread.sleep(WRITE_POLL_MS) else offset += n
                    }
                    if (failed) {
                        0
                    } else {
                        frames += samples.size
                        if (current()) 1 else 0
                    }
                }
            }
            tts.generateWithCallback(text, 0, 1.0f, NeuralVoiceCallback(handler))
        } catch (t: Throwable) {
            VoiceLog.w(VoiceLog.AUDIO, "neural voice ${voice.id} failed to synthesize", t)
            release(out)
            if (frames == 0L) {
                synchronized(loadLock) { failedToLoad.add(voice.id) }
                return false
            }
            return true
        }
        val synthMs = SystemClock.elapsedRealtime() - started
        val sink = out
        if (sink == null) {
            // Cut off before the first sound is handled; nothing speakable in the text goes to the platform voice.
            return !current()
        }
        val audioMs = frames * 1000 / tts.sampleRate()
        VoiceLog.i(
            VoiceLog.AUDIO,
            "neural voice=${voice.id} chars=${text.length} firstSoundMs=$firstSoundMs synthMs=$synthMs " +
                "audioMs=$audioMs realtimeFactor=${"%.2f".format(Locale.ROOT, synthMs.toDouble() / audioMs.coerceAtLeast(1))}",
        )
        drain(sink, frames, tts.sampleRate(), current)
        release(sink)
        return true
    }

    /** Lets the buffered audio play out, or ends early when the reply is cut off. */
    private fun drain(sink: AudioTrack, frames: Long, rate: Int, current: () -> Boolean) {
        runCatching { sink.stop() }
        val deadline = SystemClock.elapsedRealtime() + frames * 1000 / rate + DRAIN_SLACK_MS
        while (current() && sink.playbackHeadPosition < frames && SystemClock.elapsedRealtime() < deadline) {
            Thread.sleep(DRAIN_POLL_MS)
        }
    }

    private fun load(voice: NeuralVoice): OfflineTts? = synchronized(loadLock) {
        loaded?.takeIf { it.voiceId == voice.id }?.let { return it.tts }
        if (voice.id in failedToLoad) return null
        unload()
        val dir = store.directory(voice.model)
        val started = SystemClock.elapsedRealtime()
        try {
            val vits = OfflineTtsVitsModelConfig(
                model = File(dir, voice.modelFileName).absolutePath,
                tokens = File(dir, NeuralVoice.TOKENS_FILE).absolutePath,
                dataDir = File(dir, NeuralVoice.ESPEAK_DIR).absolutePath,
            )
            val config = OfflineTtsConfig(
                model = OfflineTtsModelConfig(
                    vits = vits,
                    numThreads = Runtime.getRuntime().availableProcessors().coerceIn(1, 4),
                    debug = false,
                    provider = "cpu",
                ),
            )
            val tts = OfflineTts(config = config)
            loaded = Loaded(voice.id, tts)
            VoiceLog.i(
                VoiceLog.AUDIO,
                "neural voice loaded voice=${voice.id} rate=${tts.sampleRate()} loadMs=${SystemClock.elapsedRealtime() - started}",
            )
            tts
        } catch (t: Throwable) {
            failedToLoad.add(voice.id)
            VoiceLog.w(VoiceLog.AUDIO, "neural voice ${voice.id} failed to load, platform voice", t)
            null
        }
    }

    private fun unload() {
        loaded?.let { runCatching { it.tts.release() } }
        loaded = null
    }

    private fun openTrack(rate: Int): AudioTrack {
        val minBuffer = AudioTrack.getMinBufferSize(rate, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_FLOAT)
        return AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANT)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setEncoding(AudioFormat.ENCODING_PCM_FLOAT)
                    .setSampleRate(rate)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(minBuffer, rate * Float.SIZE_BYTES / 2))
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
    }

    private fun stopAudio() {
        val active = track ?: return
        runCatching {
            active.pause()
            active.flush()
        }
    }

    private fun release(sink: AudioTrack?) {
        if (sink == null) return
        if (track === sink) track = null
        runCatching { sink.release() }
    }

    companion object {
        private const val WRITE_POLL_MS = 5L
        private const val DRAIN_POLL_MS = 20L
        private const val DRAIN_SLACK_MS = 1_500L

        @Volatile
        private var instance: NeuralVoiceEngine? = null

        fun get(context: Context): NeuralVoiceEngine =
            instance ?: synchronized(this) {
                instance ?: NeuralVoiceEngine(context.applicationContext).also { instance = it }
            }

        /** The runtime ships for the two 64-bit ABIs the app's native engines target. */
        fun supported(): Boolean = Build.SUPPORTED_ABIS.any { it == "arm64-v8a" || it == "x86_64" }
    }
}
