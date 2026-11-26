package io.kombify.speechkit.hybrid

import android.content.Context
import android.net.Uri
import com.google.mlkit.genai.common.FeatureStatus
import com.google.mlkit.genai.common.DownloadStatus as MlDownloadStatus
import com.google.mlkit.genai.common.audio.AudioSource
import com.google.mlkit.genai.speechrecognition.SpeechRecognition
import com.google.mlkit.genai.speechrecognition.SpeechRecognizerOptions
import com.google.mlkit.genai.speechrecognition.SpeechRecognizerResponse
import com.google.mlkit.genai.speechrecognition.speechRecognizerOptions
import com.google.mlkit.genai.speechrecognition.speechRecognizerRequest
import kotlinx.coroutines.flow.collect
import java.io.Closeable
import java.io.File
import java.util.Locale

/** On-device one-shot speech recognition. The host retains URI permission for the turn. */
class ImportedAudio(locale: Locale, advanced: Boolean = true) : Closeable {
    private val recognizer = SpeechRecognition.getClient(speechRecognizerOptions {
        this.locale = locale
        preferredMode = if (advanced) SpeechRecognizerOptions.Mode.MODE_ADVANCED else SpeechRecognizerOptions.Mode.MODE_BASIC
    })

    suspend fun readiness(): DeviceReadiness = classifySdkFailure { when (recognizer.checkStatus()) {
        FeatureStatus.UNAVAILABLE -> DeviceReadiness.UNAVAILABLE
        FeatureStatus.DOWNLOADABLE -> DeviceReadiness.DOWNLOADABLE
        FeatureStatus.DOWNLOADING -> DeviceReadiness.DOWNLOADING
        FeatureStatus.AVAILABLE -> DeviceReadiness.AVAILABLE
        else -> DeviceReadiness.UNKNOWN
    } }

    suspend fun download(onProgress: (DownloadProgress) -> Unit) = classifySdkFailure {
        recognizer.download().collect { status ->
            when (status) {
                is MlDownloadStatus.DownloadStarted -> onProgress(DownloadProgress.Started(status.bytesToDownload))
                is MlDownloadStatus.DownloadProgress -> onProgress(DownloadProgress.Progress(status.totalBytesDownloaded))
                is MlDownloadStatus.DownloadCompleted -> onProgress(DownloadProgress.Completed)
                is MlDownloadStatus.DownloadFailed -> throw status.e
            }
        }
    }

    suspend fun transcribe(context: Context, uri: Uri): String = recognize {
        AudioSource.fromUri(context.applicationContext, uri)
    }

    internal suspend fun transcribePcm(file: File): String = recognize {
        AudioSource.fromFile(file)
    }

    private suspend fun recognize(openSource: () -> AudioSource): String = classifySdkFailure {
        check(readiness() == DeviceReadiness.AVAILABLE) { "Speech recognition model is not ready" }
        val source = openSource()
        try {
            val request = speechRecognizerRequest { audioSource = source }
            val parts = mutableListOf<String>()
            var completed = false
            recognizer.startRecognition(request).collect { response ->
                when (response) {
                    is SpeechRecognizerResponse.FinalTextResponse -> parts.add(response.text)
                    is SpeechRecognizerResponse.ErrorResponse -> throw response.e
                    is SpeechRecognizerResponse.CompletedResponse -> completed = true
                    else -> Unit
                }
            }
            check(completed) { "Speech recognition ended before completion" }
            parts.joinToString(" ")
        } finally {
            source.close()
        }
    }

    override fun close() = recognizer.close()
}
