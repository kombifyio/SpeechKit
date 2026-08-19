package io.kombify.speechkit.ime.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.MicOff
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.TextButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.ime.R
import io.kombify.speechkit.ime.VoicePanelController
import io.kombify.speechkit.ime.VoicePanelState

/**
 * The voice-only IME input view (B-M2 Stage 1): one big mic toggle, a live
 * draft strip, a language chip, an error chip with one-tap retry, a truthful
 * capture indicator while the mic is open (privacy checklist), and a
 * switch-back key to the user's regular keyboard.
 *
 * Stateless over [state]; every intent is a callback so the same UI can be
 * embedded by other hosts later.
 */
@Composable
fun VoicePanelUi(
    state: VoicePanelState,
    language: String,
    onMicToggle: () -> Unit,
    onRetry: () -> Unit,
    onLanguageToggle: () -> Unit,
    onSwitchKeyboard: () -> Unit,
    modifier: Modifier = Modifier,
    /** Enters Voice Agent mode. Null hides the entry point entirely. */
    onStartAgent: (() -> Unit)? = null,
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
            HeaderRow(
                state = state,
                language = language,
                onLanguageToggle = onLanguageToggle,
                onSwitchKeyboard = onSwitchKeyboard,
            )
            MicButton(state = state, onMicToggle = onMicToggle)
            StatusArea(state = state, onRetry = onRetry)
            // Dictation replaces the keyboard; the agent is a conversation the
            // editor sits out. Offering it here keeps both one tap away
            // without letting either write where the other should.
            if (onStartAgent != null && state !is VoicePanelState.Blocked) {
                TextButton(onClick = onStartAgent) {
                    Text(stringResource(R.string.speechkit_ime_action_voice_agent))
                }
            }
        }
    }
}

@Composable
private fun HeaderRow(
    state: VoicePanelState,
    language: String,
    onLanguageToggle: () -> Unit,
    onSwitchKeyboard: () -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        AssistChip(
            onClick = onLanguageToggle,
            label = { Text(language.uppercase()) },
        )
        Spacer(Modifier.weight(1f))
        if (state is VoicePanelState.Listening) {
            RecordingIndicator()
            Spacer(Modifier.weight(1f))
        }
        IconButton(onClick = onSwitchKeyboard) {
            Icon(
                imageVector = Icons.Filled.Keyboard,
                contentDescription = stringResource(R.string.speechkit_ime_switch_keyboard),
            )
        }
    }
}

/** Red dot + label: visible whenever audio is actually being captured. */
@Composable
private fun RecordingIndicator() {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Box(
            modifier = Modifier
                .size(10.dp)
                .background(Color(0xFFD32F2F), CircleShape),
        )
        Text(
            text = stringResource(R.string.speechkit_ime_recording_indicator),
            style = MaterialTheme.typography.labelMedium,
            color = Color(0xFFD32F2F),
        )
    }
}

@Composable
private fun MicButton(
    state: VoicePanelState,
    onMicToggle: () -> Unit,
) {
    val listening = state is VoicePanelState.Listening
    val busy = state is VoicePanelState.Connecting || state is VoicePanelState.Finishing
    val blocked = state is VoicePanelState.Blocked

    FilledIconButton(
        onClick = onMicToggle,
        enabled = !blocked && !busy,
        modifier = Modifier.size(72.dp),
        colors = IconButtonDefaults.filledIconButtonColors(
            containerColor = if (listening) {
                MaterialTheme.colorScheme.errorContainer
            } else {
                MaterialTheme.colorScheme.primaryContainer
            },
        ),
    ) {
        when {
            busy -> CircularProgressIndicator(modifier = Modifier.size(28.dp))
            blocked -> Icon(
                imageVector = Icons.Filled.MicOff,
                contentDescription = stringResource(R.string.speechkit_ime_status_blocked),
                modifier = Modifier.size(32.dp),
            )

            listening -> Icon(
                imageVector = Icons.Filled.Stop,
                contentDescription = stringResource(R.string.speechkit_ime_mic_stop),
                modifier = Modifier.size(32.dp),
            )

            else -> Icon(
                imageVector = Icons.Filled.Mic,
                contentDescription = stringResource(R.string.speechkit_ime_mic_start),
                modifier = Modifier.size(32.dp),
            )
        }
    }
}

@Composable
private fun StatusArea(
    state: VoicePanelState,
    onRetry: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .height(56.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        when (state) {
            is VoicePanelState.Listening -> {
                if (state.draft.isNotEmpty()) {
                    Text(
                        text = state.draft,
                        style = MaterialTheme.typography.bodyLarge,
                        fontStyle = FontStyle.Italic,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        textAlign = TextAlign.Center,
                    )
                } else {
                    StatusLabel(stringResource(R.string.speechkit_ime_status_listening))
                }
            }

            is VoicePanelState.Error -> {
                AssistChip(
                    onClick = onRetry,
                    enabled = state.retryable,
                    label = {
                        Text(
                            text = "${errorLabel(state.code)} — " +
                                stringResource(R.string.speechkit_ime_action_retry),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    },
                )
            }

            is VoicePanelState.Idle -> StatusLabel(stringResource(R.string.speechkit_ime_status_idle))
            is VoicePanelState.Blocked -> StatusLabel(stringResource(R.string.speechkit_ime_status_blocked))
            is VoicePanelState.NeedsMicPermission ->
                StatusLabel(stringResource(R.string.speechkit_ime_status_needs_permission))

            is VoicePanelState.Connecting ->
                StatusLabel(stringResource(R.string.speechkit_ime_status_connecting))

            is VoicePanelState.Finishing ->
                StatusLabel(stringResource(R.string.speechkit_ime_status_finishing))

            is VoicePanelState.Committed ->
                StatusLabel(stringResource(R.string.speechkit_ime_status_committed))
        }
    }
}

@Composable
private fun StatusLabel(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = TextAlign.Center,
    )
}

/** Stable error codes map onto localized labels; unknown codes stay generic. */
@Composable
private fun errorLabel(code: String): String = when (code) {
    VoicePanelController.ERROR_NOT_CONFIGURED ->
        stringResource(R.string.speechkit_ime_error_not_configured)

    VoicePanelController.ERROR_MIC_DENIED ->
        stringResource(R.string.speechkit_ime_error_mic_denied)

    else -> stringResource(R.string.speechkit_ime_error_generic)
}
