package io.kombify.speechkit.turn

import io.kombify.speechkit.audio.AudioFormat
import io.kombify.speechkit.audio.EchoControl
import io.kombify.speechkit.audio.MicLevelMeter
import io.kombify.speechkit.audio.frameDurationMillis
import io.kombify.speechkit.audio.toPcm16Samples
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.vad.LevelVadDetector
import io.kombify.speechkit.vad.VadDetector
import kotlin.math.sqrt

/** Who decides that the user's turn is over. */
enum class TurnPolicy {
    /**
     * Default. Starts engine-endpointed, and permanently switches to
     * provider-endpointed for the session the first time
     * [TurnEngine.noteProviderTurnEnd] is called.
     *
     * This exists so a consumer never has to know which kind of session it
     * opened. A cascaded pipeline never sends a provider turn signal and stays
     * on client-side endpointing forever; a provider with native turn
     * detection (Deepgram Flux, gpt-realtime, Gemini Live) sends one on the
     * first turn and takes over. The consumer forwards the signal
     * unconditionally either way, and writes no branch.
     */
    Adaptive,

    /** Client-side endpointing on trailing silence, always. */
    EngineEndpointed,

    /**
     * The provider endpoints. Uplink is continuous — a provider running its
     * own VAD must hear everything, so nothing is withheld and pre-roll is
     * moot — and turns close only on [TurnEngine.noteProviderTurnEnd].
     */
    ProviderEndpointed,
}

/**
 * Whether speaking over the agent may cut it off.
 *
 * Off by default. Interrupting on *speech* requires telling the user's voice
 * from the agent's own voice arriving back through the microphone, and on a
 * device where the loudspeaker is not cancelled out of the capture signal that
 * distinction has no reliable answer. Getting it wrong in the permissive
 * direction is not a small error: the agent hears its own sentence, takes it
 * for an interruption, cuts itself off, answers the fragment and hears *that* —
 * a self-sustaining loop with no way in for the person holding the phone, which
 * is what a tester reported from a consuming app.
 *
 * So the feature is absent rather than tuned. A threshold that cannot be
 * calibrated without hardware is not a fix, and shipping a stricter one only
 * moves the failure. Everything that makes a turn correct once the agent is
 * quiet — onset detection, pre-roll, endpointing, provider handover — is
 * unaffected, and the explicit interrupt (host calls
 * [TurnEngine.notePlaybackStopped] after flushing the player) is untouched: it
 * is a deliberate act by a person and was never in doubt.
 *
 * @see Enabled for what re-enabling costs.
 */
enum class SpeechBargeIn {
    /**
     * Default. While the agent is audible, no microphone audio opens a turn.
     * The candidate is still tracked and refused as
     * [BargeInRejection.Disabled], so a device log still shows how often
     * someone tried to talk over the agent.
     */
    Disabled,

    /**
     * Speech over the agent may open a turn, subject to the loudness gate, the
     * speech-likeness test and the playback-reference comparison.
     *
     * Do not select this until a device session shows that the capture chain
     * actually removes the loudspeaker — that is, that [EchoControl.PlatformAec]
     * is reported *and* the microphone level during an answer stays at the
     * residual rather than at speech level. Without that evidence the envelope
     * comparison against the played audio is the only thing standing between an
     * answer and itself, and it has never been measured against a real room.
     *
     * On Android that evidence also requires the *host* to have put the session
     * into a voice-communication route: `MODE_IN_COMMUNICATION`, playback on
     * `USAGE_VOICE_COMMUNICATION`, and an output the person can hear. An
     * `AcousticEchoCanceler` attached to a capture session whose playback is on
     * the media stream reports itself enabled and subtracts nothing.
     */
    Enabled,
}

/**
 * Turn-taking for a hands-free spoken dialogue: onset detection, pre-roll,
 * endpointing, and barge-in, as one finished endpoint.
 *
 * ## Why this exists
 *
 * Every surface that wanted a duplex conversation used to assemble its own
 * scheme out of [VadDetector] and a capture flow, and each one got duplex
 * wrong differently. The failure that motivated this engine: a consumer
 * discarded microphone chunks while agent audio played, plus a fixed ~800 ms
 * tail afterwards. The user had to tap the screen to take every turn, and when
 * they spoke, the start and the end of the sentence were gone.
 *
 * So this class owns every timing decision and exposes none. There is no mute
 * window, no tail, and no threshold in the constructor. A consumer that uses
 * the API at all cannot reintroduce the defect:
 *
 * ```kotlin
 * val engine = TurnEngine()
 * capture.frames().collect { frame ->
 *     engine.noteEchoControl(capture.echoControl) // measured, so read per frame
 *     engine.offer(frame).forEach { event ->
 *         when (event) {
 *             is TurnEvent.TurnAudio -> agent.sendAudio(event.pcm)
 *             is TurnEvent.SpeechStarted -> if (event.bargeIn) { player.flush(); dropQueuedAgentAudio() }
 *             is TurnEvent.TurnEnded -> agent.endTurn()
 *             is TurnEvent.InputLevel -> orb.level = event.level
 *             is TurnEvent.BargeInRejected -> Unit
 *         }
 *     }
 * }
 * ```
 *
 * plus two lines on the playback side — `engine.notePlaybackFrame(pcm, rateHz)`
 * next to the player, and `engine.noteProviderTurnEnd()` where provider turn
 * signals arrive. That is the whole contract.
 *
 * ## What it does not do
 *
 * It never opens an audio device. Per the Android SDK surface boundary the
 * host owns `MicAudioCapture` and `PcmStreamPlayer`; the engine consumes
 * frames and emits decisions. It is synchronous and allocation-light: every
 * entry point returns the events that call caused, so there is no second flow
 * to collect and no concurrency to get wrong.
 *
 * Not thread-safe. Drive it from the single collector that reads the capture.
 *
 * @param echo what the capture chain already did about loudspeaker leakage.
 * @param policy who endpoints; the default works it out per session.
 * @param vad frame-level speech probability. [LevelVadDetector] is the Android
 *   default because it needs no model file on a fresh install; a host that has
 *   downloaded the Silero weights can pass `SileroVadDetector` with no other
 *   change, and the loudness and speech conditions below then become genuinely
 *   independent signals rather than two readings of the same envelope.
 * @param speechBargeIn whether speaking over the agent may cut it off. Off by
 *   default; see [SpeechBargeIn].
 */
class TurnEngine(
    echo: EchoControl = EchoControl.None,
    policy: TurnPolicy = TurnPolicy.Adaptive,
    private val vad: VadDetector = LevelVadDetector(),
    private val speechBargeIn: SpeechBargeIn = SpeechBargeIn.Disabled,
) {

    /**
     * What the capture chain reports *now*, not what it predicted before it
     * opened.
     *
     * A constructor argument alone cannot be honest here: a host is built
     * before its recorder exists, so at construction time the only answer
     * available is a device-capability guess. [noteEchoControl] is how the
     * measured answer arrives once the capture chain knows it.
     */
    private var echo: EchoControl = echo

    private val meter = MicLevelMeter()
    private val preRoll = PreRoll(PRE_ROLL_BYTES)

    /**
     * Envelope of the audio the host has recently handed to the loudspeaker,
     * and of what the microphone recently heard, on one shared 20 ms grid.
     *
     * These two are the only thing that can tell the agent's own voice from a
     * person's. See [echoLike].
     */
    private val playbackEnvelope = EnvelopeRing(REFERENCE_WINDOWS)
    private val captureEnvelope = EnvelopeRing(COMPARE_WINDOWS)

    private var providerEndpoints = policy == TurnPolicy.ProviderEndpointed
    private val adaptive = policy == TurnPolicy.Adaptive

    /** Continuous uplink belongs to provider-endpointed sessions only. */
    private val continuousUplink: Boolean get() = providerEndpoints

    private var turnOpen = false
    private var turnMillis = 0L
    private var silenceMillis = 0L

    private var candidateMillis = 0L
    private val candidateEnvelope = ArrayList<Float>(ENVELOPE_CAPACITY)
    private var loudHoldMillis = 0L
    private var suppressUntilQuiet = false

    /**
     * How much of the audio handed to the speaker has not been heard yet.
     *
     * This is the honest replacement for a mute-plus-tail window. It is not a
     * timer someone chose: it is exactly the duration of the PCM the host
     * passed to the player — measured at the rate that player runs at, which
     * is why [notePlaybackFrame] demands one — drained by the wall-clock the
     * microphone frames measure. The guard therefore ends when the agent's
     * audio ends, and the user speaking into the silence right after gets the
     * ordinary onset thresholds — which is what "must tap to speak again" was.
     */
    private var playbackRemainingMillis = 0L

    private var levelSinceEmitMillis = 0L

    /** True while the engine believes the loudspeaker is still audible. */
    val agentAudible: Boolean get() = playbackRemainingMillis > 0

    /** True while a user turn is open. */
    val isTurnOpen: Boolean get() = turnOpen

    /**
     * Feeds one microphone frame and returns every decision it caused.
     *
     * Frames are PCM 16-bit signed little-endian mono at
     * [AudioFormat.SAMPLE_RATE]; any frame length works.
     */
    fun offer(frame: ByteArray): List<TurnEvent> {
        if (frame.isEmpty()) return emptyList()
        val events = ArrayList<TurnEvent>(3)
        val frameMillis = frame.frameDurationMillis()
        val samples = frame.toPcm16Samples()

        // Take the playback reading before draining it: audio captured during
        // this frame overlapped the speaker.
        val duringPlayback = agentAudible
        playbackRemainingMillis = (playbackRemainingMillis - frameMillis).coerceAtLeast(0L)

        captureEnvelope.append(frame, AudioFormat.SAMPLE_RATE)

        val level = meter.accept(frame, frameMillis)
        levelSinceEmitMillis += frameMillis
        if (levelSinceEmitMillis >= LEVEL_INTERVAL_MILLIS) {
            levelSinceEmitMillis = 0
            events += TurnEvent.InputLevel(level)
        }

        val speech = vad.processFrame(samples) >= SPEECH_PROBABILITY
        val rms = rmsOf(samples, 0, samples.size)
        val gate = if (duringPlayback) bargeInRmsGate() else IDLE_RMS_GATE

        // Peak hold on the loudness condition, for the same reason the level
        // VAD carries a hangover: a consonant closure or an inter-word gap
        // must not reset an onset that is halfway confirmed. Held well below
        // the confirmation window so one loud frame can never reach it.
        loudHoldMillis = (loudHoldMillis - frameMillis).coerceAtLeast(0L)
        if (rms >= gate) loudHoldMillis = LOUD_HOLD_MILLIS
        val loud = loudHoldMillis > 0

        if (turnOpen) {
            turnMillis += frameMillis
            events += TurnEvent.TurnAudio(frame)
            if (speech) silenceMillis = 0 else silenceMillis += frameMillis
            if (!providerEndpoints && silenceMillis >= ENDPOINT_SILENCE_MILLIS) {
                events += closeTurn(TurnEndReason.Silence)
            } else if (turnMillis >= MAX_TURN_MILLIS) {
                events += closeTurn(TurnEndReason.MaxDuration)
            }
            return events
        }

        if (continuousUplink) {
            events += TurnEvent.TurnAudio(frame)
        } else {
            preRoll.write(frame)
        }

        if (suppressUntilQuiet) {
            if (!loud) suppressUntilQuiet = false
            return events
        }

        if (loud && speech) {
            candidateMillis += frameMillis
            appendEnvelope(samples)
            val needed = if (duringPlayback) BARGE_IN_CONFIRM_MILLIS else ONSET_CONFIRM_MILLIS
            if (candidateMillis >= needed) {
                // Both extra conditions are required only against the agent's
                // own voice. Idle, a false onset costs a short empty turn;
                // during playback it cuts the agent off mid-sentence, so that
                // is the side that has to be sure.
                val rejection = if (duringPlayback) bargeInRejection() else null
                when (rejection) {
                    null -> events += openTurn(bargeIn = duringPlayback)

                    BargeInRejection.NotSpeech -> {
                        events += TurnEvent.BargeInRejected(rejection)
                        VoiceLog.i(VoiceLog.AGENT, "barge-in rejected reason=not_speech")
                        resetCandidate()
                        suppressUntilQuiet = true
                    }

                    // Deliberately *not* suppressed until quiet. Both of these
                    // last for as long as the answer does — echo because the
                    // loudspeaker keeps running, [BargeInRejection.Disabled] by
                    // definition — so waiting for the microphone to go quiet
                    // would carry the refusal past the end of the agent's audio
                    // and make the *next* turn need a tap. That is the reported
                    // defect wearing the other mask. Each window is judged on
                    // its own instead.
                    else -> {
                        events += TurnEvent.BargeInRejected(rejection)
                        VoiceLog.i(VoiceLog.AGENT, "barge-in rejected reason=${rejection.logReason()}")
                        resetCandidate()
                    }
                }
            }
        } else if (candidateMillis > 0) {
            // Only meaningful where a barge-in could have been accepted. With
            // speech barge-in off there is no candidate to be too short for,
            // and latching `suppressUntilQuiet` off the agent's own echo would
            // hold the refusal into the silence after it.
            if (duringPlayback && speechBargeIn == SpeechBargeIn.Enabled) {
                events += TurnEvent.BargeInRejected(BargeInRejection.TooShort)
                VoiceLog.i(VoiceLog.AGENT, "barge-in rejected reason=too_short")
                suppressUntilQuiet = loud
            }
            resetCandidate()
        }
        return events
    }

    /**
     * Records PCM the host just handed to the loudspeaker.
     *
     * Call it next to the player, with the same bytes and the rate that player
     * will play them at. The engine needs to know how long the agent stays
     * audible; it must not reach for the playback device to find out.
     *
     * [sampleRateHz] has no default on purpose. Playback is the one variable
     * rate in this pipeline — the Voice Agent downlink is 24 kHz S16 mono while
     * capture is 16 kHz — so a default would be an invitation to omit it, and
     * omitting it is precisely how this guard once came to outlive the sound it
     * guards: 16 kHz arithmetic over 24 kHz bytes overstates the agent's
     * audible time by half, leaving the barge-in gate armed over ~2 s of
     * silence after a 4 s answer. That is the "I had to tap to speak again"
     * defect in miniature, and no consumer should be able to reach it by
     * writing less code.
     *
     * @param pcm the same PCM 16-bit signed little-endian mono bytes given to
     *   the player.
     * @param sampleRateHz the rate those bytes are played at, in Hz.
     */
    fun notePlaybackFrame(pcm: ByteArray, sampleRateHz: Int) {
        require(sampleRateHz > 0) { "playback sample rate must be positive, was $sampleRateHz" }
        if (pcm.isEmpty()) return
        playbackRemainingMillis += pcmDurationMillis(pcm.size, sampleRateHz)
        // Kept, not just measured: these bytes are the only description of the
        // agent's voice that exists on this side of the loudspeaker.
        playbackEnvelope.append(pcm, sampleRateHz)
    }

    /**
     * What the capture chain actually did about loudspeaker leakage, as
     * measured once it opened.
     *
     * Separate from the constructor because the honest answer does not exist
     * yet when a host is built: on Android the echo canceller attaches to an
     * `AudioRecord` session, and there is no session until capture starts. A
     * value chosen before that can only be a device-capability guess — and
     * `AcousticEchoCanceler.isAvailable()` is exactly that guess — while a
     * wrong guess of [EchoControl.PlatformAec] buys the *lenient* barge-in
     * gate on a microphone that is hearing the loudspeaker unaided. That is
     * how an agent comes to cut itself off with its own answer.
     *
     * Forward it whenever frames flow; repeats are ignored.
     */
    fun noteEchoControl(control: EchoControl) {
        if (control == echo) return
        echo = control
        VoiceLog.i(VoiceLog.AGENT, "echo control now $control gate=${bargeInRmsGate()}")
    }

    /**
     * Duration of PCM 16-bit mono at an arbitrary rate.
     *
     * Kept private rather than offered as a rate-taking sibling of
     * [io.kombify.speechkit.audio.frameDurationMillis]: the only variable-rate
     * audio this engine measures is playback, and the only way in is
     * [notePlaybackFrame], which cannot be called without stating the rate.
     * A public overload would restore the zero-argument call for playback
     * bytes and with it the bug.
     */
    private fun pcmDurationMillis(bytes: Int, sampleRateHz: Int): Long =
        (bytes.toLong() * 1000) / (sampleRateHz.toLong() * AudioFormat.BYTES_PER_SAMPLE)

    /** The host cut or drained playback: the loudspeaker is silent now. */
    fun notePlaybackStopped() {
        playbackRemainingMillis = 0
        // The queued audio will never be heard, so it cannot explain anything
        // the microphone picks up from here on.
        playbackEnvelope.clear()
    }

    /**
     * The provider's own turn detection reported the end of the user's turn.
     *
     * Forward this unconditionally wherever provider turn signals arrive. On a
     * cascaded session it never fires; on a provider with native turn
     * detection it both closes the turn and, under [TurnPolicy.Adaptive],
     * hands endpointing over for the rest of the session.
     */
    fun noteProviderTurnEnd(): List<TurnEvent> {
        if (adaptive && !providerEndpoints) {
            providerEndpoints = true
            VoiceLog.i(VoiceLog.AGENT, "turn endpointing handed to provider")
        }
        return if (turnOpen) listOf(closeTurn(TurnEndReason.Provider)) else emptyList()
    }

    /** The host ended the session or cancelled the turn in progress. */
    fun stop(): List<TurnEvent> {
        val events = if (turnOpen) listOf(closeTurn(TurnEndReason.Host)) else emptyList()
        notePlaybackStopped()
        return events
    }

    /** Clears all state for a new conversation. */
    fun reset() {
        turnOpen = false
        turnMillis = 0
        silenceMillis = 0
        playbackRemainingMillis = 0
        levelSinceEmitMillis = 0
        loudHoldMillis = 0
        suppressUntilQuiet = false
        resetCandidate()
        preRoll.clear()
        playbackEnvelope.clear()
        captureEnvelope.clear()
        meter.reset()
        vad.reset()
    }

    private fun openTurn(bargeIn: Boolean): List<TurnEvent> {
        turnOpen = true
        silenceMillis = 0
        resetCandidate()
        val events = ArrayList<TurnEvent>(2)
        events += TurnEvent.SpeechStarted(bargeIn)
        if (bargeIn) {
            // The host is required to cut playback on this event, so the
            // speaker stops being a hazard now rather than when the frames
            // that were queued would have finished.
            notePlaybackStopped()
        }
        if (continuousUplink) {
            turnMillis = 0
        } else {
            val lead = preRoll.drain()
            turnMillis = lead.frameDurationMillis()
            if (lead.isNotEmpty()) events += TurnEvent.TurnAudio(lead)
        }
        VoiceLog.i(
            VoiceLog.AGENT,
            "turn open bargeIn=$bargeIn preRollMs=$turnMillis echo=$echo",
        )
        return events
    }

    private fun closeTurn(reason: TurnEndReason): TurnEvent {
        turnOpen = false
        val captured = turnMillis
        turnMillis = 0
        silenceMillis = 0
        loudHoldMillis = 0
        preRoll.clear()
        vad.reset()
        VoiceLog.i(VoiceLog.AGENT, "turn end reason=$reason capturedMs=$captured")
        return TurnEvent.TurnEnded(reason, captured)
    }

    private fun resetCandidate() {
        candidateMillis = 0
        candidateEnvelope.clear()
    }

    private fun bargeInRmsGate(): Float = when (echo) {
        // The platform canceller leaves a residual far below speech, so the
        // gate is the level VAD's own "definitely speech" point.
        EchoControl.PlatformAec -> LevelVadDetector.DEFAULT_SPEECH_ABOVE

        // Without cancellation the loudspeaker occupies the ordinary speech
        // band the level VAD documents (raw 0.010-0.030 on a moderately gained
        // microphone), so the user's own voice — far closer to the microphone
        // than the speaker's reflection — has to clear the top of it.
        EchoControl.None -> UNAIDED_BARGE_IN_RMS_GATE
    }

    /**
     * Why this barge-in candidate must be refused, or `null` to let it through.
     *
     * [SpeechBargeIn.Disabled] is answered first and answers everything: with
     * the feature off there is no judgement to make, no heuristic runs, and no
     * spoken audio can cut the agent off. Below it, order matters only for the
     * diagnostic: a steady source is reported as such rather than as echo.
     */
    private fun bargeInRejection(): BargeInRejection? = when {
        speechBargeIn == SpeechBargeIn.Disabled -> BargeInRejection.Disabled
        !isSpeechLike() -> BargeInRejection.NotSpeech
        echoLike() -> BargeInRejection.Echo
        else -> null
    }

    private fun BargeInRejection.logReason(): String = when (this) {
        BargeInRejection.Disabled -> "disabled"
        BargeInRejection.Echo -> "echo"
        BargeInRejection.NotSpeech -> "not_speech"
        BargeInRejection.TooShort -> "too_short"
    }

    /**
     * Whether what the microphone just heard is the loudspeaker reproducing
     * the agent.
     *
     * The level gates cannot answer this and neither can [isSpeechLike]: a
     * speaker playing a voice *is* loud and *is* amplitude-modulated like a
     * voice. The one thing that separates the two is that the host knows
     * exactly what it is playing, so the question becomes "does the microphone
     * envelope move the way the audio I am playing moves".
     *
     * The loop delay is unknown — device output latency, the player's own
     * buffer and the air path all contribute, and they vary by device and by
     * volume — so the reference is searched rather than aligned. Every offset
     * in the retained playback history is tried and the best match wins, which
     * is why the history is a duration ([REFERENCE_WINDOWS]) rather than a
     * position derived from [playbackRemainingMillis]: an alignment model that
     * is wrong about the player's buffering would look at the wrong audio and
     * quietly never fire.
     *
     * Fails open, in three ways, all of which return "not echo":
     *
     * - too little history on either side to compare;
     * - a reference with no envelope movement (digital silence, or a steady
     *   tone), where a correlation would be meaningless — and where
     *   [isSpeechLike] has already had its say;
     * - no offset reaching [ECHO_CORRELATION_MIN].
     *
     * **Unreachable while [SpeechBargeIn.Disabled] is in force, which is the
     * default.** Failing open means an inconclusive comparison accepts the
     * barge-in, and on a device where the loudspeaker is not cancelled out of
     * the capture signal that is how an answer comes to interrupt itself.
     * Inverting it to fail closed was considered and rejected: it trades the
     * loop for an agent nobody can interrupt, and the threshold that would
     * separate the two cannot be chosen without measuring what a loudspeaker
     * leaks into a handset microphone. What is kept here is the machinery, so
     * that re-enabling is a switch rather than a rewrite.
     */
    private fun echoLike(): Boolean {
        val heard = captureEnvelope.latest(MIN_COMPARE_WINDOWS) ?: return false
        val played = playbackEnvelope.snapshot()
        if (played.size <= heard.size) return false
        if (!varies(heard) || !varies(played)) return false

        var best = 0f
        var offset = 0
        while (offset + heard.size <= played.size) {
            val match = correlation(heard, played, offset)
            if (match > best) best = match
            offset += 1
        }
        return best >= ECHO_CORRELATION_MIN
    }

    /**
     * Whether an envelope moves at all, by the same relative-spread measure
     * [isSpeechLike] uses. A flat reference carries no timing information, so
     * correlating against it would produce a number with no meaning.
     */
    private fun varies(envelope: FloatArray): Boolean {
        var sum = 0f
        for (value in envelope) sum += value
        val mean = sum / envelope.size
        if (mean <= 0f) return false
        var variance = 0f
        for (value in envelope) {
            val d = value - mean
            variance += d * d
        }
        return sqrt(variance / envelope.size) / mean >= MODULATION_MIN
    }

    /**
     * Pearson correlation of [heard] against the slice of [played] starting at
     * [offset]. Mean-removed and scale-free, because the coupling gain from
     * loudspeaker to microphone is unknown and varies with volume, distance
     * and device: only the *shape* of the two envelopes can be compared.
     */
    private fun correlation(heard: FloatArray, played: FloatArray, offset: Int): Float {
        var heardMean = 0f
        var playedMean = 0f
        for (i in heard.indices) {
            heardMean += heard[i]
            playedMean += played[offset + i]
        }
        heardMean /= heard.size
        playedMean /= heard.size

        var covariance = 0f
        var heardVariance = 0f
        var playedVariance = 0f
        for (i in heard.indices) {
            val a = heard[i] - heardMean
            val b = played[offset + i] - playedMean
            covariance += a * b
            heardVariance += a * a
            playedVariance += b * b
        }
        if (heardVariance <= 0f || playedVariance <= 0f) return 0f
        return covariance / sqrt(heardVariance * playedVariance)
    }

    /**
     * Envelope of the candidate, sampled short enough to resolve syllables.
     *
     * Speech is amplitude-modulated at roughly 3-8 Hz; steady sources are not.
     * 20 ms sub-frames put 15-20 samples inside a confirmation window, which
     * is enough for that modulation to show up as spread.
     */
    private fun appendEnvelope(samples: ShortArray) {
        var start = 0
        while (start < samples.size) {
            val end = minOf(start + ENVELOPE_WINDOW_SAMPLES, samples.size)
            if (candidateEnvelope.size < ENVELOPE_CAPACITY) {
                candidateEnvelope += rmsOf(samples, start, end)
            }
            start = end
        }
    }

    /**
     * Relative spread of the candidate's envelope.
     *
     * A 20 ms window at 16 kHz holds 320 samples, so the RMS of a stationary
     * source varies by only a few percent between windows; drifting real-world
     * noise stays well under 0.15. Speech, which goes near-silent between
     * syllables, runs far above the 0.30 line.
     */
    private fun isSpeechLike(): Boolean {
        if (candidateEnvelope.size < MIN_ENVELOPE_SAMPLES) return false
        var sum = 0f
        for (value in candidateEnvelope) sum += value
        val mean = sum / candidateEnvelope.size
        if (mean <= 0f) return false
        var variance = 0f
        for (value in candidateEnvelope) {
            val d = value - mean
            variance += d * d
        }
        val deviation = sqrt(variance / candidateEnvelope.size)
        return deviation / mean >= MODULATION_MIN
    }

    private fun rmsOf(samples: ShortArray, from: Int, to: Int): Float {
        val count = to - from
        if (count <= 0) return 0f
        var sumSquares = 0.0
        for (i in from until to) {
            val x = samples[i].toDouble()
            sumSquares += x * x
        }
        return (sqrt(sumSquares / count) / FULL_SCALE).toFloat()
    }

    /**
     * Fixed-capacity look-back over the capture stream.
     *
     * Bytes, not frames: the capture chunk size is the host's business and the
     * window has to be a duration.
     */
    private class PreRoll(private val capacity: Int) {
        private val buffer = ByteArray(capacity)
        private var head = 0
        private var size = 0

        fun write(frame: ByteArray) {
            if (frame.isEmpty()) return
            if (frame.size >= capacity) {
                frame.copyInto(buffer, 0, frame.size - capacity, frame.size)
                head = 0
                size = capacity
                return
            }
            var at = (head + size) % capacity
            for (byte in frame) {
                buffer[at] = byte
                at = if (at + 1 == capacity) 0 else at + 1
            }
            val filled = size + frame.size
            if (filled > capacity) {
                head = (head + (filled - capacity)) % capacity
                size = capacity
            } else {
                size = filled
            }
        }

        fun drain(): ByteArray {
            val out = ByteArray(size)
            for (i in 0 until size) out[i] = buffer[(head + i) % capacity]
            clear()
            return out
        }

        fun clear() {
            head = 0
            size = 0
        }
    }

    /**
     * Fixed-capacity history of RMS values on a [ENVELOPE_WINDOW_MILLIS] grid.
     *
     * A *duration* of sound reduced to its loudness contour, at whatever rate
     * that sound runs at: capture is 16 kHz and the agent downlink is 24 kHz,
     * and the whole point is to compare the two, so the window is expressed in
     * milliseconds and converted to samples per call. Samples that do not fill
     * a whole window are carried into the next call, so a window is always the
     * same duration no matter how the host chunks its audio.
     */
    private class EnvelopeRing(private val capacity: Int) {
        private val values = FloatArray(capacity)
        private var head = 0
        private var size = 0
        private var carrySumSquares = 0.0
        private var carrySamples = 0

        fun append(pcm: ByteArray, sampleRateHz: Int) {
            val windowSamples = sampleRateHz * ENVELOPE_WINDOW_MILLIS / 1000
            if (windowSamples <= 0) return
            val samples = pcm.size / 2
            for (i in 0 until samples) {
                val lo = pcm[i * 2].toInt() and 0xFF
                val hi = pcm[i * 2 + 1].toInt()
                val sample = ((hi shl 8) or lo).toShort().toDouble()
                carrySumSquares += sample * sample
                carrySamples += 1
                if (carrySamples >= windowSamples) {
                    push((sqrt(carrySumSquares / carrySamples) / FULL_SCALE).toFloat())
                    carrySumSquares = 0.0
                    carrySamples = 0
                }
            }
        }

        /**
         * The most recent windows, oldest first: as many as are held, capped
         * at [capacity], or null if fewer than [atLeast] exist yet.
         *
         * Adaptive rather than fixed because the first barge-in candidate of a
         * playback stretch confirms before a full comparison window of
         * microphone history has accumulated, and refusing to judge that one
         * would wave through the first — and loudest — self-interruption.
         */
        fun latest(atLeast: Int): FloatArray? {
            if (size < atLeast) return null
            val out = FloatArray(size)
            for (i in 0 until size) out[i] = values[(head + i) % capacity]
            return out
        }

        /** Everything retained, oldest first. */
        fun snapshot(): FloatArray {
            val out = FloatArray(size)
            for (i in 0 until size) out[i] = values[(head + i) % capacity]
            return out
        }

        fun clear() {
            head = 0
            size = 0
            carrySumSquares = 0.0
            carrySamples = 0
        }

        private fun push(value: Float) {
            values[(head + size) % capacity] = value
            if (size == capacity) head = (head + 1) % capacity else size += 1
        }
    }

    private companion object {
        const val FULL_SCALE = 32768.0

        /** Same speech verdict every other consumer in this repo compares against. */
        const val SPEECH_PROBABILITY = 0.5f

        /**
         * Loudness gate while the agent is quiet: the midpoint of the level
         * VAD's hysteresis band, i.e. exactly where that detector starts
         * calling a frame speech.
         */
        val IDLE_RMS_GATE =
            (LevelVadDetector.DEFAULT_SILENCE_BELOW + LevelVadDetector.DEFAULT_SPEECH_ABOVE) / 2f

        /**
         * Loudness gate for barge-in with no echo cancellation. Roughly 12 dB
         * over the idle gate — above the band the level VAD documents for
         * ordinary voice on a moderately gained microphone, which is also the
         * band loudspeaker leakage lands in. This is the one number that needs
         * device evidence to tune; everything else is derived.
         */
        const val UNAIDED_BARGE_IN_RMS_GATE = 0.030f

        /** Matches `VadConfig.minSpeechDurationMs`. */
        const val ONSET_CONFIRM_MILLIS = 250L

        /** Longer against the agent's own voice: barge-in is the costly error. */
        const val BARGE_IN_CONFIRM_MILLIS = 350L

        /**
         * Peak hold on the loudness condition. Long enough to bridge a
         * consonant closure, short enough that a single loud frame plus its
         * hold can never reach [ONSET_CONFIRM_MILLIS].
         */
        const val LOUD_HOLD_MILLIS = 150L

        /** Matches `VadConfig.minSilenceDurationMs`. */
        const val ENDPOINT_SILENCE_MILLIS = 700L

        /** Same safety cap the assistant's one-shot capture uses. */
        const val MAX_TURN_MILLIS = 30_000L

        /**
         * Look-back kept ahead of a confirmed onset.
         *
         * The sentence beginning is lost to two things: the confirmation
         * window itself (up to [BARGE_IN_CONFIRM_MILLIS] of speech is spent
         * proving the speech is real), and the unvoiced run-in before it — an
         * initial "s", "f" or "sh" sits under the loudness gate for another
         * 100-150 ms. Add one capture chunk of quantisation and the window is
         * 350 + 150 + 100 = 600 ms, of which at least 250 ms is genuine
         * pre-onset audio in the worst case. At 16 kHz S16 mono that is 19 200
         * bytes held per engine.
         */
        const val PRE_ROLL_MILLIS = 600
        const val PRE_ROLL_BYTES =
            PRE_ROLL_MILLIS * AudioFormat.SAMPLE_RATE * AudioFormat.BYTES_PER_SAMPLE / 1000

        /** Level publishing cadence, measured in audio time so it is deterministic. */
        const val LEVEL_INTERVAL_MILLIS = 50L

        const val ENVELOPE_WINDOW_MILLIS = 20
        const val ENVELOPE_WINDOW_SAMPLES = ENVELOPE_WINDOW_MILLIS * AudioFormat.SAMPLE_RATE / 1000
        const val ENVELOPE_CAPACITY = 64
        const val MIN_ENVELOPE_SAMPLES = 8
        const val MODULATION_MIN = 0.30f

        /**
         * How much microphone history the echo comparison judges at once.
         *
         * Long enough that two unrelated voices are very unlikely to agree
         * across it at any offset, short enough to stay inside one barge-in
         * decision. 600 ms is 30 windows, which is also comfortably more than
         * the [BARGE_IN_CONFIRM_MILLIS] the candidate itself spans.
         */
        const val COMPARE_MILLIS = 600
        const val COMPARE_WINDOWS = COMPARE_MILLIS / ENVELOPE_WINDOW_MILLIS

        /**
         * Least microphone history the comparison will judge on.
         *
         * Under [BARGE_IN_CONFIRM_MILLIS], so the first candidate after
         * playback starts is judged rather than waved through — that one is
         * the agent's own opening words, and letting it past is the reported
         * defect exactly.
         */
        const val MIN_COMPARE_MILLIS = 300
        const val MIN_COMPARE_WINDOWS = MIN_COMPARE_MILLIS / ENVELOPE_WINDOW_MILLIS

        /**
         * How much playback history the comparison searches.
         *
         * This is the loop-delay budget, and it is deliberately generous: it
         * has to cover the player's own buffering (a streaming PCM player
         * commonly holds a second of it) on top of device output latency and
         * the air path. A window too small does not fail loudly — it silently
         * compares against audio the microphone has not heard yet and never
         * matches. 3 s of a loudness contour is 150 floats.
         */
        const val REFERENCE_MILLIS = 3_000
        const val REFERENCE_WINDOWS = REFERENCE_MILLIS / ENVELOPE_WINDOW_MILLIS

        /**
         * How well the microphone must track the loudspeaker to count as the
         * agent hearing itself.
         *
         * Echo is a scaled, delayed copy, so it correlates near 1; a person
         * talking over the agent adds energy the reference cannot explain and
         * drops it sharply. Set high because the best of many offsets is taken
         * and that inflates chance agreement.
         *
         * Along with [UNAIDED_BARGE_IN_RMS_GATE] this is a judgement rather
         * than a derivation: what a loudspeaker actually leaks into a handset
         * microphone at speakerphone volume is device evidence, and this number
         * is the one to tune from a recorded session.
         */
        const val ECHO_CORRELATION_MIN = 0.80f
    }
}
