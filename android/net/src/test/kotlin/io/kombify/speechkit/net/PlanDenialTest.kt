package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ConnectionProfile
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class PlanDenialTest {

    private val server = MockWebServer()

    @BeforeEach
    fun start() = server.start()

    @AfterEach
    fun stop() = server.shutdown()

    @Test
    fun `a gateway plan refusal reaches the caller as a plan denial with its guidance`() {
        server.enqueue(
            MockResponse()
                .setResponseCode(403)
                .setHeader("Content-Type", "application/json")
                .setBody(
                    """{"error":"speechkit_preflight_denied","error_code":"speechkit_feature_not_entitled",""" +
                        """"reason_code":"speechkit_feature_missing","missing_features":["speechkit.voiceagent.live"],""" +
                        """"required_features":["speechkit.voiceagent.live"],"retryable":false,""" +
                        """"user_guidance":{"title":"Voice Agent is not in your plan","body":"Upgrade to talk to the agent.",""" +
                        """"next_steps":["See plans"]},"request_id":"r1"}""",
                ),
        )
        val api = SpeechKitServerApi(ConnectionProfile.Server(server.url("/").toString(), "user-jwt"))

        val failure = assertThrows<SpeechKitApiException> { runBlocking { api.createVoiceAgentSession() } }

        assertEquals(403, failure.httpStatus)
        assertEquals("speechkit_feature_not_entitled", failure.code)
        assertNotNull(failure.denial)
        val denial = failure.denial!!
        assertEquals(PlanDenial.Kind.FEATURE_NOT_IN_PLAN, denial.kind)
        assertEquals(listOf("speechkit.voiceagent.live"), denial.missingFeatures)
        assertEquals("Voice Agent is not in your plan", denial.title)
        assertEquals("Upgrade to talk to the agent.", denial.body)
    }

    @Test
    fun `an exhausted voice budget is a budget denial and a plain refusal is not a plan denial`() {
        val exhausted = parseApiError("""{"error":"speechkit_preflight_denied"}""", 402)
        assertEquals(PlanDenial.Kind.BUDGET, exhausted.denial?.kind)

        assertNull(parseApiError("""{"error":{"code":"forbidden","message":"no"}}""", 403).denial)
        assertNull(parseApiError("not json", 500).denial)
    }
}
