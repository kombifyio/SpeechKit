package io.kombify.speechkit.ime.ui

import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.voiceui.VoiceAuraState

/**
 * The only adapter from Voice Agent session phases to the shared orb.
 *
 * Session vocabulary (`VoiceAgentUiState.Phase`) and visual vocabulary
 * (`VoiceAuraState`) stay separate on purpose: a visual change must not
 * force a protocol change. Keyboard panel and in-app test surface both call
 * this instead of each mapping the six phases.
 */
fun VoiceAgentUiState.Phase.toAuraState(): VoiceAuraState = when (this) {
    VoiceAgentUiState.Phase.Inactive -> VoiceAuraState.INACTIVE
    VoiceAgentUiState.Phase.Connecting -> VoiceAuraState.CONNECTING
    VoiceAgentUiState.Phase.Listening -> VoiceAuraState.LISTENING
    VoiceAgentUiState.Phase.Processing -> VoiceAuraState.PROCESSING
    VoiceAgentUiState.Phase.Speaking -> VoiceAuraState.SPEAKING
    VoiceAgentUiState.Phase.Ended -> VoiceAuraState.SETTLING
}
