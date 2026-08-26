package io.kombify.speechkit.turn

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import kotlin.random.Random

/**
 * Black-box behaviour of the duplex turn engine, driven by synthetic PCM.
 *
 * These fixtures prove the decision logic, not acoustics. Real echo, real room
 * noise, and the level a loudspeaker actually leaks into a handset microphone
 * need a device; nothing here can stand in for that.
 *
 * The defect these guard against: a surface that discards microphone audio
 * while the agent speaks, so the user has to tap to take a turn and the start
 * of the sentence never reaches the provider.
 */
class TurnEngineTest {

    /**
     * A single loud frame is a door, not a turn. The confirmation window is
     * what separates the two, and collapsing it back to a level comparison is
     * what makes an agent interrupt itself on every noise.
     */
    @Test
    fun `one loud frame does not open a turn`() {
        val engine = TurnEngine()
        val events = engine.feed(
            silence(1), silence(2),
            noise(3),
            silence(4), silence(5), silence(6),
        )
        assertTrue(events.starts().isEmpty(), "a single loud frame opened a turn")
        assertFalse(engine.isTurnOpen)
    }

    /** Sustained speech is a turn. Counterpart of the frame above. */
    @Test
    fun `sustained speech opens a turn`() {
        val engine = TurnEngine()
        val events = engine.feed(silence(1), speech(101), speech(102), speech(103))
        assertEquals(1, events.starts().size)
        assertFalse(events.starts().single().bargeIn)
    }

    /**
     * The regression guard for the missing sentence beginning: audio captured
     * *before* the onset was confirmed has to be part of the captured turn.
     * Detecting speech costs a few hundred milliseconds of that speech, and
     * without a look-back buffer the provider only ever receives the middle of
     * the sentence.
     */
    @Test
    fun `the captured turn starts before the detected onset`() {
        val engine = TurnEngine()
        val lead = (1..6).map { silence(it) }
        val spoken = listOf(speech(101), speech(102), speech(103), speech(104))
        val events = engine.feed(lead + spoken)

        val markers = events.capturedPcm().markers()
        assertTrue(
            markers.contains(101),
            "the first frame of the sentence is missing from the turn: $markers",
        )
        assertTrue(
            markers.contains(6),
            "no audio from before the onset survived into the turn: $markers",
        )
        assertTrue(
            markers.indexOf(6) < markers.indexOf(101),
            "pre-onset audio must precede the sentence in the turn: $markers",
        )
    }

    /**
     * The owner's explicit requirement: ambient noise must not interrupt the
     * agent. Steady sources are loud enough and last long enough to pass a
     * level-plus-duration test, so the engine also has to look at whether the
     * envelope moves the way speech does.
     */
    @Test
    fun `steady noise during playback does not interrupt the agent`() {
        val engine = TurnEngine()
        engine.playAgentAudio(millis = 3_000, sampleRateHz = DOWNLINK_SAMPLE_RATE)
        val events = engine.feed((1..12).map { noise(it) })

        assertTrue(events.starts().isEmpty(), "ambient noise interrupted the agent")
        assertTrue(
            events.filterIsInstance<TurnEvent.BargeInRejected>()
                .any { it.reason == BargeInRejection.NotSpeech },
            "a steady source should be refused as not-speech",
        )
        assertTrue(engine.agentAudible, "the agent should still be speaking")
    }

    /** Speech over the agent is a real turn, and it arrives whole. */
    @Test
    fun `speech during playback interrupts the agent`() {
        val engine = TurnEngine()
        engine.playAgentAudio(millis = 3_000, sampleRateHz = DOWNLINK_SAMPLE_RATE)
        val events = engine.feed((101..106).map { speech(it) })

        val start = events.starts().singleOrNull()
        assertTrue(start != null && start.bargeIn, "speech over the agent did not barge in")
        assertFalse(engine.agentAudible, "accepted barge-in must disarm the echo guard")
        assertTrue(
            events.capturedPcm().markers().contains(101),
            "the sentence that interrupted the agent lost its first frame",
        )
    }

    /**
     * The symptom the tester reported as "I have to tap to speak again": a
     * consumer that mutes the microphone for a fixed tail after playback is
     * deaf exactly when a person naturally answers. The engine's guard ends
     * with the audio it was given, and the next turn needs no interaction.
     */
    @Test
    fun `a turn taken right after the agent stops needs no dead window`() {
        val engine = TurnEngine()
        engine.playAgentAudio(millis = 100, sampleRateHz = DOWNLINK_SAMPLE_RATE)
        val events = engine.feed(
            silence(1), // drains the agent's last 100 ms
            speech(101), speech(102), speech(103),
        )
        val start = events.starts().singleOrNull()
        assertTrue(start != null, "no turn was taken immediately after playback")
        assertFalse(start!!.bargeIn, "the agent had finished; this is an ordinary turn")
    }

    /**
     * The same defect one size down, and the reason the playback rate is a
     * required argument.
     *
     * The guard is a measurement, and what it measures is time — so it has to
     * be told the rate the bytes will be played at. The Voice Agent downlink is
     * 24 kHz while capture is 16 kHz, so measuring downlink bytes at the
     * capture rate stretches the guard by half: a 4 s answer would hold the
     * barge-in gate shut over 2 s of silence after the room went quiet. The
     * user gets the strict barge-in thresholds while nothing is playing, which
     * is "I had to tap to speak again" wearing a smaller hat.
     *
     * Pinning both edges is the point. A guard that ends early is an agent that
     * interrupts itself; a guard that ends late is a surface that cannot hear.
     */
    @Test
    fun `the playback guard lasts as long as the downlink audio really does`() {
        val engine = TurnEngine()
        engine.playAgentAudio(millis = 600, sampleRateHz = DOWNLINK_SAMPLE_RATE)

        engine.feed((1..5).map { silence(it) }) // 500 ms of the answer heard
        assertTrue(engine.agentAudible, "the guard released while the agent was still talking")

        engine.feed(silence(6)) // 600 ms: the answer is over
        assertFalse(engine.agentAudible, "the guard outlived the audio it guards")
    }

    /**
     * Endpointing closes the turn on trailing silence and survives the gaps
     * inside a sentence. A turn that ends on the first pause truncates the
     * end of what the user said — the other half of the reported defect.
     */
    @Test
    fun `endpointing closes the turn after trailing silence and not before`() {
        val engine = TurnEngine()
        val opening = engine.feed(speech(101), speech(102), speech(103))
        assertEquals(1, opening.starts().size)

        val midSentence = engine.feed(silence(1), silence(2), silence(3), speech(104), speech(105))
        assertTrue(midSentence.ends().isEmpty(), "a pause inside a sentence ended the turn")

        val trailing = engine.feed((10..24).map { silence(it) })
        val end = trailing.ends().singleOrNull()
        assertEquals(TurnEndReason.Silence, end?.reason)
        assertTrue(
            (midSentence + trailing).capturedPcm().markers().contains(105),
            "the last frame the user spoke is missing from the turn",
        )
    }

    /**
     * A provider with its own turn detection takes over the moment it reports
     * a turn end, and the consumer writes no branch to make that happen: it
     * forwards the signal, and after that client-side silence no longer closes
     * turns.
     */
    @Test
    fun `a provider turn signal takes endpointing over for the session`() {
        val engine = TurnEngine()
        engine.feed(speech(101), speech(102), speech(103))
        assertEquals(TurnEndReason.Provider, engine.noteProviderTurnEnd().ends().single().reason)

        engine.feed(speech(111), speech(112), speech(113))
        val trailing = engine.feed((10..30).map { silence(it) })
        assertTrue(
            trailing.ends().isEmpty(),
            "client-side silence closed a turn the provider owns",
        )
        assertEquals(TurnEndReason.Provider, engine.noteProviderTurnEnd().ends().single().reason)
    }

    // --- fixtures -----------------------------------------------------------

    private fun TurnEngine.feed(frames: List<ByteArray>): List<TurnEvent> =
        frames.flatMap { offer(it) }

    private fun TurnEngine.feed(vararg frames: ByteArray): List<TurnEvent> = feed(frames.toList())

    private fun List<TurnEvent>.starts() = filterIsInstance<TurnEvent.SpeechStarted>()

    private fun List<TurnEvent>.ends() = filterIsInstance<TurnEvent.TurnEnded>()

    private fun List<TurnEvent>.capturedPcm(): ByteArray {
        val out = filterIsInstance<TurnEvent.TurnAudio>().map { it.pcm }
        val total = out.sumOf { it.size }
        val joined = ByteArray(total)
        var at = 0
        for (chunk in out) {
            chunk.copyInto(joined, at)
            at += chunk.size
        }
        return joined
    }

    /**
     * Every fixture frame carries its identity in its first sample, so a test
     * can say which captured audio came from where without knowing anything
     * about how the engine stores it. One sample at this amplitude is four
     * orders of magnitude below the loudness gate.
     */
    private fun ByteArray.markers(): List<Int> {
        val out = ArrayList<Int>()
        var frame = 0
        while ((frame + 1) * FRAME_BYTES <= size) {
            val at = frame * FRAME_BYTES
            out += ((this[at].toInt() and 0xFF) or (this[at + 1].toInt() shl 8)).toShort().toInt()
            frame += 1
        }
        return out
    }

    /** 100 ms of digital silence. */
    private fun silence(marker: Int) = frame(marker) { 0 }

    /**
     * 100 ms of speech-like audio: loud for 60 ms, near-silent for 40 ms, so
     * the envelope swings the way a syllable does.
     */
    private fun speech(marker: Int) = frame(marker) { index ->
        val loud = (index / ENVELOPE_SAMPLES) % 5 < 3
        val amplitude = if (loud) SPEECH_PEAK else SPEECH_TROUGH
        if (index % 2 == 0) amplitude else -amplitude
    }

    /**
     * 100 ms of a steady source at speech level and above the barge-in gate —
     * a fan, a road, an air conditioner. Loud and sustained, but its envelope
     * does not move.
     */
    private fun noise(marker: Int): ByteArray {
        val random = Random(marker)
        return frame(marker) { random.nextInt(-NOISE_PEAK, NOISE_PEAK + 1) }
    }

    /**
     * Hands the engine agent audio the way a consumer does: the bytes the host
     * gave the player, and the rate that player runs at. Every call site states
     * the rate, because the engine's API leaves it no way not to.
     */
    private fun TurnEngine.playAgentAudio(millis: Int, sampleRateHz: Int) {
        val bytes = millis * sampleRateHz * BYTES_PER_SAMPLE / 1000
        notePlaybackFrame(ByteArray(bytes), sampleRateHz)
    }

    private fun frame(marker: Int, sample: (Int) -> Int): ByteArray {
        val out = ByteArray(FRAME_BYTES)
        for (i in 0 until SAMPLES_PER_FRAME) {
            val value = if (i == 0) marker else sample(i)
            out[i * 2] = (value and 0xFF).toByte()
            out[i * 2 + 1] = ((value shr 8) and 0xFF).toByte()
        }
        return out
    }

    private companion object {
        const val SAMPLE_RATE = 16_000
        const val BYTES_PER_SAMPLE = 2

        /**
         * The Voice Agent downlink, which is not the capture rate. Verified in
         * this repository at `internal/audio/stream_player.go`
         * (`voiceAgentOutputSampleRate`), `cmd/speechkit/voice_agent_echo_guard.go`
         * (`voiceAgentOutputBytesPerSecond`), and
         * `internal/server/voiceagent/livekit_bridge.go`
         * (`liveKitProviderOutputSampleRate`).
         */
        const val DOWNLINK_SAMPLE_RATE = 24_000

        const val FRAME_MILLIS = 100
        const val SAMPLES_PER_FRAME = SAMPLE_RATE * FRAME_MILLIS / 1000
        const val FRAME_BYTES = SAMPLES_PER_FRAME * 2

        /** 20 ms, the window the engine measures the envelope over. */
        const val ENVELOPE_SAMPLES = SAMPLE_RATE * 20 / 1000

        /** ~0.060 and ~0.002 of full scale. */
        const val SPEECH_PEAK = 1_966
        const val SPEECH_TROUGH = 66

        /** Uniform noise whose RMS is ~0.050 of full scale. */
        const val NOISE_PEAK = 2_838
    }
}
