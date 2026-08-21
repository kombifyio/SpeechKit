package io.kombify.speechkit.app.keyboard

import androidx.annotation.StringRes
import io.kombify.speechkit.R
import io.kombify.speechkit.net.ConnectionProfile

/**
 * The voice entry points SpeechKit offers from inside the keyboard.
 *
 * They are SpeechKit's own row rather than HeliBoard toolbar keys on purpose:
 * a native `ToolbarKey` costs an enum constant, three exhaustive icon `when`
 * blocks, a key code, a dispatch branch, a string and a vector — six to eight
 * files in the GPL fork, paid again at every upstream rebase. Six of them
 * would blow the fork's patch budget for a surface whose whole point is to be
 * rearranged while the test phase runs.
 */
enum class KeyboardAction(
    /**
     * The realtime backend to request in the Voice Agent start frame, or null
     * for the actions that are not conversations. The names are the ones the
     * server normalises (`normalizeProviderName`); a typo becomes a runtime
     * factory failure on the server, not a validation error.
     */
    val agentProvider: String? = null,
) {
    /** Dictation on the platform recognizer: no server, no keys, no account. */
    OnDeviceDictation,

    /** Dictation through the paired speechkit-server. */
    ServerDictation,

    AgentDeepgram(agentProvider = "deepgram"),
    AgentAssemblyAi(agentProvider = "assemblyai"),
    AgentOpenAi(agentProvider = "openai"),

    /** Hand the turn to the kombify Companion app. */
    CompanionApp,
}

/**
 * Why an action is shown but cannot be taken.
 *
 * Declared in the order the row prefers to name them: only one reason fits
 * under the chips, and [keyboardActionRowBlocker] shows the first that
 * applies.
 */
enum class KeyboardActionBlocker {
    /**
     * Everything past on-device dictation needs a paired speechkit-server.
     * The Voice Agent throws for any other profile before a single frame
     * moves, and the oss flavor never resolves one, so the button has to say
     * so rather than fail after the tap.
     */
    NoServer,

    /**
     * The Companion hand-off exists as a written contract and a fixture, and
     * as nothing else: no AIDL, no service binding, no package visibility
     * entry. The button is here to name the gap, not to fake it.
     */
    NoCompanion,
}

/** One button in the row, with the reason it cannot be pressed. */
data class KeyboardActionItem(
    val action: KeyboardAction,
    val blocker: KeyboardActionBlocker? = null,
) {
    val enabled: Boolean get() = blocker == null
}

/**
 * The row's contents for [profile]. Pure so the enablement rules can be read
 * and tested without a keyboard: which actions a paired server unlocks is the
 * one thing here that is easy to get quietly wrong.
 */
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

/**
 * The one reason the row states under the chips, or null when everything is
 * live.
 *
 * One line and not one per distinct reason: with no server paired both
 * reasons apply, and two full sentences stacked in a strip this thin push the
 * keys down for text nobody reads twice. [KeyboardActionBlocker]'s declaration
 * order decides which survives, and it puts the actionable one first —
 * pairing a server lights up four of the six chips, while the Companion
 * hand-off is not built yet and saying so a second time changes nothing.
 */
fun keyboardActionRowBlocker(items: List<KeyboardActionItem>): KeyboardActionBlocker? =
    items.mapNotNull { it.blocker }.minOrNull()

/**
 * The keyboard toolbar key each action answers.
 *
 * The fork reports the pressed key by its `ToolbarKey` name, which is the only
 * identity that survives the click path - the listener downstream sees a key
 * code, and six actions cannot be told apart by one. Keeping the mapping here
 * rather than in the adapter means the fork's enum and this one drift loudly,
 * in a test, instead of quietly at runtime.
 */
fun keyboardActionForToolbarKey(name: String): KeyboardAction? = when (name) {
    "SPEECHKIT_DICTATE_DEVICE" -> KeyboardAction.OnDeviceDictation
    "SPEECHKIT_DICTATE_SERVER" -> KeyboardAction.ServerDictation
    "SPEECHKIT_AGENT_DEEPGRAM" -> KeyboardAction.AgentDeepgram
    "SPEECHKIT_AGENT_ASSEMBLYAI" -> KeyboardAction.AgentAssemblyAi
    "SPEECHKIT_AGENT_GPT" -> KeyboardAction.AgentOpenAi
    "SPEECHKIT_COMPANION" -> KeyboardAction.CompanionApp
    else -> null
}

/** The string resource explaining why [KeyboardActionBlocker] refused. */
fun KeyboardActionBlocker.reasonResource(): Int = when (this) {
    KeyboardActionBlocker.NoServer -> R.string.speechkit_action_blocked_no_server
    KeyboardActionBlocker.NoCompanion -> R.string.speechkit_action_blocked_no_companion
}
