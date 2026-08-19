package io.kombify.speechkit.ime.host

import android.text.InputType
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection

/**
 * The keyboard-integration seam from `android/CONTRACT.md`: everything a host
 * keyboard needs to embed SpeechKit voice input, and nothing else.
 *
 * Two hosts exist by design:
 * - `SpeechKitVoiceImeService` (B-M2) — the standalone voice-only IME.
 * - The HeliBoard fork's inline voice panel (B-M3) — the fork patches a
 *   `VoiceInputHost` call site into its keyboard view and stays inside the
 *   agreed <=~6-file patch surface precisely because this interface is small.
 *
 * Text lands in the target field through the [InputConnection] handed to
 * [showPanel]; the [Listener] mirrors those transitions for hosts that render
 * their own transcript UI (HeliBoard's suggestion strip).
 */
interface VoiceInputHost {

    /**
     * Binds the panel to the currently focused editor. Implementations decide
     * from [editorInfo] whether dictation is allowed at all (password and
     * TYPE_NULL fields are always refused).
     */
    fun showPanel(inputConnection: InputConnection, editorInfo: EditorInfo)

    /** Unbinds from the editor and stops any active capture. */
    fun hidePanel()

    /** Optional transcript mirror for hosts with their own voice UI. */
    var listener: Listener?

    interface Listener {
        /** Live draft; shown as composing text, never committed. */
        fun onDraft(text: String) {}

        /** Final transcript, already committed to the input connection. */
        fun onFinal(text: String) {}

        /** Recoverable error surfaced to the panel (stable [code] values). */
        fun onError(code: String, message: String) {}
    }
}

/**
 * True when dictation must not start for this editor: password variations
 * never see voice input (privacy checklist), and TYPE_NULL editors do not
 * support composing regions.
 */
fun isDictationBlocked(inputType: Int): Boolean {
    if (inputType == InputType.TYPE_NULL) return true
    val variation = inputType and InputType.TYPE_MASK_VARIATION
    return when (inputType and InputType.TYPE_MASK_CLASS) {
        InputType.TYPE_CLASS_TEXT ->
            variation == InputType.TYPE_TEXT_VARIATION_PASSWORD ||
                variation == InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD ||
                variation == InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD

        InputType.TYPE_CLASS_NUMBER ->
            variation == InputType.TYPE_NUMBER_VARIATION_PASSWORD

        else -> false
    }
}
