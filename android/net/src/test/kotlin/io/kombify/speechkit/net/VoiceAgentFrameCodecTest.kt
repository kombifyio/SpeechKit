package io.kombify.speechkit.net

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * Consumer drift-check for the Voice Agent wire contract
 * (docs/server/asyncapi.v1.yaml, mirrored by the TypeScript client's
 * protocol.ts). These assert the exact field names on the wire: a rename on
 * either side must fail here rather than silently produce a session that
 * connects and then says nothing.
 */
class VoiceAgentFrameCodecTest {

    private val codec = VoiceAgentCodec()

    @Test
    fun `start frame uses the snake_case wire names`() {
        val json = codec.encodeStart(
            VoiceAgentStartFrame(
                personaId = "brainstorm",
                sequenceId = "seq-1",
                provider = "deepgram",
                mediaTransport = "websocket",
                locale = "de-DE",
                thinking = "low",
            ),
        )

        assertTrue(json.contains("\"type\":\"start\""), json)
        assertTrue(json.contains("\"persona_id\":\"brainstorm\""), json)
        assertTrue(json.contains("\"sequence_id\":\"seq-1\""), json)
        assertTrue(json.contains("\"media_transport\":\"websocket\""), json)
        assertTrue(json.contains("\"provider\":\"deepgram\""))
        // Unset options must be omitted, not sent as null: the server falls
        // back to its configured defaults only for absent fields.
        assertFalse(json.contains("\"voice\""))
        assertFalse(json.contains("\"model\""))
    }

    @Test
    fun `control frames carry only their type`() {
        assertEquals("""{"type":"audio_end"}""", codec.encodeControl(VoiceAgentMsg.AUDIO_END))
        assertEquals("""{"type":"ping"}""", codec.encodeControl(VoiceAgentMsg.PING))
        assertEquals("""{"type":"stop"}""", codec.encodeControl(VoiceAgentMsg.STOP))
    }

    @Test
    fun `decodes every server frame the contract defines`() {
        assertEquals(
            VoiceAgentStates.LISTENING,
            (codec.decodeServerFrame("""{"type":"state","state":"listening"}""")
                as VoiceAgentStateFrame).state,
        )

        val input = codec.decodeServerFrame(
            """{"type":"input_transcript","text":"hallo","done":false}""",
        ) as VoiceAgentTranscriptFrame
        assertTrue(input.isInput)
        assertEquals("hallo", input.text)

        val output = codec.decodeServerFrame(
            """{"type":"output_transcript","text":"hi","done":true}""",
        ) as VoiceAgentTranscriptFrame
        assertFalse(output.isInput)
        assertTrue(output.done)

        val toolCall = codec.decodeServerFrame(
            """{"type":"tool_call","id":"t1","name":"weather","args":{"city":"Berlin"}}""",
        ) as VoiceAgentToolCallFrame
        assertEquals("t1", toolCall.id)
        assertEquals("Berlin", toolCall.args?.get("city"))

        val error = codec.decodeServerFrame(
            """{"type":"error","code":"provider_unavailable","message":"nope","remediation":"configure"}""",
        ) as VoiceAgentErrorFrame
        assertEquals("provider_unavailable", error.code)
        assertEquals("configure", error.remediation)

        assertEquals(
            VoiceAgentEndReasons.IDLE,
            (codec.decodeServerFrame("""{"type":"session_end","reason":"idle"}""")
                as VoiceAgentSessionEndFrame).reason,
        )

        assertTrue(
            codec.decodeServerFrame("""{"type":"interrupted"}""")
                is VoiceAgentInterruptedFrame,
        )
        assertTrue(codec.decodeServerFrame("""{"type":"pong"}""") is VoiceAgentPongFrame)
    }

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
