package io.kombify.speechkit.shortcuts

/**
 * Voice shortcut resolver.
 *
 * Mirrors: internal/shortcuts/resolver.go Resolver + types.go definitions.
 * Detects command prefixes in transcribed text and maps them to actions.
 */
interface ShortcutResolver {
    /**
     * Check if the transcribed text starts with a known shortcut prefix.
     * @return the resolved shortcut, or null if no match
     */
    fun resolve(text: String): ResolvedShortcut?
}

/** A matched voice shortcut with its action and remaining text. */
data class ResolvedShortcut(
    val action: ShortcutAction,
    val remainingText: String,
)

/** Available voice shortcut actions. */
enum class ShortcutAction {
    QUICK_NOTE,
    COPY_LAST,
    INSERT_LAST,
    SUMMARIZE,
    OPEN_HOME,
    OPEN_LIBRARY,
}
