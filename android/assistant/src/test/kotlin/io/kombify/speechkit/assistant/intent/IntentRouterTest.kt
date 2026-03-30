package io.kombify.speechkit.assistant.intent

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class IntentRouterTest {

    private val router = IntentRouter()

    @Test
    fun `classifies open app intent in German`() {
        val intent = router.classify("oeffne WhatsApp")
        assertEquals(IntentType.OPEN_APP, intent.type)
        assertEquals("whatsapp", intent.parameters["app"])
    }

    @Test
    fun `classifies open app intent in English`() {
        val intent = router.classify("open YouTube")
        assertEquals(IntentType.OPEN_APP, intent.type)
        assertEquals("youtube", intent.parameters["app"])
    }

    @Test
    fun `classifies timer intent with number`() {
        val intent = router.classify("Timer auf 5 Minuten")
        assertEquals(IntentType.SET_TIMER, intent.type)
        assertEquals("5", intent.parameters["number"])
        assertEquals("minutes", intent.parameters["unit"])
    }

    @Test
    fun `classifies timer in English`() {
        val intent = router.classify("set timer for 10 minutes")
        assertEquals(IntentType.SET_TIMER, intent.type)
        assertEquals("10", intent.parameters["number"])
    }

    @Test
    fun `classifies quick note intent`() {
        val intent = router.classify("Notiz Einkaufen nicht vergessen")
        assertEquals(IntentType.QUICK_NOTE, intent.type)
        assertEquals("einkaufen nicht vergessen", intent.parameters["content"])
    }

    @Test
    fun `classifies web search in German`() {
        val intent = router.classify("suche nach Wetter Berlin")
        assertEquals(IntentType.SEARCH_WEB, intent.type)
        assertEquals("wetter berlin", intent.parameters["query"])
    }

    @Test
    fun `classifies web search with was ist`() {
        val intent = router.classify("Was ist die Hauptstadt von Frankreich")
        assertEquals(IntentType.SEARCH_WEB, intent.type)
    }

    @Test
    fun `classifies send message intent`() {
        val intent = router.classify("Nachricht an Mama ich komme spaeter")
        assertEquals(IntentType.SEND_MESSAGE, intent.type)
        assertEquals("mama", intent.parameters["target"])
        assertTrue(intent.parameters["message"]?.contains("spaeter") == true)
    }

    @Test
    fun `classifies call intent`() {
        val intent = router.classify("ruf an Papa")
        assertEquals(IntentType.MAKE_CALL, intent.type)
        assertEquals("papa", intent.parameters["contact"])
    }

    @Test
    fun `falls back to general query for unknown text`() {
        val intent = router.classify("Erzaehl mir einen Witz")
        assertEquals(IntentType.GENERAL_QUERY, intent.type)
        assertEquals(0.5f, intent.confidence)
    }

    @Test
    fun `preserves foreground app context`() {
        router.setContext(foregroundApp = "com.whatsapp")
        val intent = router.classify("Notiz Test")
        assertEquals("com.whatsapp", intent.foregroundApp)
    }

    @Test
    fun `alarm intent extracts time`() {
        val intent = router.classify("Wecker auf 7")
        assertEquals(IntentType.SET_ALARM, intent.type)
        assertEquals("7", intent.parameters["number"])
    }
}
