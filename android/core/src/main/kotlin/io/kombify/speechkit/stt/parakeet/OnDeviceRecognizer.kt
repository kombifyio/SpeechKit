package io.kombify.speechkit.stt.parakeet

import java.io.Closeable

/**
 * A loaded on-device speech-to-text model: [ParakeetRecognizer] (Parakeet on ONNX Runtime) or
 * [io.kombify.speechkit.stt.phonon.PhononRecognizer] (Phonon-2.5 on SpeechKit's five-value engine).
 */
interface OnDeviceRecognizer : Closeable {
    /**
     * Transcribes 16 kHz mono [samples] in [-1, 1]. Returns the text, empty for silence. CPU-bound: call it off the
     * main thread.
     */
    fun transcribe(samples: FloatArray): String
}
