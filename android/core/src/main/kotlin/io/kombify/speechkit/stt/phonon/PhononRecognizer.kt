package io.kombify.speechkit.stt.phonon

import io.kombify.speechkit.stt.parakeet.OnDeviceRecognizer
import io.kombify.speechkit.stt.parakeet.PhononModelFiles

/**
 * On-device Phonon-2.5 speech-to-text through SpeechKit's five-value engine, the same engine and loader as the
 * desktop sidecar `speechkit-stt-sherpa`: Fermion Research's container is checked against its manifest and
 * rearranged in native memory, its encoder weights stay five-valued, and the matrix products run in int8 (ARM dot
 * product). Recordings over 35 seconds are cut into pause-aligned windows by the engine.
 *
 * Loading takes a few seconds and about 350 MB of memory; create one instance per process and reuse it.
 * [transcribe] is synchronized and CPU-bound: call it off the main thread. [close] releases the native memory.
 */
class PhononRecognizer(
    files: PhononModelFiles,
    threads: Int = defaultThreads(),
) : OnDeviceRecognizer {
    private var handle: Long

    init {
        check(PhononNative.available()) { "the Phonon engine is not part of this app build" }
        check(PhononNative.cpuSupported()) { "this processor lacks the instructions the Phonon engine needs" }
        handle = PhononNative.open(files.dir.absolutePath, threads)
    }

    @Synchronized
    override fun transcribe(samples: FloatArray): String {
        check(handle != 0L) { "PhononRecognizer is closed" }
        return String(PhononNative.transcribe(handle, samples), Charsets.UTF_8)
    }

    @Synchronized
    override fun close() {
        if (handle == 0L) return
        PhononNative.close(handle)
        handle = 0L
    }

    companion object {
        /** Up to four threads: the big cores of a typical phone, leaving the rest to the foreground app. */
        fun defaultThreads(): Int = Runtime.getRuntime().availableProcessors().coerceIn(1, 4)
    }
}
