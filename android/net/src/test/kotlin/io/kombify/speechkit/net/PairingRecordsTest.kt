package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class PairingRecordsTest {

    @Test
    fun `a v1 payload with url and token becomes self host`() {
        val selected = PairingRecords.consume(
            """
            {"v":"speechkit.pairing.v1","server_url":"http://192.168.1.20:8080/","auth":"bearer","token":"sk-server-pairing","name":"wohnzimmer"}
            """.trimIndent(),
        )
        assertEquals(ConnectionMode.SELF_HOST, selected?.mode)
        assertEquals(
            ConnectionProfile.Server("http://192.168.1.20:8080", "sk-server-pairing"),
            selected?.profile,
        )
    }

    @Test
    fun `missing url or token does not persist cloud or tester origin`() {
        assertNull(
            PairingRecords.consume(
                """{"v":"speechkit.pairing.v1","server_url":"","auth":"bearer","token":"sk-server-pairing"}""",
            ),
        )
        assertNull(
            PairingRecords.consume(
                """{"v":"speechkit.pairing.v1","server_url":"https://speechkit.kombify.io","auth":"bearer","token":""}""",
            ),
        )
        assertNull(
            PairingRecords.consume(
                """{"v":"speechkit.pairing.v1","server_url":"https://speechkit.kombify.io","auth":"oidc","token":"sk"}""",
            ),
        )
        assertNull(PairingRecords.consume("url=http://192.168.1.20:8080\ntoken=svc-secret"))
    }
}
