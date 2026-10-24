package io.kombify.speechkit.stt.parakeet

import java.io.File
import java.io.IOException

/**
 * The files of a sherpa-onnx `nemo_transducer` Parakeet export in one
 * directory: encoder, decoder and joiner (int8 preferred) and `tokens.txt`.
 * Mirrors `ResolveModelDir` of the Go package `pkg/speechkit/x/stt/sherpa`, so
 * a directory the desktop accepts is accepted here and vice versa.
 */
class ParakeetModelFiles private constructor(
    val dir: File,
    val encoder: File,
    val decoder: File,
    val joiner: File,
    val tokens: File,
) {
    companion object {
        /**
         * An encoder below this size is a truncated download or another
         * model; Parakeet TDT 0.6B int8 is about 650 MB.
         */
        private const val MIN_ENCODER_BYTES = 100L * 1024 * 1024

        /**
         * Resolves [dir].
         *
         * @throws IOException when a file is missing or the encoder is too
         *   small to be a Parakeet export.
         */
        fun resolve(dir: File): ParakeetModelFiles {
            if (!dir.isDirectory) throw IOException("Parakeet model directory not found: ${dir.path}")
            fun pick(vararg names: String): File =
                names.map { File(dir, it) }.firstOrNull { it.isFile }
                    ?: throw IOException("Parakeet model directory ${dir.path} has no ${names.joinToString(" or ")}")
            val encoder = pick("encoder.int8.onnx", "encoder.onnx")
            val files = ParakeetModelFiles(
                dir = dir,
                encoder = encoder,
                decoder = pick("decoder.int8.onnx", "decoder.onnx"),
                joiner = pick("joiner.int8.onnx", "joiner.onnx"),
                tokens = pick("tokens.txt"),
            )
            if (encoder.length() < MIN_ENCODER_BYTES) {
                throw IOException(
                    "Parakeet encoder ${encoder.path} is ${encoder.length()} bytes; " +
                        "the download is truncated or not a supported model",
                )
            }
            return files
        }

        /** True when [dir] holds a model [resolve] accepts. */
        fun isModelDir(dir: File): Boolean = runCatching { resolve(dir) }.isSuccess
    }
}

/**
 * The ISO 639-1 languages Parakeet TDT 0.6B v3 transcribes; it detects which
 * one is spoken and cannot be pinned. Same list as the desktop catalog
 * profile `stt.local.parakeet`.
 */
object ParakeetLanguages {
    val codes: Set<String> = setOf(
        "bg", "cs", "da", "de", "el", "en", "es", "et", "fi", "fr", "hr", "hu", "it",
        "lt", "lv", "mt", "nl", "pl", "pt", "ro", "ru", "sk", "sl", "sv", "uk",
    )

    /**
     * True when a request pinned to [language] (a BCP-47 tag such as
     * "de-AT", or null/"auto"/"multi" for detection) can be served.
     */
    fun supports(language: String?): Boolean {
        val code = language?.trim()?.lowercase().orEmpty().substringBefore('-').substringBefore('_')
        return code.isEmpty() || code == "auto" || code == "multi" || code in codes
    }
}
