package io.kombify.speechkit.assistant.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import io.kombify.speechkit.voiceui.VoiceAuraOrb
import io.kombify.speechkit.voiceui.VoiceAuraState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.assistant.R
import io.kombify.speechkit.assistant.service.AssistantUiState

/**
 * Voice assistant overlay UI.
 *
 * Displayed as a bottom sheet / card when the assistant is active. Shows the
 * assistant orb ([VoiceAuraOrb]) for the idle, listening and processing
 * states, then the transcript, action result, or error state with retry.
 */
@Composable
fun AssistantOverlay(
    state: AssistantUiState,
    onRetry: () -> Unit,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Card(
        modifier = modifier
            .fillMaxWidth()
            .padding(16.dp),
        shape = RoundedCornerShape(24.dp),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surface,
        ),
        elevation = CardDefaults.cardElevation(defaultElevation = 8.dp),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            AnimatedContent(
                targetState = state,
                // Key on the state kind, not the value: Listening carries a
                // live microphone level, and keying on the value would cross-
                // fade the whole overlay on every level update.
                contentKey = { it::class },
                transitionSpec = { fadeIn(tween(300)) togetherWith fadeOut(tween(200)) },
                label = "assistant_state",
            ) { currentState ->
                when (currentState) {
                    is AssistantUiState.Idle -> IdleView()
                    is AssistantUiState.Listening -> ListeningView(currentState.level)
                    is AssistantUiState.Processing -> ProcessingView()
                    is AssistantUiState.Transcribed -> TranscribedView(currentState.text)
                    is AssistantUiState.Executing -> ExecutingView(currentState.actionName)
                    is AssistantUiState.Result -> ResultView(currentState.text, onDismiss)
                    is AssistantUiState.Error -> ErrorView(currentState.message, onRetry, onDismiss)
                }
            }
        }
    }
}

@Composable
private fun IdleView() {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        VoiceAuraOrb(state = VoiceAuraState.INACTIVE, markRes = R.drawable.kombify_ai_mark)
        Spacer(modifier = Modifier.height(16.dp))
        Text(
            text = stringResource(R.string.speechkit_assistant_idle_prompt),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

@Composable
private fun ListeningView(level: Float) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        VoiceAuraOrb(state = VoiceAuraState.LISTENING, level = level, markRes = R.drawable.kombify_ai_mark)
        Spacer(modifier = Modifier.height(16.dp))
        Text(
            text = stringResource(R.string.speechkit_assistant_state_listening),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.primary,
        )
    }
}

@Composable
private fun ProcessingView() {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        VoiceAuraOrb(state = VoiceAuraState.PROCESSING, markRes = R.drawable.kombify_ai_mark)
        Spacer(modifier = Modifier.height(16.dp))
        Text(
            text = stringResource(R.string.speechkit_assistant_state_processing),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun TranscribedView(text: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(
            text = "\"$text\"",
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
            fontWeight = FontWeight.Medium,
        )
        Spacer(modifier = Modifier.height(8.dp))
        VoiceAuraOrb(state = VoiceAuraState.PROCESSING, sizeDp = 48, markRes = R.drawable.kombify_ai_mark)
    }
}

@Composable
private fun ExecutingView(actionName: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        VoiceAuraOrb(state = VoiceAuraState.PROCESSING, sizeDp = 56, markRes = R.drawable.kombify_ai_mark)
        Spacer(modifier = Modifier.height(12.dp))
        Text(
            text = actionName,
            style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.primary,
        )
    }
}

@Composable
private fun ResultView(text: String, onDismiss: () -> Unit) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        VoiceAuraOrb(state = VoiceAuraState.SETTLING, sizeDp = 56, markRes = R.drawable.kombify_ai_mark)
        Spacer(modifier = Modifier.height(12.dp))
        Text(
            text = text,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
        )
        Spacer(modifier = Modifier.height(16.dp))
        TextButton(onClick = onDismiss) {
            Text(stringResource(R.string.speechkit_assistant_action_close))
        }
    }
}

@Composable
private fun ErrorView(message: String, onRetry: () -> Unit, onDismiss: () -> Unit) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        VoiceAuraOrb(state = VoiceAuraState.ERROR, sizeDp = 56, markRes = R.drawable.kombify_ai_mark)
        Spacer(modifier = Modifier.height(12.dp))
        Text(
            text = message,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.error,
            textAlign = TextAlign.Center,
        )
        Spacer(modifier = Modifier.height(16.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = onRetry) {
                Text(stringResource(R.string.speechkit_assistant_action_retry))
            }
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.speechkit_assistant_action_close))
            }
        }
    }
}
