package io.kombify.speechkit.net

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class LanDiscoveryRecordsTest {

    @Test
    fun `parses the announcer TXT contract`() {
        val found = LanDiscoveryRecords.parseTxtLines(
            "wohnzimmer",
            listOf(
                "url=http://192.168.1.20:8080",
                "modes=dictation,assist,voiceagent",
                "version=0.60.0",
            ),
        )
        assertEquals("wohnzimmer", found?.instanceName)
        assertEquals("http://192.168.1.20:8080", found?.url)
        assertEquals(listOf("dictation", "assist", "voiceagent"), found?.modes)
        assertEquals("0.60.0", found?.version)
    }

    @Test
    fun `a record without a url is not a server the client can dial`() {
        assertNull(
            LanDiscoveryRecords.parseTxtLines(
                "ghost",
                listOf("modes=dictation", "version=0.60.0"),
            ),
        )
    }

    @Test
    fun `credential keys in TXT never become the dial URL`() {
        val found = LanDiscoveryRecords.parseTxtLines(
            "wohnzimmer",
            listOf(
                "url=http://192.168.1.20:8080",
                "token=svc-secret",
                "auth=Bearer abc",
                "password=nope",
            ),
        )
        assertEquals("http://192.168.1.20:8080", found?.url)
        assertTrue(found?.url?.contains("svc-secret") != true)
        assertTrue(found?.url?.contains("abc") != true)
    }
}
