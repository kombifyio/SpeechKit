package io.kombify.speechkit.shortcuts

/**
 * Default voice shortcut resolver with prefix-based matching.
 *
 * Mirrors: internal/shortcuts/resolver.go DefaultResolver.
 * Matches spoken text against known command prefixes.
 */
class DefaultShortcutResolver(
    private val prefixes: Map<String, ShortcutAction> = DEFAULT_PREFIXES,
) : ShortcutResolver {

    override fun resolve(text: String): ResolvedShortcut? {
        val normalized = text.trim().lowercase()

        for ((prefix, action) in prefixes) {
            if (normalized.startsWith(prefix)) {
                val remaining = text.trim().substring(prefix.length).trim()
                return ResolvedShortcut(action = action, remainingText = remaining)
            }
        }

        return null
    }

    companion object {
        val DEFAULT_PREFIXES = mapOf(
            // German
            "schnelle notiz" to ShortcutAction.QUICK_NOTE,
            "quick note" to ShortcutAction.QUICK_NOTE,
            "notiz" to ShortcutAction.QUICK_NOTE,
            "kopiere letztes" to ShortcutAction.COPY_LAST,
            "copy last" to ShortcutAction.COPY_LAST,
            "einfuegen" to ShortcutAction.INSERT_LAST,
            "insert last" to ShortcutAction.INSERT_LAST,
            "zusammenfassen" to ShortcutAction.SUMMARIZE,
            "summarize" to ShortcutAction.SUMMARIZE,
            "oeffne home" to ShortcutAction.OPEN_HOME,
            "open home" to ShortcutAction.OPEN_HOME,
            "oeffne bibliothek" to ShortcutAction.OPEN_LIBRARY,
            "open library" to ShortcutAction.OPEN_LIBRARY,
        )
    }
}
