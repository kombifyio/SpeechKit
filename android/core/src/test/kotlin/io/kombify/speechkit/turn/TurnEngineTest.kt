package io.kombify.speechkit.turn

import io.kombify.speechkit.audio.EchoControl
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
        val engine = TurnEngine(speechBargeIn = SpeechBargeIn.Enabled)
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

    /**
     * The default configuration, and the reason it is the default: whatever
     * the microphone hears while the agent is audible, no turn opens.
     *
     * The fixture is deliberately the easiest possible barge-in — clean, loud,
     * unmistakably speech-shaped audio, with a playback reference carrying no
     * envelope at all, so every heuristic below would wave it through. It still
     * must not open a turn, because with [SpeechBargeIn.Disabled] nothing
     * spoken can. Telling a person's voice from the agent's own voice coming
     * back through a loudspeaker is not a decision this engine can make
     * reliably on a device that does not cancel the loudspeaker, and getting it
     * wrong in the permissive direction costs the whole feature: a
     * self-sustaining loop of the agent answering its own fragments.
     */
    @Test
    fun `speech over the agent does not take a turn by default`() {
        val engine = TurnEngine()
        engine.playAgentAudio(millis = 3_000, sampleRateHz = DOWNLINK_SAMPLE_RATE)

        val events = engine.feed((101..108).map { speech(it) })

        assertTrue(events.starts().isEmpty(), "the agent was interrupted while it was speaking")
        assertTrue(
            events.filterIsInstance<TurnEvent.BargeInRejected>()
                .any { it.reason == BargeInRejection.Disabled },
            "the refusal should say the feature is off, not invent an acoustic reason",
        )
        assertTrue(engine.agentAudible, "the agent should still be speaking")
    }

    /**
     * The agent hearing itself: the loudspeaker reaches the microphone, the
     * engine takes the answer for a user turn, and the answer cuts itself off.
     *
     * Nothing about the level or the shape of that signal distinguishes it
     * from a person. It is loud, it is sustained, and it is amplitude-modulated
     * like speech because it *is* speech — so both the gate and
     * `isSpeechLike` pass it by construction. Only the audio the host is
     * playing can settle it, which is why the engine keeps that audio rather
     * than only measuring how long it lasts.
     */
    @Test
    fun `the agent's own voice returning through the microphone does not interrupt it`() {
        // The device advertises a canceller, so the engine is on the lenient
        // gate and the leakage clears it.
        val engine = TurnEngine(echo = EchoControl.PlatformAec, speechBargeIn = SpeechBargeIn.Enabled)
        val answer = syllables(millis = 3_000, rateHz = DOWNLINK_SAMPLE_RATE)
        engine.notePlaybackFrame(answer, DOWNLINK_SAMPLE_RATE)
        // Listening to the first part of an answer that is still playing.
        val events = engine.feedPcm(leakageOf(answer).firstMillis(1_200))

        assertTrue(events.starts().isEmpty(), "the agent interrupted itself with its own answer")
        assertTrue(
            events.filterIsInstance<TurnEvent.BargeInRejected>()
                .any { it.reason == BargeInRejection.Echo },
            "the agent's own voice should be refused as echo",
        )
        assertTrue(engine.agentAudible, "the agent should still be speaking")
    }

    /**
     * The other edge of the same knife. A fix that stops self-interruption by
     * making the agent uninterruptible is not a fix, so: the same answer
     * playing, the same leakage in the microphone, and a person talking over
     * both. The engine has to hear the one signal the loudspeaker cannot
     * explain.
     *
     * The person starts a second into the answer rather than with it, which is
     * both what people do and the harder case: the window the engine compares
     * reaches back into audio that *was* pure echo, so a comparison that
     * simply asks "has this microphone been tracking the speaker" says yes and
     * refuses a real interruption.
     */
    @Test
    fun `a person talking over the agent and its echo still interrupts it`() {
        // Unaided, so this clears the *strict* gate as well.
        val engine = TurnEngine(speechBargeIn = SpeechBargeIn.Enabled)
        val answer = syllables(millis = 3_000, rateHz = DOWNLINK_SAMPLE_RATE)
        engine.notePlaybackFrame(answer, DOWNLINK_SAMPLE_RATE)

        val echo = leakageOf(answer).firstMillis(2_000)
        // A different voice: slower syllables, closer to the microphone, and
        // arriving a second after the agent started talking.
        val person = silentFor(1_000) +
            syllables(millis = 1_000, rateHz = SAMPLE_RATE, periodWindows = 8)
        val events = engine.feedPcm(mix(echo, person))

        val start = events.starts().firstOrNull()
        assertTrue(start != null && start.bargeIn, "a person speaking over the agent was ignored")
    }

    /**
     * The gate follows what the capture chain reports when it opens, not what
     * the device advertises before it.
     *
     * On Android the canceller attaches to an `AudioRecord` session, so there
     * is nothing to report until capture starts and a host built earlier can
     * only guess. Here the guess is `PlatformAec` and the attach fails, which
     * is the case that has to end at the strict gate: the microphone is
     * hearing the room unaided.
     *
     * The agent plays silence so that only the gate can decide.
     */
    @Test
    fun `a canceller that does not attach selects the strict barge-in gate`() {
        // Loud enough for a microphone the platform is cleaning up, too quiet
        // for one that is hearing the loudspeaker unaided.
        val midBand = syllables(millis = 800, rateHz = SAMPLE_RATE, peak = MID_BAND_PEAK)

        val failed = TurnEngine(echo = EchoControl.PlatformAec, speechBargeIn = SpeechBargeIn.Enabled)
        failed.playAgentAudio(millis = 3_000, sampleRateHz = DOWNLINK_SAMPLE_RATE)
        failed.noteEchoControl(EchoControl.None) // the attach failed
        assertTrue(
            failed.feedPcm(midBand).starts().isEmpty(),
            "an unaided microphone cut the agent off at the assisted gate",
        )

        val attached = TurnEngine(echo = EchoControl.PlatformAec, speechBargeIn = SpeechBargeIn.Enabled)
        attached.playAgentAudio(millis = 3_000, sampleRateHz = DOWNLINK_SAMPLE_RATE)
        attached.noteEchoControl(EchoControl.PlatformAec)
        assertTrue(
            attached.feedPcm(midBand).starts().isNotEmpty(),
            "a cleaned-up microphone did not hear a barge-in at this level",
        )
    }

    /** With the switch on, speech over the agent is a real turn and arrives whole. */
    @Test
    fun `speech during playback interrupts the agent`() {
        val engine = TurnEngine(speechBargeIn = SpeechBargeIn.Enabled)
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

    /** Feeds a continuous stream the way a recorder delivers it: in chunks. */
    private fun TurnEngine.feedPcm(pcm: ByteArray): List<TurnEvent> {
        val events = ArrayList<TurnEvent>()
        var offset = 0
        while (offset < pcm.size) {
            val end = minOf(offset + FRAME_BYTES, pcm.size)
            events += offer(pcm.copyOfRange(offset, end))
            offset = end
        }
        return events
    }

    /**
     * Speech-like audio of [millis] at [rateHz]: loud for three of every
     * [periodWindows] 20 ms windows and near-silent for the rest, so the
     * envelope swings the way syllables do.
     *
     * [periodWindows] is the syllable rate, and it is what makes one voice a
     * different voice from another. The same rate rendered at the downlink
     * rate and again at the capture rate is one voice heard twice — which is
     * what a loudspeaker and a microphone do to the agent's own answer.
     */
    private fun syllables(
        millis: Int,
        rateHz: Int,
        peak: Int = SPEECH_PEAK,
        periodWindows: Int = 5,
    ): ByteArray {
        val windowSamples = rateHz * ENVELOPE_WINDOW_MILLIS / 1000
        val samples = millis * rateHz / 1000
        val out = ByteArray(samples * 2)
        for (i in 0 until samples) {
            val loud = (i / windowSamples) % periodWindows < 3
            val amplitude = if (loud) peak else SPEECH_TROUGH
            val value = if (i % 2 == 0) amplitude else -amplitude
            out[i * 2] = (value and 0xFF).toByte()
            out[i * 2 + 1] = ((value shr 8) and 0xFF).toByte()
        }
        return out
    }

    /**
     * What the loudspeaker hands back to the microphone: the same audio the
     * host played, resampled from the downlink rate to the capture rate and
     * attenuated — the two things a room does to it that a level test can see.
     *
     * Derived from the played bytes rather than regenerated, so the fixture
     * cannot drift away from the audio it is supposed to be an echo of.
     *
     * What it deliberately does not model: the loop delay, room reflections
     * and the loudspeaker's own response. Those are why this file cannot prove
     * that echo rejection works in a real room — only that the reference is
     * consulted at all.
     */
    private fun leakageOf(played: ByteArray, attenuation: Double = 0.5): ByteArray {
        val outSamples = (played.size / 2) * SAMPLE_RATE / DOWNLINK_SAMPLE_RATE
        val out = ByteArray(outSamples * 2)
        for (i in 0 until outSamples) {
            val source = (i * DOWNLINK_SAMPLE_RATE / SAMPLE_RATE) * 2
            val value = (played.sampleAt(source) * attenuation).toInt().coerceIn(-32_768, 32_767)
            out[i * 2] = (value and 0xFF).toByte()
            out[i * 2 + 1] = ((value shr 8) and 0xFF).toByte()
        }
        return out
    }

    /** Sums two capture-rate signals, the way a microphone sums two sources. */
    private fun mix(first: ByteArray, second: ByteArray): ByteArray {
        val out = ByteArray(maxOf(first.size, second.size))
        for (i in out.indices step 2) {
            val value = (first.sampleAt(i) + second.sampleAt(i)).coerceIn(-32_768, 32_767)
            out[i] = (value and 0xFF).toByte()
            out[i + 1] = ((value shr 8) and 0xFF).toByte()
        }
        return out
    }

    /** [millis] of capture-rate digital silence. */
    private fun silentFor(millis: Int): ByteArray =
        ByteArray(millis * SAMPLE_RATE / 1000 * BYTES_PER_SAMPLE)

    /** The first [millis] of capture-rate PCM. */
    private fun ByteArray.firstMillis(millis: Int): ByteArray =
        copyOf(millis * SAMPLE_RATE / 1000 * BYTES_PER_SAMPLE)

    private fun ByteArray.sampleAt(byteOffset: Int): Int =
        if (byteOffset + 1 >= size) {
            0
        } else {
            ((this[byteOffset].toInt() and 0xFF) or (this[byteOffset + 1].toInt() shl 8)).toShort().toInt()
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
        const val ENVELOPE_WINDOW_MILLIS = 20
        const val ENVELOPE_SAMPLES = SAMPLE_RATE * ENVELOPE_WINDOW_MILLIS / 1000

        /** ~0.060 and ~0.002 of full scale. */
        const val SPEECH_PEAK = 1_966
        const val SPEECH_TROUGH = 66

        /**
         * Roughly 0.020 of full scale once the duty cycle is accounted for:
         * over the level VAD's "definitely speech" point, which is the gate a
         * *claimed* canceller buys, and under the unaided gate. The band where
         * an honest [EchoControl] report is the whole difference.
         */
        const val MID_BAND_PEAK = 850

        /** Uniform noise whose RMS is ~0.050 of full scale. */
        const val NOISE_PEAK = 2_838
    }
}
