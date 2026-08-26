package io.kombify.speechkit.domain

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ConnectionProfileResolverTest {

    private val hosted = ConnectionProfile.Server("https://speechkit.kombify.io", "tester-token")
    private val typed = ConnectionProfile.Server("http://192.168.1.20:8080", "lan-token")
    private val companion = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-token")

    @Test
    fun cloudModeUsesTheCompanionSession() {
        val profile = resolveConnectionProfile(
            ConnectionMode.KOMBIFY_CLOUD,
            companion,
            typed,
            hosted,
        )
        assertEquals(companion, profile)
    }

    @Test
    fun originModeKeepsTheShippedTesterServerWhenCompanionIsPresent() {
        val profile = resolveConnectionProfile(
            ConnectionMode.SPEECHKIT_ORIGIN,
            companion,
            typed,
            hosted,
        )
        assertEquals(hosted, profile)
    }

    @Test
    fun aTesterBuildUsesTheHostedSpeechKitWithoutTypingAServer() {
        val profile = resolveConnectionProfile(
            ConnectionMode.SPEECHKIT_ORIGIN,
            null,
            null,
            hosted,
        )
        assertEquals(hosted, profile)
    }

    @Test
    fun selfHostUsesTheTypedServerAndIgnoresCompanion() {
        assertEquals(
            typed,
            resolveConnectionProfile(ConnectionMode.SELF_HOST, companion, typed, hosted),
        )
    }

    @Test
    fun onDeviceIgnoresEveryServer() {
        assertTrue(
            resolveConnectionProfile(ConnectionMode.ON_DEVICE, companion, typed, hosted)
                is ConnectionProfile.SystemOnDevice,
        )
    }

    @Test
    fun disconnectReturnsToOriginOnlyWhenAShippedServerExists() {
        assertEquals(ConnectionMode.SPEECHKIT_ORIGIN, fallbackModeAfterDisconnect(hosted))
        assertEquals(ConnectionMode.ON_DEVICE, fallbackModeAfterDisconnect(null))
    }

    @Test
    fun missingModeNeverInfersCloud() {
        assertEquals(ConnectionMode.SELF_HOST, inferConnectionMode(typed, hosted))
        assertEquals(ConnectionMode.SPEECHKIT_ORIGIN, inferConnectionMode(null, hosted))
        assertEquals(ConnectionMode.ON_DEVICE, inferConnectionMode(null, null))
    }

    @Test
    fun cloudWithoutASessionStaysOnDeviceAndIgnoresTesterOrSelfHost() {
        assertTrue(
            resolveConnectionProfile(ConnectionMode.KOMBIFY_CLOUD, null, typed, hosted)
                is ConnectionProfile.SystemOnDevice,
        )
    }

    @Test
    fun aCompanionGatewayRootMapsOriginPathsOntoTheSpeechkitPerimeter() {
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
    fun anOriginSpeechKitUrlKeepsTheV1Prefix() {
        val origin = ConnectionProfile.Server("https://speechkit.kombify.io", "tester-token")
        assertEquals(
            "https://speechkit.kombify.io/v1/dictation/transcribe",
            origin.endpoint("/v1/dictation/transcribe"),
        )
    }

    @Test
    fun aBlankTestSurfaceKeepsTheResolvedOriginInsteadOfInventingASelfHost() {
        assertEquals(hosted, testSurfaceConnectProfile(hosted, "", ""))
        assertTrue(
            testSurfaceConnectProfile(ConnectionProfile.SystemOnDevice(), "", "")
                is ConnectionProfile.SystemOnDevice,
        )
    }

    @Test
    fun aMatchingUrlWithABlankTokenKeepsTheResolvedBearer() {
        assertEquals(
            hosted,
            testSurfaceConnectProfile(hosted, "https://speechkit.kombify.io/", ""),
        )
    }

    @Test
    fun describeNeverIncludesTheBearer() {
        val secret = "tester-token-secret"
        val described = ConnectionProfile.Server(
            "https://speechkit.kombify.io",
            secret,
        ).describe()
        assertTrue(described.contains("token=present"))
        assertTrue(!described.contains(secret))
        assertTrue(
            ConnectionProfile.Server("https://speechkit.kombify.io", null)
                .describe()
                .contains("token=absent"),
        )
    }

    @Test
    fun aDifferentTypedUrlIsAOneShotOverrideAndDoesNotDropAnExistingTokenWhenBlank() {
        val override = testSurfaceConnectProfile(hosted, "http://192.168.1.20:8080", "")
        assertEquals(
            ConnectionProfile.Server("http://192.168.1.20:8080", "tester-token"),
            override,
        )
        assertEquals(
            typed,
            testSurfaceConnectProfile(hosted, typed.baseUrl, "lan-token"),
        )
    }
}
