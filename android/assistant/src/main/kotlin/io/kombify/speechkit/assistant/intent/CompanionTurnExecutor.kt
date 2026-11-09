package io.kombify.speechkit.assistant.intent

/**
 * One Companion-backed AI turn from the system assistant or IME.
 *
 * The callee lives in kombify Companion over `speechkit.coinstall.v1`; the
 * Companion runs the turn with its own signed-in account. [available] is the
 * route gate (the app decides it: kombify Cloud selected and a Companion
 * session present, so a transcript never leaves the device when the person
 * chose on-device or self-hosted dictation). [startTurn] streams the answer
 * so far to [onPartial] and returns null when the Companion did not take the
 * turn (signed out, unreachable, past its ceiling), so the caller answers
 * with local Assist instead.
 */
interface CompanionTurnExecutor {
    fun available(): Boolean
    suspend fun startTurn(text: String, onPartial: (String) -> Unit = {}): ActionResult?
}
