package io.kombify.speechkit.stt.phonon

import io.kombify.speechkit.log.VoiceLog

/**
 * The JNI surface of `libspeechkit_phonon.so`, SpeechKit's five-value engine with its Go model loader
 * (`app/cmd/speechkit-phonon-jni`). The core module builds the library for arm64-v8a and x86_64; a build without
 * it reports [available] false and the Phonon models are not offered.
 */
internal object PhononNative {
    @Volatile
    private var loaded = false

    /**
     * Whether the library loads in this process. Only success is remembered: a failed load may be transient.
     */
    fun available(): Boolean {
        if (loaded) return true
        return try {
            System.loadLibrary("speechkit_phonon")
            loaded = true
            true
        } catch (e: UnsatisfiedLinkError) {
            VoiceLog.w(VoiceLog.DICTATION, "phonon natives unavailable for this process", e)
            false
        }
    }

    /** Whether this processor has the instructions the engine is built for (ARM dot product, or x86-64-v3). */
    @JvmStatic external fun cpuSupported(): Boolean

    /** Loads the model in [modelDir]; returns its handle. Throws [java.io.IOException] with the reason. */
    @JvmStatic external fun open(modelDir: String, threads: Int): Long

    /** Transcribes 16 kHz mono samples; returns the UTF-8 text. Throws [IllegalStateException] on failure. */
    @JvmStatic external fun transcribe(handle: Long, samples: FloatArray): ByteArray

    /** Releases the model; the handle is invalid afterwards. */
    @JvmStatic external fun close(handle: Long)

    /**
     * Extracts [names] from a zstd-compressed tar [archive] into [dir], each through a temporary file. Throws
     * [java.io.IOException] when one is missing or the archive is damaged.
     */
    @JvmStatic external fun unpack(archive: String, dir: String, names: Array<String>)
}
