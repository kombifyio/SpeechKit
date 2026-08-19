package io.kombify.speechkit.stt.system

import android.content.Context
import android.content.Intent
import android.os.Build
import android.speech.RecognitionSupport
import android.speech.RecognitionSupportCallback
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import androidx.annotation.RequiresApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlin.coroutines.resume

/**
 * Support surface for the system on-device recognizer, built for the B-M4
 * onboarding: which languages are installed / downloadable on-device, and a
 * fire-and-forget model download trigger.
 *
 * API gates: `checkRecognitionSupport` and `triggerModelDownload` exist since
 * API 33; below that the query returns null and the trigger no-ops. The
 * dictation session itself does not depend on either — pre-33 devices still
 * dictate through `EXTRA_PREFER_OFFLINE` / the default service.
 */
object SystemSttSupport {

    /** Snapshot of `RecognitionSupport`, all lists as BCP-47 tags. */
    data class LanguageSupport(
        /** Usable offline right now. */
        val installedOnDevice: List<String>,
        /** Downloadable for offline use (superset intent of installed). */
        val supportedOnDevice: List<String>,
        /** Download scheduled / in progress. */
        val pendingOnDevice: List<String>,
        /** Only recognized with a network connection. */
        val online: List<String>,
    )

    /** True when the API 31+ dedicated on-device recognizer exists. */
    fun isOnDeviceRecognitionAvailable(context: Context): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
            SpeechRecognizer.isOnDeviceRecognitionAvailable(context)

    /**
     * Queries on-device language support (API 33+; null below, or when no
     * on-device recognizer exists, or when the platform reports a check
     * error). Pass a [languageTag] to scope the query, null for the full
     * catalog.
     */
    suspend fun checkRecognitionSupport(
        context: Context,
        languageTag: String? = null,
    ): LanguageSupport? {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return null
        if (!isOnDeviceRecognitionAvailable(context)) return null
        return Api33.checkRecognitionSupport(context.applicationContext, languageTag)
    }

    /**
     * Asks the platform to download the on-device model for [languageTag].
     * Fire-and-forget: the download is scheduled with the platform service;
     * progress lands in [LanguageSupport.pendingOnDevice] on a later check.
     * No-op below API 33 or without an on-device recognizer.
     */
    fun triggerModelDownload(context: Context, languageTag: String) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        if (!isOnDeviceRecognitionAvailable(context)) return
        Api33.triggerModelDownload(context.applicationContext, languageTag)
    }

    /** API-33 code isolated behind one annotation so lint stays exact. */
    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    private object Api33 {

        suspend fun checkRecognitionSupport(
            appContext: Context,
            languageTag: String?,
        ): LanguageSupport? = withContext(Dispatchers.Main.immediate) {
            val recognizer = SpeechRecognizer.createOnDeviceSpeechRecognizer(appContext)
            try {
                suspendCancellableCoroutine { continuation ->
                    recognizer.checkRecognitionSupport(
                        supportIntent(languageTag),
                        appContext.mainExecutor,
                        object : RecognitionSupportCallback {
                            override fun onSupportResult(recognitionSupport: RecognitionSupport) {
                                if (continuation.isActive) {
                                    continuation.resume(recognitionSupport.toLanguageSupport())
                                }
                            }

                            override fun onError(error: Int) {
                                if (continuation.isActive) continuation.resume(null)
                            }
                        },
                    )
                }
            } finally {
                recognizer.destroy()
            }
        }

        fun triggerModelDownload(appContext: Context, languageTag: String) {
            appContext.mainExecutor.execute {
                val recognizer = SpeechRecognizer.createOnDeviceSpeechRecognizer(appContext)
                // The API 33 variant hands the request to the platform
                // service; the client handle can be released immediately.
                recognizer.triggerModelDownload(supportIntent(languageTag))
                recognizer.destroy()
            }
        }

        private fun RecognitionSupport.toLanguageSupport() = LanguageSupport(
            installedOnDevice = installedOnDeviceLanguages,
            supportedOnDevice = supportedOnDeviceLanguages,
            pendingOnDevice = pendingOnDeviceLanguages,
            online = onlineLanguages,
        )

        private fun supportIntent(languageTag: String?): Intent =
            Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                putExtra(
                    RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                    RecognizerIntent.LANGUAGE_MODEL_FREE_FORM,
                )
                languageTag?.let { putExtra(RecognizerIntent.EXTRA_LANGUAGE, it) }
            }
    }
}
