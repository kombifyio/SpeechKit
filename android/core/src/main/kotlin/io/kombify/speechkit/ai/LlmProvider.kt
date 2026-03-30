package io.kombify.speechkit.ai

/**
 * LLM provider interface for AI text actions.
 *
 * Abstracts the AI backend used for keyboard intelligence:
 * - Text rewriting
 * - Tone adjustment
 * - Summarization
 * - Smart suggestions / next-word prediction
 *
 * Implementations:
 * - On-device: Small model via ONNX (Phi-3 mini, Gemma 2B)
 * - Cloud: HuggingFace Inference API or kombify AI backend
 */
interface LlmProvider {
    /** Provider identifier. */
    val name: String

    /** Generate a text completion/response. */
    suspend fun generate(prompt: String, opts: GenerateOpts = GenerateOpts()): GenerateResult

    /** Check if the provider is available. Throws on failure. */
    suspend fun health()
}

data class GenerateOpts(
    val maxTokens: Int = 256,
    val temperature: Float = 0.7f,
    val systemPrompt: String? = null,
)

data class GenerateResult(
    val text: String,
    val provider: String,
    val tokensUsed: Int = 0,
    val latencyMs: Long = 0,
)

/**
 * Registry for multiple LLM providers with fallback chain.
 *
 * Tries providers in order until one succeeds.
 * Similar pattern to SttRouter but for text generation.
 */
class LlmRegistry(
    private val providers: MutableList<LlmProvider> = mutableListOf(),
) {
    fun add(provider: LlmProvider) {
        providers.add(provider)
    }

    fun remove(name: String) {
        providers.removeAll { it.name == name }
    }

    suspend fun generate(prompt: String, opts: GenerateOpts = GenerateOpts()): GenerateResult {
        for (provider in providers) {
            return try {
                provider.generate(prompt, opts)
            } catch (e: Exception) {
                continue
            }
        }
        error("No LLM provider available")
    }

    fun availableProviders(): List<String> = providers.map { it.name }
}
