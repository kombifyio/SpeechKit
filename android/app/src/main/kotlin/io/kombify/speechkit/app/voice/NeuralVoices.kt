package io.kombify.speechkit.app.voice

import io.kombify.speechkit.stt.parakeet.ModelArchive
import io.kombify.speechkit.stt.parakeet.ParakeetModel
import io.kombify.speechkit.stt.parakeet.ParakeetModelFile
import java.util.Locale

/**
 * One downloadable neural voice: a Piper VITS voice exported for sherpa-onnx. The same two voices the desktop app
 * speaks with (`pkg/speechkit/x/tts/sherpa/voices.go`), in the int8 export, which is a third of the float size.
 *
 * The archive carries the model, its tokens and the espeak-ng phoneme data. It is pinned by SHA-256 as a whole, so
 * the phoneme data is verified with it; the model and tokens are pinned again on their own so an installed voice can
 * be checked by size without hashing it.
 */
internal class NeuralVoice(
    val id: String,
    /** Name shown in Settings. */
    val name: String,
    /** BCP-47 language subtag of the language the voice speaks. */
    val language: String,
    /** A short sentence in that language for the sample button. */
    val sample: String,
    val modelFileName: String,
    private val archiveFileName: String,
    private val archiveSha256: String,
    private val archiveBytes: Long,
    private val modelSha256: String,
    private val modelBytes: Long,
    private val tokensSha256: String,
    private val tokensBytes: Long,
) {
    /** The descriptor the resumable, hash-checking model store installs. */
    val model: ParakeetModel = ParakeetModel(
        id = "tts.$id",
        name = name,
        dirName = id,
        files = listOf(
            ParakeetModelFile("voice.tar.bz2", "$RELEASE_URL/$archiveFileName", archiveSha256, archiveBytes),
        ),
        archive = ModelArchive(
            fileName = "voice.tar.bz2",
            members = listOf(
                ParakeetModelFile(modelFileName, "", modelSha256, modelBytes),
                ParakeetModelFile(TOKENS_FILE, "", tokensSha256, tokensBytes),
            ),
        ),
    )

    /** Download size in bytes. */
    val downloadBytes: Long get() = archiveBytes

    companion object {
        const val TOKENS_FILE = "tokens.txt"
        const val ESPEAK_DIR = "espeak-ng-data"

        /**
         * Release `tts-models` of k2-fsa/sherpa-onnx. A release asset can be replaced upstream; the SHA-256 pins
         * the content, so a replaced file fails verification instead of installing.
         */
        private const val RELEASE_URL = "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models"
    }
}

/** The voices SpeechKit offers on Android, chosen by the language of the reply. */
internal object NeuralVoices {
    val GERMAN = NeuralVoice(
        id = "vits-piper-de_DE-ramona-low",
        name = "Ramona (Deutsch)",
        language = "de",
        sample = "Hallo, ich bin deine Stimme auf diesem Gerät. Ich spreche auch ohne Internetverbindung.",
        modelFileName = "de_DE-ramona-low.onnx",
        archiveFileName = "vits-piper-de_DE-ramona-low-int8.tar.bz2",
        archiveSha256 = "e532ddcbb09c16a65f56441bbbbf301083d2ea5b18ce937775303942f2b7a8f4",
        archiveBytes = 21_199_380,
        modelSha256 = "501bade50bfa0f098fdc571a5cbefe679c39501e265c2a7fc736166ba9871419",
        modelBytes = 18_555_532,
        tokensSha256 = "42d1a69ed2b91a51928a711aa228ed9f3dc021c6d359a3e9c4f37eb1d20f80bd",
        tokensBytes = 763,
    )

    val ENGLISH = NeuralVoice(
        id = "vits-piper-en_US-ljspeech-medium",
        name = "LJSpeech (English)",
        language = "en",
        sample = "Hello, I am your voice on this device. I speak without an internet connection, too.",
        modelFileName = "en_US-ljspeech-medium.onnx",
        archiveFileName = "vits-piper-en_US-ljspeech-medium-int8.tar.bz2",
        archiveSha256 = "24dc3bd77dd48c291e52c297878d3437c9492f245d823d7f6a06c4bbb67f4b6b",
        archiveBytes = 21_090_429,
        modelSha256 = "206a8403baad4df7642156918a6ebe205798290d82f24d0d0119d7afa8a2738e",
        modelBytes = 19_349_795,
        tokensSha256 = "ef3a7e4a8d1af0c9d4dc45aaae1a6242ebe24a7ed6f3d025a49eb29682784c6d",
        tokensBytes = 940,
    )

    val all: List<NeuralVoice> = listOf(GERMAN, ENGLISH)

    fun byModelId(modelId: String?): NeuralVoice? = all.firstOrNull { it.model.id == modelId }

    /** German for a German locale, English for an English one; null for every other language (platform voice). */
    fun forLocale(locale: Locale): NeuralVoice? = when (locale.language) {
        "de" -> GERMAN
        "en" -> ENGLISH
        else -> null
    }
}
