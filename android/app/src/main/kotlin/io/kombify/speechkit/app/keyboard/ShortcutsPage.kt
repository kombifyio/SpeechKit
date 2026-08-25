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
    onTranscribe: () -> Unit,
    onVoiceAgent: () -> Unit,
    onTranscribeAudio: () -> Unit,
    onSummarizeAudio: () -> Unit,
    onKeyboardSettings: () -> Unit,
    onSpeechKitSettings: () -> Unit,
    onClose: () -> Unit,
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
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    ShortcutChip(stringResource(R.string.shortcut_transcribe), Modifier.weight(1f), onTranscribe)
                    ShortcutChip(stringResource(R.string.shortcut_voice_agent), Modifier.weight(1f), onVoiceAgent)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    ShortcutChip(stringResource(R.string.shortcut_transcribe_audio), Modifier.weight(1f), onTranscribeAudio)
                    ShortcutChip(stringResource(R.string.shortcut_summarize_audio), Modifier.weight(1f), onSummarizeAudio)
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
