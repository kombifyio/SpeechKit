package io.kombify.speechkit.hybrid

import android.content.Context
import io.kombify.speechkit.domain.ExecutionFailure
import io.kombify.speechkit.domain.ExecutionFailureKind
import io.kombify.speechkit.stt.LocalSpeechAdapter
import io.kombify.speechkit.stt.LocalSpeechAudio
import io.kombify.speechkit.stt.LocalSpeechAvailability
import io.kombify.speechkit.stt.LocalSpeechFailure
import io.kombify.speechkit.stt.LocalSpeechFailureKind
import io.kombify.speechkit.stt.LocalSpeechLimits
import io.kombify.speechkit.stt.LocalSpeechResult
import io.kombify.speechkit.stt.readPcm16
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.IOException
import java.util.Locale

/** Only the Basic file recognizer's explicit platform permission precondition uses this cause. */
class LocalAudioPermissionRequired : SecurityException("Basic speech recognition requires audio permission")

/** Optional ML Kit speech engine. It uses only installed models and never opens a microphone. */
class MlKitLocalSpeechAdapter(context: Context) : LocalSpeechAdapter {
    private val context = context.applicationContext
    override val id: String = "google_mlkit"

    // These are the locales documented for the pinned alpha2 SDK. Multi-language and
    // unspecified requests cannot safely be interpreted as the device locale.
    companion object {
        private val advanced = setOf("ar-SA", "cmn-Hans-CN", "cmn-Hant-TW", "da-DK", "de-DE", "en-US", "es-ES", "fr-FR", "hi-IN", "id-ID", "it-IT", "ja-JP", "ko-KR", "nl-NL", "pl-PL", "pt-PT", "ru-RU", "sv-SE", "th-TH", "tr-TR", "vi-VN")
        private val basic = setOf("cmn-Hans-CN", "cmn-Hant-TW", "de-DE", "en-US", "es-ES", "fr-FR", "hi-IN", "it-IT", "ja-JP", "ko-KR", "pl-PL", "pt-BR", "ru-RU", "tr-TR", "vi-VN")
        val supportedLanguageTags: Set<String> = advanced + basic
    }
    private val defaultRegions = mapOf("ar" to "ar-SA", "da" to "da-DK", "de" to "de-DE", "en" to "en-US", "es" to "es-ES", "fr" to "fr-FR", "hi" to "hi-IN", "id" to "id-ID", "it" to "it-IT", "ja" to "ja-JP", "ko" to "ko-KR", "nl" to "nl-NL", "pl" to "pl-PL", "ru" to "ru-RU", "sv" to "sv-SE", "th" to "th-TH", "tr" to "tr-TR", "vi" to "vi-VN")

    private fun language(options: DictationSegmentOptions): String? {
        val requested = options.language?.trim()?.replace('_', '-') ?: return null
        if (requested.isEmpty() || requested.equals("multi", true) || requested.equals("auto", true)) return null
        val tag = Locale.forLanguageTag(requested).toLanguageTag()
        if (tag in setOf("zh-CN", "zh-Hans", "zh-Hans-CN")) return "cmn-Hans-CN"
        if (tag in setOf("zh-TW", "zh-Hant", "zh-Hant-TW")) return "cmn-Hant-TW"
        return if ('-' !in tag) defaultRegions[tag.lowercase(Locale.ROOT)] else tag
    }

    override suspend fun availability(options: DictationSegmentOptions): LocalSpeechAvailability {
        val tag = language(options) ?: return LocalSpeechAvailability.LANGUAGE_UNSUPPORTED
        if (tag !in advanced && tag !in basic) return LocalSpeechAvailability.LANGUAGE_UNSUPPORTED
        return if (readyMode(tag) != null) LocalSpeechAvailability.AVAILABLE else LocalSpeechAvailability.UNAVAILABLE
    }

    private suspend fun readyMode(tag: String): Boolean? {
        for (useAdvanced in listOf(true, false)) {
            if (tag !in (if (useAdvanced) advanced else basic)) continue
            try {
                ImportedAudio(Locale.forLanguageTag(tag), useAdvanced).use { client ->
                    if (client.readiness() == DeviceReadiness.AVAILABLE) return useAdvanced
                }
            } catch (failure: ExecutionFailure) {
                if (failure.kind != ExecutionFailureKind.UNAVAILABLE && failure.kind != ExecutionFailureKind.TRANSIENT) throw mapFailure(failure)
            }
        }
        return null
    }

    override suspend fun transcribe(audio: LocalSpeechAudio, options: DictationSegmentOptions): LocalSpeechResult {
        val tag = language(options) ?: throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "An explicit supported language is required")
        if (tag !in advanced && tag !in basic) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Language is not supported")
        try {
            val mode = readyMode(tag) ?: throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local speech model is not ready")
            // Android's Basic recognizer requires this permission even for a file source.
            // Keep the request in the host; Advanced and native file adapters need no microphone grant here.
            if (!mode && context.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO) != android.content.pm.PackageManager.PERMISSION_GRANTED)
                throw LocalSpeechFailure(LocalSpeechFailureKind.PERMISSION, "Basic speech recognition requires audio permission", LocalAudioPermissionRequired())
            // The content snapshot is owned here; PCM passed by the caller is never mutated.
            val ownedPcm = if (audio !is LocalSpeechAudio.Pcm16) audio.readPcm16(context) else null
            try {
                val pcm = ownedPcm ?: (audio as LocalSpeechAudio.Pcm16).bytes
                if (pcm.isEmpty() || pcm.size % 2 != 0 || pcm.size > LocalSpeechLimits.MAX_PCM_BYTES)
                    throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "PCM input has an invalid length")
                val text = ImportedAudio(Locale.forLanguageTag(tag), mode).use { client ->
                    withContext(Dispatchers.IO) {
                        val file = java.io.File.createTempFile("local-speech-", ".pcm", context.cacheDir)
                        try {
                            file.writeBytes(pcm)
                            client.transcribePcm(file)
                        } finally {
                            file.delete()
                        }
                    }
                }
                return LocalSpeechResult(text, if (mode) "google_mlkit_advanced" else "google_mlkit_basic")
            } finally {
                ownedPcm?.fill(0)
            }
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: LocalSpeechFailure) {
            throw failure
        } catch (failure: ExecutionFailure) {
            throw mapFailure(failure)
        } catch (failure: SecurityException) {
            throw LocalSpeechFailure(LocalSpeechFailureKind.PERMISSION, "Audio access denied", failure)
        } catch (failure: IOException) {
            throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio input cannot be read", failure)
        } catch (failure: IllegalArgumentException) {
            throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio input is invalid", failure)
        }
    }

    private fun mapFailure(failure: ExecutionFailure): LocalSpeechFailure = LocalSpeechFailure(
        when (failure.kind) {
            ExecutionFailureKind.UNAVAILABLE -> LocalSpeechFailureKind.UNAVAILABLE
            ExecutionFailureKind.TRANSIENT -> LocalSpeechFailureKind.TRANSIENT
            ExecutionFailureKind.AUTHORIZATION -> LocalSpeechFailureKind.PERMISSION
            else -> LocalSpeechFailureKind.INPUT
        }, "Local speech recognition failed", failure,
    )
}
