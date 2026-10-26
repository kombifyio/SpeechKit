package io.kombify.speechkit.stt.parakeet

import java.io.File
import java.io.IOException

/**
 * The files of a model directory the on-device tier can load: a Parakeet export ([ParakeetModelFiles]) or a
 * Phonon-2.5 model in its five-value format ([PhononModelFiles]).
 */
sealed interface OnDeviceModelFiles {
    val dir: File

    /**
     * True when a segment pinned to [language] (a BCP-47 tag such as "de-AT", or null/"auto"/"multi" for detection)
     * can be served.
     */
    fun supports(language: String?): Boolean

    companion object {
        /** Resolves [dir] by what it holds: Fermion's container means Phonon, anything else Parakeet. */
        fun resolve(dir: File): OnDeviceModelFiles =
            if (File(dir, PhononModelFiles.CONTAINER).isFile) PhononModelFiles.resolve(dir) else ParakeetModelFiles.resolve(dir)
    }
}

/**
 * A Phonon-2.5 model directory as Fermion Research publishes it: the five-value container `model.fermion`, its
 * `config.json` and the `packed_manifest.json` that pins the container's SHA-256. Mirrors `ResolveModelDir` of the
 * Go package `pkg/speechkit/x/stt/phonon`. The directory `phonon-2.5-en` (Phonon-2.5 English) transcribes English
 * only; the others the 25 Parakeet languages.
 */
class PhononModelFiles private constructor(override val dir: File, val englishOnly: Boolean) : OnDeviceModelFiles {
    override fun supports(language: String?): Boolean {
        if (!englishOnly) return ParakeetLanguages.supports(language)
        val code = language?.trim()?.lowercase().orEmpty().substringBefore('-').substringBefore('_')
        return code.isEmpty() || code == "auto" || code == "multi" || code == "en"
    }

    companion object {
        const val CONTAINER = "model.fermion"
        const val ENGLISH_DIR = "phonon-2.5-en"

        /** A container below this size is a truncated download; Phonon-2.5's is about 178 MB. */
        private const val MIN_CONTAINER_BYTES = 100L * 1024 * 1024

        /**
         * Resolves [dir]. The engine checks the container against the manifest when it loads.
         *
         * @throws IOException when a file is missing or the container is too small.
         */
        fun resolve(dir: File): PhononModelFiles {
            if (!dir.isDirectory) throw IOException("Phonon model directory not found: ${dir.path}")
            for (name in listOf(CONTAINER, "config.json", "packed_manifest.json")) {
                if (!File(dir, name).isFile) throw IOException("Phonon model directory ${dir.path} has no $name")
            }
            val container = File(dir, CONTAINER)
            if (container.length() < MIN_CONTAINER_BYTES) {
                throw IOException("Phonon container ${container.path} is ${container.length()} bytes; the download is truncated")
            }
            return PhononModelFiles(dir, englishOnly = dir.name == ENGLISH_DIR)
        }
    }
}
