package io.kombify.speechkit.app.keyboard

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.R

/**
 * Gboard-style more page: two columns of labelled tools over the keys.
 *
 * Voice-message transcription and summary are the extras. Further extras
 * (translate) land here rather than on the suggestion strip.
 */
@Composable
fun ShortcutsPage(
    onTranscribe: (Boolean) -> Unit,
    onVoiceAgent: (Boolean) -> Unit,
    onTextAction: (KeyboardTextAction, Boolean) -> Unit,
    onAnalyzeScreen: (Boolean) -> Unit,
    alternativeSymbol: String,
    onVoiceHoldStart: (() -> Unit)? = null,
    onVoiceHoldEnd: ((Boolean) -> Unit)? = null,
    onDictationHoldStart: (() -> Unit)? = null,
    onDictationHoldEnd: ((Boolean) -> Unit)? = null,
    onTranscribeAudio: (Boolean) -> Unit,
    onSummarizeAudio: (Boolean) -> Unit,
    onKeyboardSettings: () -> Unit,
    onSpeechKitSettings: () -> Unit,
    onClose: () -> Unit,
    onHybrid: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        tonalElevation = 2.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = stringResource(R.string.shortcuts_title),
                    style = MaterialTheme.typography.titleSmall,
                )
                TextButton(onClick = onClose) {
                    Text(stringResource(R.string.shortcuts_close))
                }
            }
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (io.kombify.speechkit.BuildConfig.CONNECTION_SETUP_ENABLED && io.kombify.speechkit.BuildConfig.HYBRID_ENABLED) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    ExecutionShortcut(stringResource(R.string.shortcut_transcribe), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol, Modifier.weight(1f), { onTranscribe(false) }, { onTranscribe(true) }, onDictationHoldStart, onDictationHoldEnd)
                    ExecutionShortcut(stringResource(R.string.shortcut_voice_agent), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol, Modifier.weight(1f), { onVoiceAgent(false) }, { onVoiceAgent(true) }, onVoiceHoldStart, onVoiceHoldEnd)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    ExecutionShortcut(stringResource(R.string.speechkit_ime_shortcut_summarize), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol, Modifier.weight(1f), { onTextAction(KeyboardTextAction.SUMMARIZE, false) }, { onTextAction(KeyboardTextAction.SUMMARIZE, true) })
                    ExecutionShortcut(stringResource(R.string.speechkit_ime_shortcut_rewrite), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol, Modifier.weight(1f), { onTextAction(KeyboardTextAction.REWRITE, false) }, { onTextAction(KeyboardTextAction.REWRITE, true) })
                    ExecutionShortcut(stringResource(R.string.speechkit_ime_shortcut_correct), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol, Modifier.weight(1f), { onTextAction(KeyboardTextAction.CORRECT, false) }, { onTextAction(KeyboardTextAction.CORRECT, true) })
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    ExecutionShortcut(stringResource(R.string.shortcut_transcribe_audio), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol, Modifier.weight(1f), { onTranscribeAudio(false) }, { onTranscribeAudio(true) })
                    ExecutionShortcut(stringResource(R.string.shortcut_summarize_audio), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol, Modifier.weight(1f), { onSummarizeAudio(false) }, { onSummarizeAudio(true) })
                }
                } else {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                        ShortcutChip(stringResource(R.string.shortcut_transcribe), Modifier.weight(1f)) { onTranscribe(false) }
                        ShortcutChip(stringResource(R.string.shortcut_voice_agent), Modifier.weight(1f)) { onVoiceAgent(false) }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                        ShortcutChip(stringResource(R.string.shortcut_transcribe_audio), Modifier.weight(1f)) { onTranscribeAudio(false) }
                        ShortcutChip(stringResource(R.string.shortcut_summarize_audio), Modifier.weight(1f)) { onSummarizeAudio(false) }
                    }
                }
                if (io.kombify.speechkit.BuildConfig.HYBRID_ENABLED) {
                    if (io.kombify.speechkit.BuildConfig.CONTEXT_ACTIONS_ENABLED) {
                        ExecutionShortcut(stringResource(R.string.context_screen_action), stringResource(R.string.speechkit_ime_shortcut_alternative), alternativeSymbol,
                            Modifier.fillMaxWidth(), { onAnalyzeScreen(false) }, { onAnalyzeScreen(true) })
                    }
                    ShortcutChip(stringResource(R.string.hybrid_title), Modifier.fillMaxWidth(), onHybrid)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    ShortcutChip(stringResource(R.string.shortcut_keyboard_settings), Modifier.weight(1f), onKeyboardSettings)
                    ShortcutChip(stringResource(R.string.shortcut_speechkit_settings), Modifier.weight(1f), onSpeechKitSettings)
                }
            }
        }
    }
}

@Composable
private fun ShortcutChip(label: String, modifier: Modifier, onClick: () -> Unit) {
    Surface(
        modifier = modifier.clickable(onClick = onClick),
        shape = MaterialTheme.shapes.large,
        color = MaterialTheme.colorScheme.surfaceVariant,
        tonalElevation = 0.dp,
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelLarge,
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 14.dp),
        )
    }
}
