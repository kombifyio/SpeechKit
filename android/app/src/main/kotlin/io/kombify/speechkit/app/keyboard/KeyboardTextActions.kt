package io.kombify.speechkit.app.keyboard

import android.content.Intent
import android.inputmethodservice.InputMethodService
import android.text.InputType
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.ExtractedTextRequest
import android.widget.Toast
import io.kombify.speechkit.R
import io.kombify.speechkit.domain.ExecutionPolicy
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.net.StoredConnectionSelection
import java.util.UUID

enum class KeyboardTextAction { SUMMARIZE, REWRITE, CORRECT }

/** Ephemeral editor state never leaves this process or an intent extra. */
object KeyboardTextActions {
    internal data class EditorSnapshot(
        val packageName: String,
        val fieldId: Int,
        val text: String,
        val selectionStart: Int,
        val selectionEnd: Int,
    )
    internal data class Request internal constructor(
        val action: KeyboardTextAction,
        val policy: ExecutionPolicy,
        val profile: ConnectionProfile,
        val revision: Long,
        val input: String,
        internal val editor: EditorSnapshot,
    )
    private data class Pending(val nonce: String, val request: Request, val result: String? = null, val createdAt: Long = android.os.SystemClock.elapsedRealtime())
    private var pending: Pending? = null

    fun launch(service: InputMethodService, action: KeyboardTextAction, policy: ExecutionPolicy, expectedRevision: Long) {
        val info = service.currentInputEditorInfo
        if (info == null) {
            Toast.makeText(service, R.string.keyboard_text_missing, Toast.LENGTH_LONG).show()
            return
        }
        if (isPassword(info)) {
            Toast.makeText(service, R.string.keyboard_text_password, Toast.LENGTH_LONG).show()
            return
        }
        val snapshot = snapshot(service)
        if (snapshot == null) {
            Toast.makeText(service, R.string.keyboard_text_missing, Toast.LENGTH_LONG).show()
            return
        }
        val start = minOf(snapshot.selectionStart, snapshot.selectionEnd)
        val end = maxOf(snapshot.selectionStart, snapshot.selectionEnd)
        val input = if (start != end) snapshot.text.substring(start, end) else snapshot.text
        if (input.isBlank() || input.length > MAX_TEXT || snapshot.text.length > MAX_TEXT) {
            Toast.makeText(service, R.string.keyboard_text_empty, Toast.LENGTH_LONG).show()
            return
        }
        val nonce = UUID.randomUUID().toString()
        val route = StoredConnectionSelection(service).snapshot(ShippedDefaults.shippedProfile())
        if (route.revision != expectedRevision) {
            Toast.makeText(service, R.string.keyboard_text_changed, Toast.LENGTH_LONG).show()
            return
        }
        synchronized(this) { pending = Pending(nonce, Request(action, policy, route.profile, route.revision, input, snapshot)) }
        try {
            service.startActivity(Intent(service, KeyboardTextActivity::class.java)
                .putExtra(EXTRA_NONCE, nonce).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        } catch (_: Exception) {
            discard(nonce)
            Toast.makeText(service, R.string.keyboard_text_failed, Toast.LENGTH_LONG).show()
        }
    }

    internal fun request(nonce: String): Request? = synchronized(this) {
        pending?.takeIf { it.nonce == nonce && it.result == null && android.os.SystemClock.elapsedRealtime() - it.createdAt < MAX_AGE_MS }?.request
    }

    internal fun submit(nonce: String, result: String): Boolean = synchronized(this) {
        val current = pending?.takeIf { it.nonce == nonce && it.result == null } ?: return@synchronized false
        if (result.isBlank() || result.length > 24_000 || android.os.SystemClock.elapsedRealtime() - current.createdAt >= MAX_AGE_MS) return@synchronized false
        pending = current.copy(result = result)
        true
    }

    internal fun discard(nonce: String) = synchronized(this) {
        if (pending?.nonce == nonce) pending = null
    }

    /** Called when the IME returns to an editor; consumes an approved preview exactly once. */
    fun applyPending(service: InputMethodService) {
        val approved = synchronized(this) {
            val current = pending?.takeIf { it.result != null } ?: return
            pending = null
            current
        }
        val original = approved.request.editor
        val text = approved.result ?: return
        val current = snapshot(service)
        val connection = service.currentInputConnection
        if (current != original || connection == null || StoredConnectionSelection(service).revision() != approved.request.revision ||
            android.os.SystemClock.elapsedRealtime() - approved.createdAt >= MAX_AGE_MS) {
            Toast.makeText(service, R.string.keyboard_text_changed, Toast.LENGTH_LONG).show()
            return
        }
        val start = minOf(original.selectionStart, original.selectionEnd)
        val end = maxOf(original.selectionStart, original.selectionEnd)
        val replaceStart = if (start == end) 0 else start
        val replaceEnd = if (start == end) original.text.length else end
        val applied = try {
            connection.beginBatchEdit()
            connection.setSelection(replaceStart, replaceEnd) && connection.commitText(text, 1)
        } catch (_: Exception) { false }
        finally { runCatching { connection.endBatchEdit() } }
        if (!applied) Toast.makeText(service, R.string.keyboard_text_changed, Toast.LENGTH_LONG).show()
    }

    private fun snapshot(service: InputMethodService): EditorSnapshot? = try {
        readSnapshot(service)
    } catch (_: Exception) { null }

    private fun readSnapshot(service: InputMethodService): EditorSnapshot? {
        val info = service.currentInputEditorInfo ?: return null
        if (isPassword(info) || info.inputType and InputType.TYPE_MASK_CLASS != InputType.TYPE_CLASS_TEXT ||
            info.packageName.isNullOrBlank() || info.fieldId == 0 || info.fieldId == android.view.View.NO_ID) return null
        val extracted = service.currentInputConnection?.getExtractedText(ExtractedTextRequest(), 0) ?: return null
        if (extracted.startOffset != 0 || extracted.partialStartOffset >= 0) return null
        val text = extracted.text?.toString() ?: return null
        val start = extracted.selectionStart
        val end = extracted.selectionEnd
        if (text.length > MAX_TEXT || start !in 0..text.length || end !in 0..text.length) return null
        return EditorSnapshot(info.packageName, info.fieldId, text, start, end)
    }

    private fun isPassword(info: EditorInfo): Boolean {
        val input = info.inputType
        val variation = input and InputType.TYPE_MASK_VARIATION
        return when (input and InputType.TYPE_MASK_CLASS) {
            InputType.TYPE_CLASS_TEXT -> variation == InputType.TYPE_TEXT_VARIATION_PASSWORD ||
                variation == InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD ||
                variation == InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD
            InputType.TYPE_CLASS_NUMBER -> variation == InputType.TYPE_NUMBER_VARIATION_PASSWORD
            else -> false
        }
    }

    internal const val EXTRA_NONCE = "keyboard_text_request"
    private const val MAX_TEXT = 12_000
    private const val MAX_AGE_MS = 5 * 60 * 1000L
}
