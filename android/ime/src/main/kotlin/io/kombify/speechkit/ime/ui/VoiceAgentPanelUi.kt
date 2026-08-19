package io.kombify.speechkit.ime.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.voiceui.VoiceAuraOrb
import io.kombify.speechkit.voiceui.VoiceAuraState

/**
 * Voice Agent surface inside the keyboard window: the conversation takes over
 * where the keys would be, because you are talking, not typing.
 *
 * Nothing here writes into the editor. Dictation is the keyboard replacement
 * and commits what you said; this is a conversation the editor sits out. That
 * separation is the reason the two modes are different surfaces rather than
 * one panel with a flag.
 *
 * It draws the same orb as every other SpeechKit surface, from the shared
 * `:voice-ui-compose` module. The orb used to live inside the assistant
 * application, which no keyboard should depend on, so this panel drew its
 * own indicator instead; extracting the orb removed that split.
 */
@Composable
fun VoiceAgentPanelUi(
    state: VoiceAgentUiState,
    holding: Boolean,
    onHoldChange: (Boolean) -> Unit,
    onEnd: () -> Unit,
    onSwitchToDictation: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        tonalElevation = 3.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 12.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                VoiceAuraOrb(state = state.phase.orbState(), sizeDp = 40)
                Text(
                    text = state.phase.label(),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                TextButton(onClick = onSwitchToDictation) { Text("Diktat") }
            }

            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 84.dp, max = 168.dp)
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                if (state.userText.isNotBlank()) {
                    Text("Du", style = MaterialTheme.typography.labelSmall)
                    Text(state.userText, style = MaterialTheme.typography.bodyMedium)
                }
                if (state.agentText.isNotBlank()) {
                    Text("Assistent", style = MaterialTheme.typography.labelSmall)
                    Text(state.agentText, style = MaterialTheme.typography.bodyMedium)
                }
                state.error?.let {
                    Text(it, style = MaterialTheme.typography.bodySmall)
                }
            }

            Button(
                onClick = { onHoldChange(!holding) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(if (holding) "Loslassen" else "Halten zum Sprechen")
            }

            TextButton(onClick = onEnd) { Text("Gespräch beenden") }
        }
    }
}

/**
 * The conversation phase drawn as an orb state. The session vocabulary and
 * the visual vocabulary stay separate on purpose: a visual change must not
 * force a protocol change.
 */
private fun VoiceAgentUiState.Phase.orbState(): VoiceAuraState = when (this) {
    VoiceAgentUiState.Phase.Inactive -> VoiceAuraState.INACTIVE
    VoiceAgentUiState.Phase.Connecting -> VoiceAuraState.CONNECTING
    VoiceAgentUiState.Phase.Listening -> VoiceAuraState.LISTENING
    VoiceAgentUiState.Phase.Processing -> VoiceAuraState.PROCESSING
    VoiceAgentUiState.Phase.Speaking -> VoiceAuraState.SPEAKING
    VoiceAgentUiState.Phase.Ended -> VoiceAuraState.SETTLING
}

private fun VoiceAgentUiState.Phase.label(): String = when (this) {
    VoiceAgentUiState.Phase.Inactive -> "Bereit"
    VoiceAgentUiState.Phase.Connecting -> "Verbinde…"
    VoiceAgentUiState.Phase.Listening -> "Hört zu"
    VoiceAgentUiState.Phase.Processing -> "Denkt nach…"
    VoiceAgentUiState.Phase.Speaking -> "Antwortet"
    VoiceAgentUiState.Phase.Ended -> "Beendet"
}
