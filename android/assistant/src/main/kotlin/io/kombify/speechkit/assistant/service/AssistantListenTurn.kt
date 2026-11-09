package io.kombify.speechkit.assistant.service

import io.kombify.speechkit.assistant.intent.ActionResult
import io.kombify.speechkit.assistant.intent.AssistantIntent
import io.kombify.speechkit.assistant.intent.CompanionTurnExecutor
import io.kombify.speechkit.assistant.intent.IntentRouter
import io.kombify.speechkit.assistant.intent.IntentType
import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import kotlinx.coroutines.CancellationException

/**
 * One system-assistant listen: STT, then Assist (or a Companion turn).
 *
 * Empty recognizer finals stay [EmptyFinal]; mic permission and capture
 * failures stay typed and never collapse into a generic mic-unavailable
 * fall-through.
 */
sealed interface AssistantTurnOutcome {
    data class Heard(val text: String, val assist: ActionResult) : AssistantTurnOutcome
    data object EmptyFinal : AssistantTurnOutcome
    data object NoSpeech : AssistantTurnOutcome
    data object MicPermissionDenied : AssistantTurnOutcome
    data object MicUnavailable : AssistantTurnOutcome
    data class StreamFailed(val detail: String?) : AssistantTurnOutcome
    data object Timeout : AssistantTurnOutcome
    data class Closed(val detail: String?) : AssistantTurnOutcome
}

class AssistantListenTurn(
    private val sessionFactory: suspend () -> StreamingSttSession,
    private val audioCapture: AudioCapture,
    private val intentRouter: IntentRouter,
    private val executeIntent: suspend (AssistantIntent) -> ActionResult,
    private val companionTurn: CompanionTurnExecutor? = null,
    private val onLevel: (Float) -> Unit = {},
    /** The Companion took the heard text (null) and then each answer so far, so the overlay never sits idle. */
    private val onCompanionProgress: (String?) -> Unit = {},
) {
    suspend fun run(): AssistantTurnOutcome {
        val utterance = try {
            UtteranceTranscriber(
                sessionFactory = sessionFactory,
                audioCapture = audioCapture,
                onLevel = onLevel,
            ).transcribe()
        } catch (e: CancellationException) {
            throw e
        } catch (e: SecurityException) {
            return AssistantTurnOutcome.MicPermissionDenied
        }
        return when (utterance.reason) {
            UtteranceResult.Reason.HEARD -> heard(utterance.text)
            UtteranceResult.Reason.EMPTY_FINAL -> AssistantTurnOutcome.EmptyFinal
            UtteranceResult.Reason.NO_SPEECH -> AssistantTurnOutcome.NoSpeech
            UtteranceResult.Reason.MIC_PERMISSION -> AssistantTurnOutcome.MicPermissionDenied
            UtteranceResult.Reason.MIC_UNAVAILABLE -> AssistantTurnOutcome.MicUnavailable
            UtteranceResult.Reason.TIMEOUT -> AssistantTurnOutcome.Timeout
            UtteranceResult.Reason.CLOSED -> AssistantTurnOutcome.Closed(utterance.detail)
            UtteranceResult.Reason.STREAM_FAILED ->
                AssistantTurnOutcome.StreamFailed(utterance.detail)
        }
    }

    private suspend fun heard(text: String): AssistantTurnOutcome {
        val intent = intentRouter.classify(text)
        val companion = companionTurn
        // With the hosted Cloud connection selected the Companion answers a general query (the app's gate keeps a transcript
        // on the device otherwise); a turn it does not take falls back to local Assist.
        val assist = if (
            intent.type == IntentType.GENERAL_QUERY &&
            companion != null &&
            companion.available()
        ) {
            onCompanionProgress(null)
            companion.startTurn(text) { partial -> onCompanionProgress(partial) } ?: executeIntent(intent)
        } else {
            executeIntent(intent)
        }
        return AssistantTurnOutcome.Heard(text, assist)
    }
}
