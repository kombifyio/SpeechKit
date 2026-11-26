package io.kombify.speechkit.app.hybrid

import android.app.Activity
import android.os.Bundle
import android.widget.EditText

/** A real editor for the explicitly requested installed-keyboard journey. */
class KeyboardJourneyActivity : Activity() {
    lateinit var editor: EditText
        private set

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        editor = EditText(this).apply {
            setSingleLine(false)
            setText("Synthetic keyboard journey note")
            setSelection(text.length)
            requestFocus()
        }
        setContentView(editor)
    }
}
