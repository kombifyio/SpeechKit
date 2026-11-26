package io.kombify.speechkit.hybrid

import com.google.firebase.ai.type.ContentBlockedException
import com.google.firebase.ai.type.FirebaseAIOnDeviceNotAvailableException
import com.google.firebase.ai.type.InvalidAPIKeyException
import com.google.firebase.ai.type.PermissionMissingException
import com.google.firebase.ai.type.PromptBlockedException
import com.google.firebase.ai.type.QuotaExceededException
import com.google.firebase.ai.type.RequestTimeoutException
import com.google.firebase.ai.type.PublicPreviewAPI
import com.google.mlkit.genai.common.GenAiException
import io.kombify.speechkit.domain.ExecutionFailure
import io.kombify.speechkit.domain.ExecutionFailureKind
import kotlinx.coroutines.CancellationException

/** Classify documented types only; unknown, denial and policy errors never trigger fallback. */
@OptIn(PublicPreviewAPI::class)
internal suspend fun <T> classifySdkFailure(block: suspend () -> T): T = try {
    block()
} catch (failure: CancellationException) {
    throw failure
} catch (failure: ExecutionFailure) {
    throw failure
} catch (failure: Exception) {
    val causes = generateSequence(failure as Throwable?) { it.cause }.take(12).toList()
    causes.filterIsInstance<CancellationException>().firstOrNull()?.let { throw it }
    val kind = when {
        causes.any { it is PermissionMissingException || it is InvalidAPIKeyException || it is SecurityException } -> ExecutionFailureKind.AUTHORIZATION
        causes.any { it is PromptBlockedException || it is ContentBlockedException } -> ExecutionFailureKind.SAFETY
        causes.filterIsInstance<GenAiException>().any { it.errorCode in listOf(
            GenAiException.ErrorCode.REQUEST_PROCESSING_ERROR,
            GenAiException.ErrorCode.RESPONSE_PROCESSING_ERROR,
            GenAiException.ErrorCode.RESPONSE_GENERATION_ERROR,
        ) } -> ExecutionFailureKind.SAFETY
        causes.filterIsInstance<GenAiException>().any { it.errorCode == GenAiException.ErrorCode.CANCELLED } -> ExecutionFailureKind.INTERRUPTED
        causes.any { it is FirebaseAIOnDeviceNotAvailableException } -> ExecutionFailureKind.UNAVAILABLE
        causes.filterIsInstance<GenAiException>().any { it.errorCode in listOf(
            GenAiException.ErrorCode.NOT_AVAILABLE,
            GenAiException.ErrorCode.NOT_SUPPORTED,
            GenAiException.ErrorCode.AICORE_INCOMPATIBLE,
            GenAiException.ErrorCode.NEEDS_SYSTEM_UPDATE,
        ) } -> ExecutionFailureKind.UNAVAILABLE
        causes.any { it is QuotaExceededException } -> ExecutionFailureKind.POLICY_DENIED
        causes.any { it is RequestTimeoutException } -> ExecutionFailureKind.TRANSIENT
        causes.filterIsInstance<GenAiException>().any { it.errorCode == GenAiException.ErrorCode.BUSY } -> ExecutionFailureKind.TRANSIENT
        else -> ExecutionFailureKind.OTHER
    }
    throw ExecutionFailure(kind, "AI request failed (${kind.name.lowercase()})", failure)
}
