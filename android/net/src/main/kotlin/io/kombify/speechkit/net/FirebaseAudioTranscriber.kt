package io.kombify.speechkit.net

import android.content.Context
import com.google.firebase.ai.type.*
import io.kombify.speechkit.domain.*
import kotlinx.coroutines.*

/** Cloud audio transcription through the same admitted Firebase project and official SDK. */
class FirebaseAudioTranscriber(private val profile: ConnectionProfile.Firebase, private val context: Context) {
    suspend fun transcribe(bytes: ByteArray, mimeType: String, language: String? = null): String = coroutineScope {
        require(bytes.isNotEmpty() && bytes.size <= MAX_AUDIO_BYTES) { "Audio exceeds the supported request size" }
        require(mimeType.startsWith("audio/")) { "An audio media type is required" }
        val selection = StoredConnectionSelection(context)
        val job = currentCoroutineContext().job
        val guard = selection.withCurrent(profile) {
            val policy = StoredExecutionPolicy(context).read(profile)
            check(policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY) { "Cloud transcription is disabled" }
            NetworkUseGuard.listenIfAllowed { job.cancel(); listOf(job) }
                ?: throw ExecutionFailure(ExecutionFailureKind.POLICY_DENIED, "Cloud transcription is disabled")
        }
        try {
            val ai = FirebaseAiServices.get(context, FirebaseAiProject(profile.applicationId, profile.projectId, profile.apiKey, true, profile.debugAppCheck))
            val response = ai.generativeModel(profile.cloudModel, requestOptions = RequestOptions(timeoutInMillis = 10_000L)).generateContent(content {
                inlineData(bytes, mimeType)
                text("Transcribe the speech verbatim in its original language. Return only the transcript, without commentary." +
                    (language?.takeIf { it.matches(Regex("[A-Za-z-]{2,20}")) }?.let { " Language hint: $it." } ?: ""))
            })
            selection.requireCurrent(profile)
            response.text?.takeIf { it.isNotBlank() } ?: throw ExecutionFailure(ExecutionFailureKind.OTHER, "No speech was transcribed")
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: ExecutionFailure) {
            throw failure
        } catch (failure: Exception) {
            val kind = when (failure) {
                is InvalidAPIKeyException, is PermissionMissingException -> ExecutionFailureKind.AUTHORIZATION
                is PromptBlockedException, is ContentBlockedException -> ExecutionFailureKind.SAFETY
                is QuotaExceededException -> ExecutionFailureKind.POLICY_DENIED
                is RequestTimeoutException, is java.io.IOException -> ExecutionFailureKind.TRANSIENT
                else -> ExecutionFailureKind.OTHER
            }
            throw ExecutionFailure(kind, "Audio transcription failed", failure)
        } finally { guard.close() }
    }

    companion object { const val MAX_AUDIO_BYTES = 14 * 1024 * 1024 }
}
