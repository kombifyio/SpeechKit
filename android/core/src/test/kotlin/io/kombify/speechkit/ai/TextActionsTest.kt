package io.kombify.speechkit.ai

import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class TextActionsTest {

    private fun mockRegistry(response: String): LlmRegistry {
        val provider = object : LlmProvider {
            override val name: String = "mock"
            var lastPrompt: String = ""
            override suspend fun generate(prompt: String, opts: GenerateOpts): GenerateResult {
                lastPrompt = prompt
                return GenerateResult(text = response, provider = name)
            }
            override suspend fun health() {}
        }
        return LlmRegistry(mutableListOf(provider))
    }

    @Test
    fun `rewrite returns transformed text`() = runTest {
        val actions = TextActions(mockRegistry("Der verbesserte Text"))
        val result = actions.rewrite("Ein Text", "de")
        assertEquals("Der verbesserte Text", result)
    }

    @Test
    fun `adjustTone returns toned text`() = runTest {
        val actions = TextActions(mockRegistry("Sehr geehrte Damen und Herren"))
        val result = actions.adjustTone("Hey Leute", "formal", "de")
        assertEquals("Sehr geehrte Damen und Herren", result)
    }

    @Test
    fun `summarize returns shorter text`() = runTest {
        val actions = TextActions(mockRegistry("Kurze Zusammenfassung"))
        val result = actions.summarize("Ein langer Text mit vielen Details", "de")
        assertEquals("Kurze Zusammenfassung", result)
    }

    @Test
    fun `suggest returns list of suggestions`() = runTest {
        val actions = TextActions(mockRegistry("Vorschlag eins\nVorschlag zwei\nVorschlag drei"))
        val result = actions.suggest("Hallo, wie", count = 3, language = "de")
        assertEquals(3, result.size)
        assertEquals("Vorschlag eins", result[0])
    }

    @Test
    fun `suggest handles bullet-point formatting`() = runTest {
        val actions = TextActions(mockRegistry("- Erste Option\n- Zweite Option"))
        val result = actions.suggest("Context", count = 3)
        assertEquals(2, result.size)
        assertEquals("Erste Option", result[0])
    }

    @Test
    fun `suggest respects count limit`() = runTest {
        val actions = TextActions(mockRegistry("A\nB\nC\nD\nE"))
        val result = actions.suggest("Context", count = 2)
        assertEquals(2, result.size)
    }
}
