package io.kombify.speechkit.app.companion

import io.kombify.speechkit.assistant.intent.ActionResult
import io.kombify.speechkit.assistant.intent.CompanionTurnExecutor
import kotlinx.coroutines.suspendCancellableCoroutine
import java.util.UUID
import kotlin.coroutines.resume

/**
 * SpeechKit caller for `ICoinstallService.startTurn` / `cancelTurn`.
 *
 * The Companion callee lives in kombify-Mobile. Missing or signed-out
 * Companion is a typed failure; a turn ends on the first terminal callback.
 */
sealed interface CoinstallTurnResult {
    data class Complete(val text: String) : CoinstallTurnResult
    data class Failed(val code: Int, val message: String) : CoinstallTurnResult
    data object NoSession : CoinstallTurnResult
    data object Unavailable : CoinstallTurnResult
}

interface CoinstallTurnTransport {
    fun startTurn(turnId: String, text: String, callback: CoinstallTurnCallback)
    fun cancelTurn(turnId: String)
}

interface CoinstallTurnCallback {
    fun onPartial(turnId: String, text: String)
    fun onComplete(turnId: String, text: String)
    fun onError(turnId: String, code: Int, message: String)
}

class CoinstallTurnClient(
    private val sessionPresent: () -> Boolean,
    private val transport: CoinstallTurnTransport?,
) : CompanionTurnExecutor {

    override fun hasSession(): Boolean = sessionPresent()

    suspend fun startTurnResult(text: String): CoinstallTurnResult {
        if (!sessionPresent()) return CoinstallTurnResult.NoSession
        val live = transport ?: return CoinstallTurnResult.Unavailable
        val turnId = UUID.randomUUID().toString()
        return suspendCancellableCoroutine { cont ->
            val callback = object : CoinstallTurnCallback {
                override fun onPartial(turnId: String, text: String) = Unit

                override fun onComplete(turnId: String, text: String) {
                    if (cont.isActive) cont.resume(CoinstallTurnResult.Complete(text))
                }

                override fun onError(turnId: String, code: Int, message: String) {
                    if (cont.isActive) cont.resume(CoinstallTurnResult.Failed(code, message))
                }
            }
            cont.invokeOnCancellation { live.cancelTurn(turnId) }
            live.startTurn(turnId, text, callback)
        }
    }

    override suspend fun startTurn(text: String): ActionResult =
        when (val outcome = startTurnResult(text)) {
            is CoinstallTurnResult.Complete ->
                ActionResult(success = true, responseText = outcome.text)
            is CoinstallTurnResult.Failed ->
                ActionResult(success = false, errorMessage = outcome.message)
            CoinstallTurnResult.NoSession ->
                ActionResult(success = false, errorMessage = "Companion is signed out")
            CoinstallTurnResult.Unavailable ->
                ActionResult(success = false, errorMessage = "Companion is not available")
        }
}
