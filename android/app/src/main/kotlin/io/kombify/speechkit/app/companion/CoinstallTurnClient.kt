package io.kombify.speechkit.app.companion

import android.os.SystemClock
import io.kombify.speechkit.assistant.intent.ActionResult
import io.kombify.speechkit.assistant.intent.CompanionTurnExecutor
import io.kombify.speechkit.coinstall.v1.CoinstallContract
import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.util.UUID
import java.util.concurrent.atomic.AtomicLong
import kotlin.coroutines.resume

/**
 * SpeechKit caller for `ICoinstallService.startTurn` / `cancelTurn`.
 *
 * The callee is the kombify Companion (`companion/` CoinstallService): it runs the text through its own
 * signed-in account and streams the answer back. No Companion session is [CoinstallTurnResult.NoSession]
 * (also when the Companion answers signed out), a missing or unreachable Companion
 * [CoinstallTurnResult.Unavailable], and a turn without a terminal callback within [TURN_CEILING_MS]
 * [CoinstallTurnResult.Ceiling].
 */
sealed interface CoinstallTurnResult {
    data class Complete(val text: String) : CoinstallTurnResult
    data class Failed(val code: Int, val message: String) : CoinstallTurnResult
    data object NoSession : CoinstallTurnResult
    data object Unavailable : CoinstallTurnResult
    data object Ceiling : CoinstallTurnResult
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
    /** A Companion-provisioned kombify Cloud session exists (same gate as before the default route). */
    private val sessionPresent: () -> Boolean,
    private val transport: CoinstallTurnTransport?,
) : CompanionTurnExecutor {

    override fun available(): Boolean = sessionPresent()

    /**
     * Runs off the main thread (package resolution, bind); [onPartial] receives the answer so far on a
     * binder thread.
     */
    suspend fun startTurnResult(text: String, onPartial: (String) -> Unit = {}): CoinstallTurnResult =
        withContext(Dispatchers.IO) {
            if (!sessionPresent()) return@withContext CoinstallTurnResult.NoSession
            val live = transport ?: return@withContext CoinstallTurnResult.Unavailable
            val turnId = UUID.randomUUID().toString()
            val started = SystemClock.elapsedRealtime()
            val firstPartial = AtomicLong(-1L)
            val outcome = withTimeoutOrNull(TURN_CEILING_MS) {
                suspendCancellableCoroutine { cont ->
                    val callback = object : CoinstallTurnCallback {
                        override fun onPartial(turnId: String, text: String) {
                            firstPartial.compareAndSet(-1L, SystemClock.elapsedRealtime() - started)
                            if (cont.isActive) runCatching { onPartial(text) }
                        }

                        override fun onComplete(turnId: String, text: String) {
                            if (cont.isActive) cont.resume(CoinstallTurnResult.Complete(text))
                        }

                        override fun onError(turnId: String, code: Int, message: String) {
                            if (!cont.isActive) return
                            cont.resume(
                                when (code) {
                                    CoinstallContract.ERROR_SIGNED_OUT -> CoinstallTurnResult.NoSession
                                    COINSTALL_TURN_UNAVAILABLE -> CoinstallTurnResult.Unavailable
                                    else -> CoinstallTurnResult.Failed(code, message)
                                },
                            )
                        }
                    }
                    cont.invokeOnCancellation { live.cancelTurn(turnId) }
                    live.startTurn(turnId, text, callback)
                }
            }
            val totalMs = SystemClock.elapsedRealtime() - started
            val firstPartialMs = firstPartial.get()
            val result = outcome ?: CoinstallTurnResult.Ceiling
            // The measured turn against the chat budget (AI-HARNESS-CONCEPT-STANDARD §8.8); no text is logged.
            VoiceLog.i(
                VoiceLog.ASSIST,
                "companion turn outcome=${result.label()} first_ms=$firstPartialMs total_ms=$totalMs " +
                    "within_budget=${result is CoinstallTurnResult.Complete && withinBudget(firstPartialMs, totalMs)}",
            )
            result
        }

    /** The Companion's answer, or null when it did not take the turn so local Assist answers instead. */
    override suspend fun startTurn(text: String, onPartial: (String) -> Unit): ActionResult? =
        when (val outcome = startTurnResult(text, onPartial)) {
            is CoinstallTurnResult.Complete -> ActionResult(success = true, responseText = outcome.text)
            is CoinstallTurnResult.Failed,
            CoinstallTurnResult.NoSession,
            CoinstallTurnResult.Unavailable,
            CoinstallTurnResult.Ceiling,
            -> null
        }

    private fun CoinstallTurnResult.label(): String = when (this) {
        is CoinstallTurnResult.Complete -> "complete"
        is CoinstallTurnResult.Failed -> "failed_$code"
        CoinstallTurnResult.NoSession -> "signed_out"
        CoinstallTurnResult.Unavailable -> "unavailable"
        CoinstallTurnResult.Ceiling -> "ceiling"
    }

    companion object {
        /** First answer text and whole turn, from the chat latency budget (p50 targets, §8.8). */
        const val FIRST_TEXT_BUDGET_MS: Long = 2_500
        const val TURN_BUDGET_MS: Long = 5_000

        /**
         * The hard stop after which the assistant answers locally. Not the budget: the hosted turn does not
         * meet the budget yet, and cutting it there would make the Companion route never answer.
         */
        const val TURN_CEILING_MS: Long = 30_000

        fun withinBudget(firstPartialMs: Long, totalMs: Long): Boolean =
            totalMs <= TURN_BUDGET_MS && (firstPartialMs in 0..FIRST_TEXT_BUDGET_MS || totalMs <= FIRST_TEXT_BUDGET_MS)
    }
}
