package io.kombify.speechkit.app.keyboard

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.onClick
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp

/** A per-invocation alternative; pointer cancellation never dispatches an action. */
@Composable
internal fun ExecutionShortcut(
    label: String,
    alternativeLabel: String,
    alternativeSymbol: String,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
    onAlternative: () -> Unit,
    onHoldStart: (() -> Unit)? = null,
    onHoldEnd: ((Boolean) -> Unit)? = null,
) {
    val haptic = LocalHapticFeedback.current
    var pressed by remember { mutableStateOf(false) }
    val progress by animateFloatAsState(if (pressed) 1f else 0f, tween(650), label = "shortcut hold")
    val ring = MaterialTheme.colorScheme.primary
    Surface(
        modifier = modifier.semantics(mergeDescendants = true) {
            role = Role.Button
            onClick { onClick(); true }
            customActions = listOf(CustomAccessibilityAction(alternativeLabel) { onAlternative(); true })
        }.pointerInput(onClick, onAlternative, onHoldStart, onHoldEnd) {
            awaitEachGesture {
                val down = awaitFirstDown(requireUnconsumed = false)
                down.consume()
                pressed = true
                var holding = false
                var completed = false
                try {
                    val early = withTimeoutOrNull(650) { true to waitForUpOrCancellation() }
                    if (early != null) {
                        early.second?.let { it.consume(); onClick() }
                    } else {
                        // Timeout arms the gesture; a canceled/dragged pointer still does nothing.
                        haptic.performHapticFeedback(HapticFeedbackType.LongPress)
                        if (onHoldStart != null) { holding = true; onHoldStart() }
                        val up = waitForUpOrCancellation()
                        if (up != null) { up.consume(); completed = true; if (!holding) onAlternative() }
                    }
                } finally {
                    pressed = false
                    if (holding) onHoldEnd?.invoke(completed)
                }
            }
        },
        shape = MaterialTheme.shapes.large,
        color = MaterialTheme.colorScheme.surfaceVariant,
        tonalElevation = 0.dp,
    ) {
        Box {
            Text(label, style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(horizontal = 14.dp, vertical = 14.dp))
            if (pressed) AnimatedContent(targetState = progress >= 0.98f, modifier = Modifier.align(Alignment.TopEnd).padding(end = 4.dp), label = "shortcut lock") { armed ->
                Text(if (alternativeSymbol == "🔒" && !armed) "🔓" else alternativeSymbol, style = MaterialTheme.typography.labelSmall)
            }
            Canvas(Modifier.matchParentSize()) {
                if (progress > 0f) drawArc(ring, -90f, 360f * progress, false, style = Stroke(width = 2.dp.toPx()))
            }
        }
    }
}
