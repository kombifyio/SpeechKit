package io.kombify.speechkit.net

import com.squareup.moshi.Moshi
import com.squareup.moshi.Types
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * Consumer drift-check: replays the golden frames from
 * docs/server/fixtures/dictation-stream.v1.json (the same fixtures the Go
 * producer test marshals against) through the Kotlin codec.
 */
class DictationFrameCodecTest {

    private val codec = DictationStreamCodec()
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
            val candidate = File(dir, "docs/server/fixtures/dictation-stream.v1.json")
            if (candidate.isFile) return candidate
            dir = dir?.parentFile ?: return@repeat
        }
        error("dictation-stream.v1.json not found walking up from ${System.getProperty("user.dir")}")
    }

    @Test
    fun `ready frame decodes`() {
        val frame = codec.decodeServerFrame(frames.getValue("ready"))
        assertEquals(StreamReadyFrame(segmentId = 1), frame)
    }

    @Test
    fun `draft transcript decodes`() {
        val frame = codec.decodeServerFrame(frames.getValue("transcript_draft")) as StreamTranscriptFrame
        assertEquals(1L, frame.segmentId)
        assertEquals(3L, frame.sequence)
        assertEquals("hallo wel", frame.text)
        assertEquals(false, frame.done)
        assertEquals("deepgram", frame.provider)
    }

    @Test
    fun `final transcript decodes with words`() {
        val frame = codec.decodeServerFrame(frames.getValue("transcript_final")) as StreamTranscriptFrame
        assertTrue(frame.done)
        assertEquals("Hallo Welt.", frame.text)
        assertEquals("de", frame.language)
        assertEquals(0.94, frame.confidence!!, 1e-9)
        val words = frame.words.orEmpty()
        assertEquals(2, words.size)
        assertEquals(StreamWord("Hallo", 0.97, 120, 480), words[0])
        assertEquals(StreamWord("Welt.", 0.91, 520, 900), words[1])
    }

    @Test
    fun `segment_done decodes`() {
        assertEquals(
            StreamSegmentDoneFrame(segmentId = 1),
            codec.decodeServerFrame(frames.getValue("segment_done")),
        )
    }

    @Test
    fun `error frame decodes with stable code`() {
        val frame = codec.decodeServerFrame(frames.getValue("error")) as StreamErrorFrame
        assertEquals(StreamErrorCodes.STREAMING_UNAVAILABLE, frame.code)
        assertTrue(frame.message.isNotEmpty())
    }

    @Test
    fun `session_end decodes`() {
        val frame = codec.decodeServerFrame(frames.getValue("session_end")) as StreamSessionEndFrame
        assertEquals(StreamEndReasons.CLIENT, frame.reason)
    }

    @Test
    fun `pong decodes`() {
        assertEquals(StreamPongFrame(), codec.decodeServerFrame(frames.getValue("pong")))
    }

    @Test
    fun `unknown frame type does not crash`() {
        val frame = codec.decodeServerFrame("""{"type":"totally_new_thing","x":1}""")
        assertEquals(UnknownFrame("totally_new_thing"), frame)
    }

    @Test
    fun `unknown json keys are ignored`() {
        val frame = codec.decodeServerFrame("""{"type":"ready","segment_id":7,"future_field":"x"}""")
        assertEquals(StreamReadyFrame(segmentId = 7), frame)
    }

    @Test
    fun `start frame round-trips byte-identically at the tree level`() {
        val fixtureJson = frames.getValue("start")
        val decoded = codec.decodeStart(fixtureJson) ?: error("start frame did not decode")
        val reencoded = codec.encodeStart(decoded)
        assertEquals(mapAdapter.fromJson(fixtureJson), mapAdapter.fromJson(reencoded))
    }

    @Test
    fun `control frames encode to the fixture shape`() {
        for (name in listOf("finalize", "stop", "ping")) {
            assertEquals(
                mapAdapter.fromJson(frames.getValue(name)),
                mapAdapter.fromJson(codec.encodeControl(name)),
                "control frame $name",
            )
        }
    }
}
