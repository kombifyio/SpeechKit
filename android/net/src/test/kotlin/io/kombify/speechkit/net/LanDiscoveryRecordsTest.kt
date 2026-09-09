package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
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
    fun `an announcement that carried credentials is rejected`() {
        assertNull(
            LanDiscoveryRecords.parseTxtLines(
                "wohnzimmer",
                listOf(
                    "url=http://192.168.1.20:8080",
                    "token=svc-secret",
                    "auth=Bearer abc",
                ),
            ),
        )
        assertNull(
            LanDiscoveryRecords.selfHostSelection(
                "wohnzimmer",
                mapOf("url" to "http://192.168.1.20:8080", "token" to "svc-secret"),
            ),
        )
    }

    @Test
    fun `a credential-free announcement becomes a self-host profile`() {
        val selected = LanDiscoveryRecords.selfHostSelection(
            "wohnzimmer",
            mapOf(
                "url" to "http://192.168.1.20:8080",
                "modes" to "dictation,assist",
                "version" to "0.60.0",
            ),
            token = "typed-pairing",
        )
        assertTrue(selected != null)
        assertEquals(ConnectionMode.SELF_HOST, selected!!.mode)
        assertEquals(
            ConnectionProfile.Server("http://192.168.1.20:8080", "typed-pairing"),
            selected.profile,
        )
    }
}
