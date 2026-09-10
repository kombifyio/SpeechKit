package io.kombify.speechkit.assistant.service

import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.SpeechKitApiException
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class ListenSessionRecoveryTest {

    private val dead = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "dead-jwt")
    private val fresh = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "fresh-jwt")
    private val unauthorized = SpeechKitApiException(401, "unauthenticated", "identity not available")

    @Test
    fun aCloudUnauthorizedUsesTheRecoveredSession() {
        val recovered = recoveredListenProfile(dead, unauthorized) { failed ->
            if (failed == dead) fresh else null
        }
        assertEquals(fresh, recovered)
    }

    @Test
    fun aNonUnauthorizedFailureDoesNotReprovision() {
        val forbidden = SpeechKitApiException(403, "forbidden", "not allowed")
        assertNull(recoveredListenProfile(dead, forbidden) { fresh })
    }

    @Test
    fun aFailedRecoveryDoesNotKeepTheDeadBearer() {
        assertNull(recoveredListenProfile(dead, unauthorized) { null })
    }
}
