package io.kombify.speechkit.net

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test


class KombifyCloudAuthTest {

    private val server = MockWebServer()

    @BeforeEach
    fun start() = server.start()

    @AfterEach
    fun stop() = server.shutdown()

    private fun auth(): KombifyCloudAuth =
        KombifyCloudAuth(
            KombifyCloudAuth.Config(
                clientId = "speechkit-android",
                domain = server.url("/").toString().trimEnd('/'),
            ),
        )

    @Test
    fun startReturnsTheBrowserUrlWithoutACompanionBind() = runBlocking {
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody(
                    """{"device_code":"dev-1","user_code":"WDJB-MJHT","verification_uri":"https://login.kombify.io/activate","verification_uri_complete":"https://login.kombify.io/activate?user_code=WDJB-MJHT","expires_in":600,"interval":5}""",
                ),
        )
        val started = auth().start()
        assertEquals("dev-1", started.deviceCode)
        assertTrue(started.verificationUriComplete.contains("user_code=WDJB-MJHT"))
        val posted = server.takeRequest()
        assertEquals("/oauth/device/code", posted.path)
        assertTrue(posted.body.readUtf8().contains("client_id=speechkit-android"))
    }

    @Test
    fun pollPendingIsNotAFailure() = runBlocking {
        server.enqueue(
            MockResponse()
                .setResponseCode(403)
                .setHeader("Content-Type", "application/json")
                .setBody("""{"error":"authorization_pending"}"""),
        )
        assertEquals(KombifyCloudAuth.Poll.Pending, auth().poll("dev-1"))
    }

    @Test
    fun pollReadyYieldsTheGatewayBearer() = runBlocking {
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody("""{"access_token":"user-jwt","refresh_token":"rt","expires_in":3600}"""),
        )
        val ready = auth().poll("dev-1") as KombifyCloudAuth.Poll.Ready
        assertEquals("user-jwt", ready.tokens.accessToken)
        assertEquals("rt", ready.tokens.refreshToken)
    }
}
