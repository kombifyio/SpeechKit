package io.kombify.speechkit.ime.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.MicOff
import androidx.compose.material3.Icon
import androidx.compose.material3.Button
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.ime.ImeVoiceAgentController
import io.kombify.speechkit.ime.R
import io.kombify.speechkit.ime.VoicePanelController
import io.kombify.speechkit.net.KombifyLinks
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.net.R as NetR
import io.kombify.speechkit.voiceui.OpalProfile
import io.kombify.speechkit.voiceui.VoiceAuraState
import io.kombify.speechkit.voiceui.VoiceOpal
import java.text.BreakIterator
import java.util.Locale

/** The host owns capture and playback across both layouts. */
@Composable
fun VoiceAgentOpalPanel(
    state: VoiceAgentUiState,
    holding: Boolean,
    expanded: Boolean,
    compactAvailable: Boolean,
    onExpandedChange: (Boolean) -> Unit,
    onHoldChange: (Boolean) -> Unit,
    onEnd: () -> Unit,
    onSwitchToDictation: () -> Unit,
    onSetup: () -> Unit,
    inputLevel: Float = 0f,
    outputLevel: Float = 0f,
    provider: String? = null,
    modifier: Modifier = Modifier,
) {
    Surface(modifier = modifier.fillMaxWidth(), color = MaterialTheme.colorScheme.surface, tonalElevation = 0.dp) {
        if (expanded) {
            ExpandedAgent(state, holding, compactAvailable, onExpandedChange, onHoldChange, onEnd, onSwitchToDictation, onSetup, inputLevel, outputLevel, provider)
        } else {
            CompactAgent(state, holding, onExpandedChange, onHoldChange, onEnd, inputLevel, outputLevel, provider)
        }
    }
}

@Composable
private fun CompactAgent(
    state: VoiceAgentUiState, holding: Boolean, onExpandedChange: (Boolean) -> Unit,
    onHoldChange: (Boolean) -> Unit, onEnd: () -> Unit,
    inputLevel: Float, outputLevel: Float, provider: String?,
) {
    val active = state.phase in setOf(VoiceAgentUiState.Phase.Listening, VoiceAgentUiState.Phase.Processing, VoiceAgentUiState.Phase.Speaking)
    val opalLabel = stringResource(if (active) {
        if (holding) R.string.speechkit_ime_agent_send else R.string.speechkit_ime_agent_talk
    } else R.string.speechkit_ime_agent_title)
    Row(Modifier.fillMaxSize(), verticalAlignment = Alignment.CenterVertically) {
        // The native toolbar determines the hit target height (36–44 dp).
        ShortcutButton(opalLabel, { if (active) onHoldChange(!holding) else onExpandedChange(true) }) {
            VoiceOpal(opalState(state), inputLevel = inputLevel, outputLevel = outputLevel, sizeDp = 36, profile = OpalProfile.ORIGINAL)
        }
        Box(Modifier.weight(1f).fillMaxHeight().clickable(role = Role.Button) { onExpandedChange(true) }, contentAlignment = Alignment.CenterStart) {
            val current = currentText(state)
            val sentence = remember(current) { latestSentence(current) }
            when {
                state.errorCode == ImeVoiceAgentController.ERROR_NO_SERVER -> Text(stringResource(R.string.speechkit_ime_agent_setup_hint), style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                state.error != null || state.errorCode != null -> Text(stringResource(R.string.speechkit_ime_agent_attention), style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                sentence.isNotBlank() -> TrailingTranscript(sentence)
                else -> Text(agentStatusLabel(state, provider), style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        ShortcutButton(stringResource(R.string.speechkit_ime_agent_expand), { onExpandedChange(true) }) {
            Icon(Icons.Filled.ExpandMore, contentDescription = null, modifier = Modifier.size(20.dp))
        }
        ShortcutButton(stringResource(R.string.speechkit_ime_agent_action_end), onEnd) {
            Icon(Icons.Filled.Close, contentDescription = null, modifier = Modifier.size(20.dp))
        }
    }
}

@Composable
private fun ShortcutButton(label: String, onClick: () -> Unit, content: @Composable () -> Unit) {
    Box(Modifier.width(36.dp).fillMaxHeight().semantics { contentDescription = label }
        .clickable(role = Role.Button, onClick = onClick), contentAlignment = Alignment.Center, content = { content() })
}

@Composable
private fun ExpandedAgent(
    state: VoiceAgentUiState, holding: Boolean, compactAvailable: Boolean, onExpandedChange: (Boolean) -> Unit,
    onHoldChange: (Boolean) -> Unit, onEnd: () -> Unit, onSwitchToDictation: () -> Unit,
    onSetup: () -> Unit,
    inputLevel: Float, outputLevel: Float, provider: String?,
) {
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val needsSetup = state.errorCode == ImeVoiceAgentController.ERROR_NO_SERVER
        val shortWindow = maxHeight < 240.dp
        val diameter = when {
            maxHeight < 260.dp -> 72
            maxHeight < 280.dp -> 96
            else -> 128
        }
        Column(Modifier.fillMaxSize().padding(horizontal = 12.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Row(Modifier.fillMaxWidth().heightIn(min = 48.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(if (needsSetup) stringResource(R.string.speechkit_ime_agent_title) else agentStatusLabel(state, provider), Modifier.weight(1f), style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (compactAvailable) IconButton(onClick = { onExpandedChange(false) }, modifier = Modifier.sizeIn(minWidth = 48.dp, minHeight = 48.dp)) {
                    Icon(Icons.Filled.ExpandLess, contentDescription = stringResource(R.string.speechkit_ime_agent_collapse))
                }
            }
            if (shortWindow) {
                Row(Modifier.weight(1f).fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    AgentOpal(state, inputLevel, outputLevel, 64, onHoldChange)
                    if (needsSetup) SetupPrompt(onSetup, Modifier.weight(1f).fillMaxHeight())
                    else AgentTranscript(state, Modifier.weight(1f).fillMaxHeight())
                }
            } else {
                AgentOpal(state, inputLevel, outputLevel, diameter, onHoldChange)
                if (needsSetup) SetupPrompt(onSetup, Modifier.weight(1f).fillMaxWidth())
                else AgentTranscript(state, Modifier.weight(1f).fillMaxWidth())
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceEvenly, verticalAlignment = Alignment.CenterVertically) {
                AgentControls(state, holding, onHoldChange, onEnd, onSwitchToDictation)
            }
        }
    }
}

@Composable
private fun SetupPrompt(onSetup: () -> Unit, modifier: Modifier) {
    Column(modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        Text(stringResource(R.string.speechkit_ime_agent_setup_body), style = MaterialTheme.typography.bodyMedium,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center)
        Button(onClick = onSetup, modifier = Modifier.padding(top = 8.dp).heightIn(min = 48.dp)) {
            Text(stringResource(R.string.speechkit_ime_agent_setup))
        }
    }
}

@Composable
private fun AgentTranscript(state: VoiceAgentUiState, modifier: Modifier) {
    Column(modifier.verticalScroll(rememberScrollState()).padding(vertical = 4.dp)) {
        if (state.userText.isNotBlank()) {
            Text(stringResource(R.string.speechkit_ime_agent_speaker_user), style = MaterialTheme.typography.labelSmall)
            Text(state.userText, style = MaterialTheme.typography.bodyMedium)
        }
        if (state.agentText.isNotBlank()) {
            Text(stringResource(R.string.speechkit_ime_agent_speaker_agent), style = MaterialTheme.typography.labelSmall)
            Text(state.agentText, style = MaterialTheme.typography.bodyMedium)
        }
        AgentError(state, compact = false)
    }
}

@Composable
private fun AgentOpal(state: VoiceAgentUiState, input: Float, output: Float, size: Int, onHoldChange: (Boolean) -> Unit) {
    val speaking = state.phase == VoiceAgentUiState.Phase.Speaking
    val interrupt = stringResource(R.string.speechkit_ime_agent_talk)
    val modifier = if (speaking) Modifier.clickable(onClickLabel = interrupt) { onHoldChange(true) } else Modifier
    VoiceOpal(opalState(state), modifier = modifier, inputLevel = input, outputLevel = output, sizeDp = size, profile = OpalProfile.ORIGINAL)
}

private fun opalState(state: VoiceAgentUiState): VoiceAuraState =
    if ((state.error != null || state.errorCode != null) && state.errorCode != ImeVoiceAgentController.ERROR_NO_SERVER) VoiceAuraState.ERROR else state.phase.toAuraState()

@Composable
private fun AgentControls(state: VoiceAgentUiState, holding: Boolean, onHoldChange: (Boolean) -> Unit, onEnd: () -> Unit, onSwitchToDictation: () -> Unit) {
    val enabled = state.phase != VoiceAgentUiState.Phase.Connecting && state.phase != VoiceAgentUiState.Phase.Inactive && state.phase != VoiceAgentUiState.Phase.Ended
    if (state.errorCode != ImeVoiceAgentController.ERROR_NO_SERVER) IconButton(onClick = { onHoldChange(!holding) }, enabled = enabled, modifier = Modifier.sizeIn(minWidth = 48.dp, minHeight = 48.dp)) {
        Icon(if (holding) Icons.Filled.MicOff else Icons.Filled.Mic, contentDescription = stringResource(if (holding) R.string.speechkit_ime_agent_send else R.string.speechkit_ime_agent_talk))
    }
    TextButton(onClick = onSwitchToDictation, modifier = Modifier.heightIn(min = 48.dp)) {
        Text(stringResource(R.string.speechkit_ime_agent_action_dictation))
    }
    IconButton(onClick = onEnd, modifier = Modifier.sizeIn(minWidth = 48.dp, minHeight = 48.dp)) {
        Icon(Icons.Filled.Close, contentDescription = stringResource(R.string.speechkit_ime_agent_action_end))
    }
}

@Composable
private fun AgentError(state: VoiceAgentUiState, compact: Boolean) {
    if (state.errorCode == ImeVoiceAgentController.ERROR_NO_SERVER || (state.error == null && state.errorCode == null)) return
    Text(agentErrorLabel(state.errorCode, state.error, state.errorClass), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error, maxLines = if (compact) 2 else Int.MAX_VALUE, overflow = TextOverflow.Ellipsis)
    if (VoicePanelController.isPlanDenialCode(state.errorCode)) {
        val context = LocalContext.current
        TextButton(onClick = { KombifyLinks.open(context, KombifyLinks.PLANS_URL) }, modifier = Modifier.heightIn(min = 48.dp)) {
            Text(stringResource(NetR.string.plan_denial_see_plans))
        }
    }
}

private fun currentText(state: VoiceAgentUiState): String = when (state.latestSpeaker) {
    VoiceAgentUiState.Speaker.USER -> state.userText
    VoiceAgentUiState.Speaker.AGENT -> state.agentText
    null -> ""
}

/** Keep the newest words visible without scrolling or moving the transcript container. */
@Composable
private fun TrailingTranscript(sentence: String) {
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val style = MaterialTheme.typography.bodySmall
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        val width = with(density) { maxWidth.roundToPx() }.coerceAtLeast(1)
        val text = remember(sentence, width, density, style) {
            fun fits(value: String) = !measurer.measure(value, style, maxLines = 1,
                constraints = Constraints(maxWidth = width)).hasVisualOverflow
            if (fits(sentence)) sentence else {
                val boundaries = mutableListOf<Int>()
                val words = BreakIterator.getCharacterInstance(Locale.getDefault()).apply { setText(sentence) }
                var boundary = words.first()
                while (boundary != BreakIterator.DONE) {
                    boundaries.add(boundary)
                    boundary = words.next()
                }
                var low = 0
                var high = boundaries.lastIndex
                while (low < high) {
                    val mid = (low + high) / 2
                    if (fits("…" + sentence.substring(boundaries[mid]).trimStart())) high = mid else low = mid + 1
                }
                "…" + sentence.substring(boundaries[low]).trimStart()
            }
        }
        Text(text, style = style, maxLines = 1)
    }
}

private fun latestSentence(text: String): String {
    val trimmed = text.trim()
    if (trimmed.isEmpty()) return ""
    val iterator = BreakIterator.getSentenceInstance(Locale.getDefault()).apply { setText(trimmed) }
    val end = iterator.last()
    val start = iterator.previous().let { if (it == BreakIterator.DONE) 0 else it }
    return trimmed.substring(start, end).trim()
}
