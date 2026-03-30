package io.kombify.speechkit.assistant.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.assistant.service.AssistantUiState

/**
 * Voice assistant overlay UI.
 *
 * Displayed as a bottom sheet / card when the assistant is active.
 * Shows:
 * - Listening animation (pulsating circles)
 * - Processing indicator
 * - Transcribed text
 * - Action result / response
 * - Error state with retry
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
                transitionSpec = { fadeIn(tween(300)) togetherWith fadeOut(tween(200)) },
                label = "assistant_state",
            ) { currentState ->
                when (currentState) {
                    is AssistantUiState.Idle -> IdleView()
                    is AssistantUiState.Listening -> ListeningView()
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
    Text(
        text = "Wie kann ich helfen?",
        style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.onSurface,
    )
}

@Composable
private fun ListeningView() {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        PulsatingCircles()
        Spacer(modifier = Modifier.height(16.dp))
        Text(
            text = "Ich hoere zu...",
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.primary,
        )
    }
}

@Composable
private fun ProcessingView() {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        CircularProgressIndicator(
            modifier = Modifier.size(48.dp),
            strokeWidth = 3.dp,
        )
        Spacer(modifier = Modifier.height(16.dp))
        Text(
            text = "Verarbeite...",
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
        CircularProgressIndicator(
            modifier = Modifier.size(24.dp),
            strokeWidth = 2.dp,
        )
    }
}

@Composable
private fun ExecutingView(actionName: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        CircularProgressIndicator(
            modifier = Modifier.size(32.dp),
            strokeWidth = 2.dp,
        )
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
        Text(
            text = text,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
        )
        Spacer(modifier = Modifier.height(16.dp))
        TextButton(onClick = onDismiss) {
            Text("Schliessen")
        }
    }
}

@Composable
private fun ErrorView(message: String, onRetry: () -> Unit, onDismiss: () -> Unit) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(
            text = message,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.error,
            textAlign = TextAlign.Center,
        )
        Spacer(modifier = Modifier.height(16.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            TextButton(onClick = onRetry) {
                Text("Nochmal")
            }
            TextButton(onClick = onDismiss) {
                Text("Schliessen")
            }
        }
    }
}

/**
 * Pulsating circles animation for the listening state.
 * Three concentric circles that pulse outward.
 */
@Composable
private fun PulsatingCircles() {
    val transition = rememberInfiniteTransition(label = "pulse")

    val scales = listOf(0f, 0.33f, 0.66f).map { delay ->
        val scale by transition.animateFloat(
            initialValue = 0.6f,
            targetValue = 1.2f,
            animationSpec = infiniteRepeatable(
                animation = tween(1200, delayMillis = (delay * 1200).toInt()),
                repeatMode = RepeatMode.Reverse,
            ),
            label = "scale_$delay",
        )
        scale
    }

    val alphas = listOf(0f, 0.33f, 0.66f).map { delay ->
        val alpha by transition.animateFloat(
            initialValue = 0.6f,
            targetValue = 0.1f,
            animationSpec = infiniteRepeatable(
                animation = tween(1200, delayMillis = (delay * 1200).toInt()),
                repeatMode = RepeatMode.Reverse,
            ),
            label = "alpha_$delay",
        )
        alpha
    }

    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier.size(80.dp),
    ) {
        scales.forEachIndexed { i, scale ->
            Box(
                modifier = Modifier
                    .size(64.dp)
                    .scale(scale)
                    .clip(CircleShape)
                    .background(MaterialTheme.colorScheme.primary.copy(alpha = alphas[i])),
            )
        }

        // Center mic icon placeholder
        Box(
            modifier = Modifier
                .size(24.dp)
                .clip(CircleShape)
                .background(MaterialTheme.colorScheme.primary),
        )
    }
}
