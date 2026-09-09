package io.kombify.speechkit.assistant.intent

/**
 * One Companion-backed AI turn from the system assistant or IME.
 *
 * The callee lives in kombify Companion over `speechkit.coinstall.v1`.
 * [hasSession] is the same gate as the IME Companion chip: without a live
 * session the assistant uses local Assist instead of startTurn.
 */
interface CompanionTurnExecutor {
    fun hasSession(): Boolean
    suspend fun startTurn(text: String): ActionResult
}
