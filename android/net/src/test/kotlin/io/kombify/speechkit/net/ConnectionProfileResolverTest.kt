package io.kombify.speechkit.net

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ConnectionProfileResolverTest {

    private val hosted = ConnectionProfile.Server("https://speechkit.kombify.io", "tester-token")
    private val typed = ConnectionProfile.Server("http://192.168.1.20:8080", "lan-token")
    private val companion = ConnectionProfile.Server("https://speechkit.kombify.io", "user-token")

    @Test
    fun `companion login supplies the user token`() {
        val profile = resolveConnectionProfile(companion, typed, hosted)
        assertEquals(companion, profile)
    }

    @Test
    fun `a tester build uses the hosted SpeechKit without typing a server`() {
        val profile = resolveConnectionProfile(null, null, hosted)
        assertEquals(hosted, profile)
    }

    @Test
    fun `a typed homelab override still wins over the tester default`() {
        assertEquals(typed, resolveConnectionProfile(null, typed, hosted))
    }

    @Test
    fun `without companion, stored, or shipped the floor is on-device`() {
        assertTrue(resolveConnectionProfile(null, null, null) is ConnectionProfile.SystemOnDevice)
    }

    @Test
    fun `a companion gateway root maps origin paths onto the speechkit perimeter`() {
        val gateway = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        assertEquals(
            "https://api.kombify.io/v1/speechkit/dictation/transcribe",
            gateway.endpoint("/v1/dictation/transcribe"),
        )
        assertEquals(
            "https://api.kombify.io/v1/speechkit/healthz",
            gateway.endpoint("/healthz"),
        )
    }

    @Test
    fun `an origin SpeechKit URL keeps the v1 prefix`() {
        val origin = ConnectionProfile.Server("https://speechkit.kombify.io", "tester-token")
        assertEquals(
            "https://speechkit.kombify.io/v1/dictation/transcribe",
            origin.endpoint("/v1/dictation/transcribe"),
        )
    }
}
