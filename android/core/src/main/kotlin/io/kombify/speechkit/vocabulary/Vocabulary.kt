package io.kombify.speechkit.vocabulary

import java.util.regex.Matcher
import java.util.regex.Pattern

/** One enabled word of a vocabulary pack: the canonical spelling and how it tends to be misheard. */
data class VocabularyTerm(
    val term: String,
    /** Spoken or misheard variants (the pack's `sounds_like`) that should read as [term]. */
    val soundsLike: List<String> = emptyList(),
)

/**
 * The vocabulary one signed-in account carries to this device.
 *
 * Two jobs, both mirroring the Go kernel:
 * - [keyterms] bias hosted recognition (`customize.BuildKeyterms`).
 * - [rewrite] turns the aliases an on-device recognizer produced into the
 *   canonical term: case-insensitive, whole-word, alias to term (the
 *   `spoken_alias` replacement of `customize.SynonymReplacement`). Java's `\b`
 *   is Unicode-aware where Go's is ASCII-only, so an alias next to an accented
 *   letter does not match here.
 *
 * Immutable; the alias patterns compile once.
 */
class Vocabulary(terms: List<VocabularyTerm>) {

    private val terms: List<VocabularyTerm> = terms.mapNotNull { raw ->
        val term = raw.term.trim()
        if (term.isEmpty()) {
            null
        } else {
            VocabularyTerm(term, normalizeAliases(term, raw.soundsLike))
        }
    }

    private class Rule(val pattern: Pattern, val replacement: String)

    // Longest alias first, so "foo bar" wins over "foo" where both exist.
    private val rules: List<Rule> = this.terms
        .flatMap { entry -> entry.soundsLike.map { alias -> alias to entry.term } }
        .sortedByDescending { (alias, _) -> alias.length }
        .map { (alias, term) ->
            Rule(
                Pattern.compile(
                    "\\b" + Pattern.quote(alias) + "\\b",
                    Pattern.CASE_INSENSITIVE or Pattern.UNICODE_CASE,
                ),
                Matcher.quoteReplacement(term),
            )
        }

    val isEmpty: Boolean get() = terms.isEmpty()

    /** Canonical terms in pack order, without duplicates. */
    val canonicalTerms: List<String> = this.terms.map { it.term }.distinctBy { it.lowercase() }

    /** Canonical terms, then (when [withAliases]) their aliases, de-duplicated case-insensitively. */
    fun keyterms(withAliases: Boolean = true): List<String> {
        if (!withAliases) return canonicalTerms
        val seen = HashSet<String>()
        return (canonicalTerms + this.terms.flatMap { it.soundsLike }).filter { seen.add(it.lowercase()) }
    }

    /**
     * The account's misheard variants with the term they stand for, for display. The pairs are
     * taken from the pack and are not part of this source.
     */
    fun aliasPairs(): List<Pair<String, String>> =
        this.terms.flatMap { entry -> entry.soundsLike.map { alias -> alias to entry.term } }

    /** [text] with every alias replaced by its canonical term. */
    fun rewrite(text: String): String {
        if (rules.isEmpty() || text.isEmpty()) return text
        var out = text
        for (rule in rules) out = rule.pattern.matcher(out).replaceAll(rule.replacement)
        return out
    }

    companion object {
        val EMPTY = Vocabulary(emptyList())

        // Go's NormalizeAliasList: trimmed, no empties, not the term itself, de-duplicated.
        private fun normalizeAliases(term: String, aliases: List<String>): List<String> {
            val seen = hashSetOf(term.lowercase())
            return aliases.map { it.trim() }.filter { it.isNotEmpty() && seen.add(it.lowercase()) }
        }
    }
}

/**
 * The vocabulary sessions of this process apply right now. Empty until a
 * signed-in account's pack is installed, and emptied again on sign-out.
 */
object ActiveVocabulary {
    @Volatile
    var current: Vocabulary = Vocabulary.EMPTY
        private set

    fun set(vocabulary: Vocabulary) {
        current = vocabulary
    }

    fun clear() {
        current = Vocabulary.EMPTY
    }
}
