package io.kombify.speechkit.net

import com.squareup.moshi.Moshi
import com.squareup.moshi.Types
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * Consumer drift-check for the Voice Agent wire contract: replays the golden
 * frames from docs/server/fixtures/voiceagent.v1.json (the same fixture the Go
 * producer test in internal/server/voiceagent/protocol_fixture_test.go
 * marshals against) through the Kotlin codec. A rename on either side must
 * fail here rather than silently produce a session that connects and then
 * says nothing.
 */
class VoiceAgentFrameCodecTest {

    private val codec = VoiceAgentCodec()
    private val moshi = Moshi.Builder().build()
    private val mapAdapter = moshi.adapter<Map<String, Any?>>(
        Types.newParameterizedType(Map::class.java, String::class.java, Any::class.java),
    )

    /** Fixture frame name → canonical JSON string. */
    private val frames: Map<String, String> by lazy {
        val root = mapAdapter.fromJson(fixtureFile().readText())
            ?: error("fixture parse failed")
        @Suppress("UNCHECKED_CAST")
        val frameMap = root["frames"] as? Map<String, Any?>
            ?: error("fixture has no frames object")
        frameMap.mapValues { (_, value) ->
            @Suppress("UNCHECKED_CAST")
            mapAdapter.toJson(value as Map<String, Any?>)
        }
    }

    private fun fixtureFile(): File {
        var dir: File? = File(System.getProperty("user.dir") ?: ".").absoluteFile
        repeat(6) {
            val candidate = File(dir, "docs/server/fixtures/voiceagent.v1.json")
            if (candidate.isFile) return candidate
            dir = dir?.parentFile ?: return@repeat
        }
        error("voiceagent.v1.json not found walking up from ${System.getProperty("user.dir")}")
    }

    private fun assertTreeEquals(fixtureName: String, encoded: String) {
        assertEquals(
            mapAdapter.fromJson(frames.getValue(fixtureName)),
            mapAdapter.fromJson(encoded),
            "frame $fixtureName",
        )
    }

    // ── client → server: encode must match the golden frames ────────────────

    @Test
    fun `start frame encodes to the fixture shape`() {
        assertTreeEquals(
            "start",
            codec.encodeStart(
                VoiceAgentStartFrame(
                    personaId = "brainstorm",
                    sequenceId = "seq-1",
                    provider = "deepgram",
                    mediaTransport = "websocket",
                    locale = "de-DE",
                    thinking = "low",
                ),
            ),
        )
    }

    @Test
    fun `start frame omits unset options instead of sending null`() {
        val json = codec.encodeStart(VoiceAgentStartFrame(personaId = "brainstorm"))
        // The server falls back to its configured defaults only for absent
        // fields, so unset options must be omitted, not sent as null.
        assertFalse(json.contains("\"voice\""), json)
        assertFalse(json.contains("\"model\""), json)
        assertFalse(json.contains("null"), json)
    }

    @Test
    fun `text frame encodes to the fixture shape`() {
        assertTreeEquals("text", codec.encodeText("Wie ist das Wetter in Berlin?"))
    }

    @Test
    fun `tool_response encodes to the fixture shape`() {
        assertTreeEquals(
            "tool_response",
            codec.encodeToolResponse(
                VoiceAgentToolResponseFrame(
                    id = "t1",
                    name = "weather",
                    response = mapOf("city" to "Berlin", "temperature_c" to 21.5),
                ),
            ),
        )
    }

    @Test
    fun `control frames encode to the fixture shape`() {
        for (name in listOf("audio_end", "ping", "stop")) {
            assertTreeEquals(name, codec.encodeControl(name))
        }
    }

    // ── server → client: the golden frames must decode ──────────────────────

    @Test
    fun `state frames decode`() {
        val ready = codec.decodeServerFrame(frames.getValue("state_session_ready"))
            as VoiceAgentStateFrame
        assertEquals(VoiceAgentStates.LISTENING, ready.state)
        assertEquals("session_ready", ready.eventType)

        val speaking = codec.decodeServerFrame(frames.getValue("state")) as VoiceAgentStateFrame
        assertEquals(VoiceAgentStates.SPEAKING, speaking.state)
    }

    @Test
    fun `transcript frames decode for both directions`() {
        val partial = codec.decodeServerFrame(frames.getValue("input_transcript_partial"))
            as VoiceAgentTranscriptFrame
        assertTrue(partial.isInput)
        assertEquals("wie ist das", partial.text)
        assertFalse(partial.done)

        val final = codec.decodeServerFrame(frames.getValue("input_transcript_final"))
            as VoiceAgentTranscriptFrame
        assertTrue(final.isInput)
        assertTrue(final.done)
        assertEquals("Wie ist das Wetter in Berlin?", final.text)

        // Speaker-attribution fields are additive server-side extras the
        // Kotlin frame does not yet surface; the frame must still decode.
        val attributed = codec.decodeServerFrame(frames.getValue("input_transcript_speaker"))
            as VoiceAgentTranscriptFrame
        assertTrue(attributed.done)

        val output = codec.decodeServerFrame(frames.getValue("output_transcript"))
            as VoiceAgentTranscriptFrame
        assertFalse(output.isInput)
        assertTrue(output.done)
        assertEquals("In Berlin sind es 21 Grad und sonnig.", output.text)
    }

    @Test
    fun `tool_call decodes`() {
        val toolCall = codec.decodeServerFrame(frames.getValue("tool_call"))
            as VoiceAgentToolCallFrame
        assertEquals("t1", toolCall.id)
        assertEquals("weather", toolCall.name)
        assertEquals("Berlin", toolCall.args?.get("city"))
    }

    @Test
    fun `sequence_step decodes`() {
        val step = codec.decodeServerFrame(frames.getValue("sequence_step"))
            as VoiceAgentSequenceStepFrame
        assertEquals("seq-1", step.sequenceId)
        assertEquals("step-2", step.stepId)
        assertEquals(2, step.stepIndex)
        assertEquals("entered", step.status)
    }

    @Test
    fun `event and interrupted decode`() {
        val event = codec.decodeServerFrame(frames.getValue("event")) as VoiceAgentEventFrame
        assertEquals("turn_end", event.eventType)
        assertTrue(
            codec.decodeServerFrame(frames.getValue("interrupted"))
                is VoiceAgentInterruptedFrame,
        )
    }

    @Test
    fun `error frame decodes with stable code`() {
        val error = codec.decodeServerFrame(frames.getValue("error")) as VoiceAgentErrorFrame
        assertEquals(VoiceAgentErrorCodes.PROVIDER_UNAVAILABLE, error.code)
        assertTrue(error.message.isNotEmpty())
    }

    @Test
    fun `error frame accepts additive remediation fields`() {
        // Not in the golden fixture: the Go producer does not emit these yet,
        // but the client models them additively (asyncapi.v1.yaml).
        val error = codec.decodeServerFrame(
            """{"type":"error","code":"provider_unavailable","message":"nope","remediation":"configure"}""",
        ) as VoiceAgentErrorFrame
        assertEquals("configure", error.remediation)
    }

    @Test
    fun `session_end decodes`() {
        assertEquals(
            VoiceAgentEndReasons.IDLE,
            (codec.decodeServerFrame(frames.getValue("session_end"))
                as VoiceAgentSessionEndFrame).reason,
        )
    }

    @Test
    fun `pong decodes`() {
        assertTrue(codec.decodeServerFrame(frames.getValue("pong")) is VoiceAgentPongFrame)
    }

    // ── forward compatibility ───────────────────────────────────────────────

    @Test
    fun `unknown frame types stay forward-compatible`() {
        val frame = codec.decodeServerFrame("""{"type":"future_frame","payload":1}""")
        assertEquals("future_frame", (frame as VoiceAgentUnknownFrame).type)
    }

    @Test
    fun `malformed json degrades to unknown instead of throwing`() {
        assertTrue(codec.decodeServerFrame("not json") is VoiceAgentUnknownFrame)
    }

    // Providers differ: some stream the cumulative turn text, others deltas.
    // Rendering assumes cumulative, so the client normalises both.
    @Test
    fun `transcript accumulation handles cumulative and delta providers`() {
        assertEquals("hello", accumulateVoiceAgentTranscript("", "hello"))
        assertEquals("hello world", accumulateVoiceAgentTranscript("hello", "hello world"))
        assertEquals("hello world", accumulateVoiceAgentTranscript("hello ", "world"))
        assertEquals("hello", accumulateVoiceAgentTranscript("hello", "lo"))
        assertEquals("hello", accumulateVoiceAgentTranscript("hello", ""))
    }
}
