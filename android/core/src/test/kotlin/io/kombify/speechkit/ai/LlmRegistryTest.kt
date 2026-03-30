package io.kombify.speechkit.ai

import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class LlmRegistryTest {

    private fun fakeProvider(name: String, response: String = "generated"): LlmProvider = object : LlmProvider {
        override val name: String = name
        override suspend fun generate(prompt: String, opts: GenerateOpts) = GenerateResult(
            text = response, provider = name, tokensUsed = 10, latencyMs = 50,
        )
        override suspend fun health() {}
    }

    private fun failingProvider(name: String): LlmProvider = object : LlmProvider {
        override val name: String = name
        override suspend fun generate(prompt: String, opts: GenerateOpts): GenerateResult =
            throw RuntimeException("$name unavailable")
        override suspend fun health() { throw RuntimeException("$name unhealthy") }
    }

    @Test
    fun `generates with first available provider`() = runTest {
        val registry = LlmRegistry(mutableListOf(
            fakeProvider("local", "local-response"),
            fakeProvider("cloud", "cloud-response"),
        ))

        val result = registry.generate("test prompt")
        assertEquals("local-response", result.text)
        assertEquals("local", result.provider)
    }

    @Test
    fun `falls back to next provider on failure`() = runTest {
        val registry = LlmRegistry(mutableListOf(
            failingProvider("local"),
            fakeProvider("cloud", "cloud-response"),
        ))

        val result = registry.generate("test prompt")
        assertEquals("cloud-response", result.text)
        assertEquals("cloud", result.provider)
    }

    @Test
    fun `throws when all providers fail`() = runTest {
        val registry = LlmRegistry(mutableListOf(
            failingProvider("local"),
            failingProvider("cloud"),
        ))

        assertThrows<IllegalStateException> {
            registry.generate("test prompt")
        }
    }

    @Test
    fun `throws when no providers configured`() = runTest {
        val registry = LlmRegistry()
        assertThrows<IllegalStateException> {
            registry.generate("test prompt")
        }
    }

    @Test
    fun `availableProviders returns all names`() {
        val registry = LlmRegistry(mutableListOf(
            fakeProvider("local"),
            fakeProvider("cloud"),
        ))
        assertEquals(listOf("local", "cloud"), registry.availableProviders())
    }

    @Test
    fun `remove provider by name`() {
        val registry = LlmRegistry(mutableListOf(
            fakeProvider("local"),
            fakeProvider("cloud"),
        ))
        registry.remove("local")
        assertEquals(listOf("cloud"), registry.availableProviders())
    }
}
