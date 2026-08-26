package io.kombify.speechkit.app.keyboard

import io.kombify.speechkit.R
import io.kombify.speechkit.domain.ConnectionProfile

/**
 * The voice entry points SpeechKit offers from the keyboard strip.
 *
 * They sit on HeliBoard's own suggestion strip (Gboard-shaped: suggestions in
 * the middle, two voice actions on the right, extras behind the left more
 * key). A dedicated second bar crowded the suggestions out.
 */
enum class KeyboardAction(
    /**
     * The realtime backend to request in the Voice Agent start frame, or null
     * for the actions that are not conversations. The names are the ones the
     * server normalises (`normalizeProviderName`).
     */
    val agentProvider: String? = null,
) {
    OnDeviceDictation,
    ServerDictation,
    AgentDeepgram(agentProvider = "deepgram"),
    AgentAssemblyAi(agentProvider = "assemblyai"),
    AgentOpenAi(agentProvider = "openai"),
    CompanionApp,
}

enum class KeyboardActionBlocker {
    NoServer,
    NoCompanion,
}

data class KeyboardActionItem(
    val action: KeyboardAction,
    val blocker: KeyboardActionBlocker? = null,
) {
    val enabled: Boolean get() = blocker == null
}

fun keyboardActionRowItems(profile: ConnectionProfile): List<KeyboardActionItem> {
    val server = if (profile is ConnectionProfile.Server) null else KeyboardActionBlocker.NoServer
    return listOf(
        KeyboardActionItem(KeyboardAction.OnDeviceDictation),
        KeyboardActionItem(KeyboardAction.ServerDictation, server),
        KeyboardActionItem(KeyboardAction.AgentDeepgram, server),
        KeyboardActionItem(KeyboardAction.AgentAssemblyAi, server),
        KeyboardActionItem(KeyboardAction.AgentOpenAi, server),
        KeyboardActionItem(KeyboardAction.CompanionApp, KeyboardActionBlocker.NoCompanion),
    )
}

fun keyboardActionRowBlocker(items: List<KeyboardActionItem>): KeyboardActionBlocker? =
    items.mapNotNull { it.blocker }.minOrNull()

/** True for strip/More Voice Agent keys: the IME panel, never ROLE_ASSISTANT. */
fun KeyboardAction.opensVoiceAgentInIme(): Boolean = agentProvider != null

/** The always-visible strip agent key. Settings chooses which backend it opens. */
const val STRIP_AGENT_TOOLBAR_KEY: String = "SPEECHKIT_AGENT_DEEPGRAM"

fun keyboardActionForToolbarKey(name: String): KeyboardAction? = when (name) {
    "SPEECHKIT_DICTATE_DEVICE" -> KeyboardAction.OnDeviceDictation
    "SPEECHKIT_DICTATE_SERVER" -> KeyboardAction.ServerDictation
    "SPEECHKIT_AGENT_DEEPGRAM" -> KeyboardAction.AgentDeepgram
    "SPEECHKIT_AGENT_ASSEMBLYAI" -> KeyboardAction.AgentAssemblyAi
    "SPEECHKIT_AGENT_GPT" -> KeyboardAction.AgentOpenAi
    "SPEECHKIT_COMPANION" -> KeyboardAction.CompanionApp
    else -> null
}

/**
 * Strip agent key follows the Settings backend. GPT and AssemblyAI keys in
 * More stay those providers, so GPT can move to Call GPT without stealing
 * the default Voice Agent slot.
 */
fun keyboardActionForToolbarKey(name: String, stripProvider: KeyboardAction): KeyboardAction? =
    if (name == STRIP_AGENT_TOOLBAR_KEY) stripProvider else keyboardActionForToolbarKey(name)

fun KeyboardActionBlocker.reasonResource(): Int = when (this) {
    KeyboardActionBlocker.NoServer -> R.string.speechkit_action_blocked_no_server
    KeyboardActionBlocker.NoCompanion -> R.string.speechkit_action_blocked_no_companion
}
