package io.kombify.speechkit.ai

import timber.log.Timber

/**
 * AI-powered text actions for the keyboard.
 *
 * Orchestrates LLM calls for text transformation:
 * - Rewrite: Reformulate text while preserving meaning
 * - Tone: Adjust formality/style
 * - Summarize: Condense text
 * - Suggest: Generate completions
 *
 * Each action constructs a prompt and delegates to LlmRegistry.
 */
class TextActions(
    private val llmRegistry: LlmRegistry,
) {
    /**
     * Rewrite text to improve clarity and flow.
     * @param text The text to rewrite
     * @param language Target language code ("de", "en")
     * @return Rewritten text
     */
    suspend fun rewrite(text: String, language: String = "de"): String {
        val langName = languageName(language)
        val prompt = """Rewrite the following text in $langName to improve clarity and flow.
Keep the same meaning. Return only the rewritten text, nothing else.

Text: $text"""

        val result = llmRegistry.generate(prompt, GenerateOpts(
            maxTokens = (text.length * 1.5).toInt().coerceIn(64, 512),
            temperature = 0.3f,
        ))

        Timber.d("Rewrite: ${text.length} -> ${result.text.length} chars via ${result.provider}")
        return result.text.trim()
    }

    /**
     * Adjust the tone of text.
     * @param text The text to adjust
     * @param tone Target tone: "formal", "casual", "professional", "concise", "friendly"
     * @param language Target language code
     * @return Text with adjusted tone
     */
    suspend fun adjustTone(text: String, tone: String, language: String = "de"): String {
        val langName = languageName(language)
        val toneDescription = when (tone) {
            "formal" -> "formal and professional"
            "casual" -> "casual and friendly"
            "professional" -> "business professional"
            "concise" -> "shorter and more concise"
            "friendly" -> "warm and approachable"
            else -> tone
        }

        val prompt = """Rewrite the following text in $langName with a $toneDescription tone.
Keep the core meaning. Return only the rewritten text, nothing else.

Text: $text"""

        val result = llmRegistry.generate(prompt, GenerateOpts(
            maxTokens = (text.length * 1.5).toInt().coerceIn(64, 512),
            temperature = 0.4f,
        ))

        Timber.d("Tone ($tone): ${text.length} -> ${result.text.length} chars")
        return result.text.trim()
    }

    /**
     * Summarize text.
     * @param text The text to summarize
     * @param language Target language code
     * @return Summarized text
     */
    suspend fun summarize(text: String, language: String = "de"): String {
        val langName = languageName(language)
        val prompt = """Summarize the following text in $langName in 1-2 sentences.
Return only the summary, nothing else.

Text: $text"""

        val result = llmRegistry.generate(prompt, GenerateOpts(
            maxTokens = 128,
            temperature = 0.3f,
        ))

        Timber.d("Summarize: ${text.length} -> ${result.text.length} chars")
        return result.text.trim()
    }

    /**
     * Generate smart suggestions / completions.
     * @param context Recent text context
     * @param count Number of suggestions to generate
     * @param language Target language code
     * @return List of suggestion strings
     */
    suspend fun suggest(context: String, count: Int = 3, language: String = "de"): List<String> {
        val langName = languageName(language)
        val prompt = """Given the following text context in $langName, suggest $count possible
next phrases (2-5 words each). Return each suggestion on a new line, nothing else.

Context: $context"""

        val result = llmRegistry.generate(prompt, GenerateOpts(
            maxTokens = 64,
            temperature = 0.8f,
        ))

        return result.text.trim()
            .lines()
            .map { it.trim().removePrefix("- ").removePrefix("* ") }
            .filter { it.isNotBlank() }
            .take(count)
    }

    private fun languageName(code: String): String = when (code) {
        "de" -> "German"
        "en" -> "English"
        "fr" -> "French"
        "es" -> "Spanish"
        "it" -> "Italian"
        "pt" -> "Portuguese"
        else -> "the target language"
    }
}
