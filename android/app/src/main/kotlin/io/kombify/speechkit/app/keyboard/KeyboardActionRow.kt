package io.kombify.speechkit.app.keyboard

import androidx.annotation.ColorInt
import androidx.annotation.StringRes
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.AssistChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
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
 * The strip's own palette, resolved from the keyboard it is stacked on.
 *
 * The row lives outside `main_keyboard_frame`, so HeliBoard's colouring pass
 * never reaches it, and its colours are user settings rather than theme
 * attributes, so no themed context carries them either. Composed under a bare
 * `MaterialTheme` the row therefore painted itself in the default light
 * palette however dark the keys below it were. [surface] is what the keyboard
 * is actually painted in; [content] follows from it so the labels stay
 * legible on either.
 */
data class KeyboardActionRowPalette(
    @param:ColorInt val surface: Int,
    @param:ColorInt val content: Int,
    val dark: Boolean,
)

/**
 * The palette for a keyboard painted in [keyboardBackground].
 *
 * [keyboardBackground] is null until the keyboard has painted itself at least
 * once — the fork applies its background on the input view's first layout
 * pass, which is after the hook that mounts this row. The system's night mode
 * is the stand-in for that one frame: it is what the default keyboard themes
 * follow, so it is wrong only for a user who pinned a theme against it, and
 * only until the real colour arrives.
 */
fun keyboardActionRowPalette(
    @ColorInt keyboardBackground: Int?,
    nightMode: Boolean,
): KeyboardActionRowPalette {
    val surface = keyboardBackground ?: if (nightMode) FALLBACK_DARK else FALLBACK_LIGHT
    val dark = relativeLuminance(surface) < DARK_SURFACE_LUMINANCE
    return KeyboardActionRowPalette(
        surface = surface,
        content = if (dark) CONTENT_ON_DARK else CONTENT_ON_LIGHT,
        dark = dark,
    )
}

/**
 * Draws [content] in the keyboard's colours.
 *
 * The host wraps the row in this instead of a bare `MaterialTheme`, which
 * would hand the chips the default light palette on every keyboard theme.
 */
@Composable
fun KeyboardActionRowTheme(
    palette: KeyboardActionRowPalette,
    content: @Composable () -> Unit,
) {
    MaterialTheme(colorScheme = keyboardActionRowScheme(palette), content = content)
}

private fun keyboardActionRowScheme(palette: KeyboardActionRowPalette): ColorScheme {
    val surface = Color(palette.surface)
    val content = Color(palette.content)
    return (if (palette.dark) darkColorScheme() else lightColorScheme()).copy(
        surface = surface,
        surfaceVariant = surface,
        background = surface,
        onSurface = content,
        onBackground = content,
        onSurfaceVariant = content.copy(alpha = 0.72f),
        outline = content.copy(alpha = 0.38f),
        outlineVariant = content.copy(alpha = 0.24f),
    )
}

/**
 * sRGB relative luminance per WCAG. Written out rather than taken from
 * `android.graphics.ColorUtils` so the rule that decides light-on-dark is
 * plain arithmetic that a unit test can pin without a device.
 */
private fun relativeLuminance(@ColorInt color: Int): Double {
    fun channel(shift: Int): Double {
        val v = (color shr shift and 0xFF) / 255.0
        return if (v <= 0.03928) v / 12.92 else Math.pow((v + 0.055) / 1.055, 2.4)
    }
    return 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0)
}

/** Below this the keyboard reads as dark and the row needs light content. */
private const val DARK_SURFACE_LUMINANCE = 0.18

private val FALLBACK_DARK = 0xFF1E1E1E.toInt()
private val FALLBACK_LIGHT = 0xFFF1F1F1.toInt()
private val CONTENT_ON_DARK = 0xFFECECEC.toInt()
private val CONTENT_ON_LIGHT = 0xFF1B1B1B.toInt()

/**
 * The row itself: one thin strip above the keys, always present while the
 * keyboard is up.
 *
 * A blocked action keeps its place and states its reason underneath, because
 * a keyboard offers nowhere to put a tooltip and a chip that simply does
 * nothing reads as a broken key.
 */
@Composable
fun KeyboardActionRow(
    items: List<KeyboardActionItem>,
    onAction: (KeyboardAction) -> Unit,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surface,
        // Flat on purpose: tonal elevation tints the surface towards the
        // scheme's primary, which would pull the strip off the keyboard colour
        // this scheme exists to match.
        tonalElevation = 0.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 8.dp, vertical = 4.dp),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                items.forEach { item ->
                    AssistChip(
                        onClick = { onAction(item.action) },
                        enabled = item.enabled,
                        label = { Text(stringResource(item.action.label())) },
                    )
                }
            }
            keyboardActionRowBlocker(items)?.let { blocker ->
                Text(
                    text = stringResource(blocker.reason()),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@StringRes
private fun KeyboardAction.label(): Int = when (this) {
    KeyboardAction.OnDeviceDictation -> R.string.speechkit_action_on_device
    KeyboardAction.ServerDictation -> R.string.speechkit_action_server
    KeyboardAction.AgentDeepgram -> R.string.speechkit_action_agent_deepgram
    KeyboardAction.AgentAssemblyAi -> R.string.speechkit_action_agent_assemblyai
    KeyboardAction.AgentOpenAi -> R.string.speechkit_action_agent_openai
    KeyboardAction.CompanionApp -> R.string.speechkit_action_companion
}

@StringRes
private fun KeyboardActionBlocker.reason(): Int = when (this) {
    KeyboardActionBlocker.NoServer -> R.string.speechkit_action_blocked_no_server
    KeyboardActionBlocker.NoCompanion -> R.string.speechkit_action_blocked_no_companion
}
