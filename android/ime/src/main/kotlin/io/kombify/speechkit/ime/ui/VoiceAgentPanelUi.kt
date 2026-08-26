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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.ime.ImeVoiceAgentController
import io.kombify.speechkit.ime.R
import io.kombify.speechkit.net.VoiceAgentErrorCodes
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.voiceui.VoiceAuraOrb

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
 *
 * @param provider the realtime backend this conversation was opened on, shown
 *   next to the phase because the surfaces that offer a choice of backend
 *   would otherwise leave the user guessing which one is answering.
 */
@Composable
fun VoiceAgentPanelUi(
    state: VoiceAgentUiState,
    holding: Boolean,
    onHoldChange: (Boolean) -> Unit,
    onEnd: () -> Unit,
    onSwitchToDictation: () -> Unit,
    modifier: Modifier = Modifier,
    provider: String? = null,
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
                VoiceAuraOrb(state = state.phase.toAuraState(), sizeDp = 40)
                Text(
                    text = agentStatusLabel(state, provider),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                TextButton(onClick = onSwitchToDictation) {
                    Text(stringResource(R.string.speechkit_ime_agent_action_dictation))
                }
            }

            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 84.dp, max = 168.dp)
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                if (state.userText.isNotBlank()) {
                    Text(
                        text = stringResource(R.string.speechkit_ime_agent_speaker_user),
                        style = MaterialTheme.typography.labelSmall,
                    )
                    Text(state.userText, style = MaterialTheme.typography.bodyMedium)
                }
                if (state.agentText.isNotBlank()) {
                    Text(
                        text = stringResource(R.string.speechkit_ime_agent_speaker_agent),
                        style = MaterialTheme.typography.labelSmall,
                    )
                    Text(state.agentText, style = MaterialTheme.typography.bodyMedium)
                }
                if (state.errorCode != ImeVoiceAgentController.ERROR_NO_SERVER &&
                    (state.error != null || state.errorCode != null)
                ) {
                    Text(
                        text = agentErrorLabel(state.errorCode, state.error),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }

            Button(
                onClick = { onHoldChange(!holding) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(
                    stringResource(
                        if (holding) {
                            R.string.speechkit_ime_agent_release
                        } else {
                            R.string.speechkit_ime_agent_hold_to_talk
                        },
                    ),
                )
            }

            TextButton(onClick = onEnd) {
                Text(stringResource(R.string.speechkit_ime_agent_action_end))
            }
        }
    }
}

/**
 * The conversation phase is mapped onto the orb in [toAuraState]. The session
 * vocabulary and the visual vocabulary stay separate on purpose: a visual
 * change must not force a protocol change.
 */

@Composable
private fun agentStatusLabel(state: VoiceAgentUiState, provider: String?): String {
    if (state.errorCode == ImeVoiceAgentController.ERROR_NO_SERVER) {
        return stringResource(R.string.speechkit_ime_agent_error_no_server)
    }
    val phase = stringResource(state.phase.label())
    return if (provider.isNullOrBlank()) phase else "$phase · $provider"
}

private fun VoiceAgentUiState.Phase.label(): Int = when (this) {
    VoiceAgentUiState.Phase.Inactive -> R.string.speechkit_ime_agent_phase_inactive
    VoiceAgentUiState.Phase.Connecting -> R.string.speechkit_ime_agent_phase_connecting
    VoiceAgentUiState.Phase.Listening -> R.string.speechkit_ime_agent_phase_listening
    VoiceAgentUiState.Phase.Processing -> R.string.speechkit_ime_agent_phase_processing
    VoiceAgentUiState.Phase.Speaking -> R.string.speechkit_ime_agent_phase_speaking
    VoiceAgentUiState.Phase.Ended -> R.string.speechkit_ime_agent_phase_ended
}

/**
 * Stable failure codes become localized explanations; anything else falls back
 * to the server's own prose, which is still better than nothing. A refused
 * provider is called out by name because no endpoint tells a client which
 * backends a deployment registered — being turned down is how you find out.
 */
@Composable
private fun agentErrorLabel(code: String?, message: String?): String = when (code) {
    VoiceAgentErrorCodes.PROVIDER_UNAVAILABLE ->
        stringResource(R.string.speechkit_ime_agent_error_provider_unavailable)

    ImeVoiceAgentController.ERROR_NO_SERVER ->
        stringResource(R.string.speechkit_ime_agent_error_no_server)

    ImeVoiceAgentController.ERROR_MIC_DENIED ->
        stringResource(R.string.speechkit_ime_error_mic_denied)

    else -> message.orEmpty()
}
