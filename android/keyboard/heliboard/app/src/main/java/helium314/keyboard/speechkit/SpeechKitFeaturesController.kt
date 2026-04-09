package helium314.keyboard.speechkit

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.view.View
import android.widget.Toast
import helium314.keyboard.keyboard.KeyboardSwitcher
import helium314.keyboard.latin.LatinIME
import helium314.keyboard.latin.R

/**
 * Wires the SpeechKit feature panel buttons to real actions.
 * Called from KeyboardSwitcher when the feature panel is shown.
 */
class SpeechKitFeaturesController(private val ime: LatinIME) {

    fun attach(view: View) {
        // Dictate: start voice recording
        view.findViewById<View>(R.id.speechkit_feature_dictate)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures() // close panel
            ime.onSpeechKitDictate()
        }

        // Live: start live transcription (same as dictate for now)
        view.findViewById<View>(R.id.speechkit_feature_assist)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures()
            ime.onSpeechKitAssist()
        }

        // Rewrite: get current text and show rewrite options
        view.findViewById<View>(R.id.speechkit_feature_rewrite)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures()
            val text = getTextBeforeCursor(500)
            if (text.isNotBlank()) {
                Toast.makeText(ime, "Rewrite: LLM-Integration folgt", Toast.LENGTH_SHORT).show()
            } else {
                Toast.makeText(ime, "Kein Text zum Umschreiben", Toast.LENGTH_SHORT).show()
            }
        }

        // Summarize: summarize text before cursor
        view.findViewById<View>(R.id.speechkit_feature_summarize)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures()
            val text = getTextBeforeCursor(2000)
            if (text.isNotBlank()) {
                Toast.makeText(ime, "Summarize: LLM-Integration folgt", Toast.LENGTH_SHORT).show()
            } else {
                Toast.makeText(ime, "Kein Text zum Zusammenfassen", Toast.LENGTH_SHORT).show()
            }
        }

        // Tone: adjust text tone
        view.findViewById<View>(R.id.speechkit_feature_tone)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures()
            Toast.makeText(ime, "Tone: LLM-Integration folgt", Toast.LENGTH_SHORT).show()
        }

        // Translate: translate text
        view.findViewById<View>(R.id.speechkit_feature_translate)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures()
            Toast.makeText(ime, "Translate: LLM-Integration folgt", Toast.LENGTH_SHORT).show()
        }

        // Quick Note: save current text as quick note
        view.findViewById<View>(R.id.speechkit_feature_quicknote)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures()
            val text = getTextBeforeCursor(2000)
            if (text.isNotBlank()) {
                // Copy to clipboard as simple quick note
                val clipboard = ime.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                clipboard.setPrimaryClip(ClipData.newPlainText("SpeechKit Note", text))
                Toast.makeText(ime, "Notiz in Zwischenablage gespeichert", Toast.LENGTH_SHORT).show()
            } else {
                Toast.makeText(ime, "Kein Text fuer Notiz", Toast.LENGTH_SHORT).show()
            }
        }

        // Settings: open SpeechKit settings
        view.findViewById<View>(R.id.speechkit_feature_settings)?.setOnClickListener {
            KeyboardSwitcher.getInstance().toggleSpeechKitFeatures()
            // Open HeliBoard settings (where HF token can be configured)
            val settingsIntent = android.content.Intent(ime, helium314.keyboard.settings.SettingsActivity::class.java)
            settingsIntent.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
            ime.startActivity(settingsIntent)
        }
    }

    private fun getTextBeforeCursor(maxChars: Int): String {
        val ic = ime.currentInputConnection ?: return ""
        return ic.getTextBeforeCursor(maxChars, 0)?.toString() ?: ""
    }
}
