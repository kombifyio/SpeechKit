package io.kombify.speechkit.net

import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow

/** Bounded callback handoff; terminal delivery never competes with audio for capacity. */
internal class VoiceAgentEventQueue {
    private val lock = Any()
    private val pending = ArrayDeque<VoiceAgentEvent>()
    private val wake = Channel<Unit>(Channel.CONFLATED)
    private var audioBytes = 0
    private var controls = 0
    private var terminal: VoiceAgentEvent.Closed? = null
    private var ended = false
    private var awaitingInterruption = false
    private var terminalFailure: VoiceAgentEvent.Failure? = null

    val events: Flow<VoiceAgentEvent> = flow {
        while (true) {
            val next = synchronized(lock) {
                if (pending.isNotEmpty()) {
                    pending.removeFirst().also {
                        if (it is VoiceAgentEvent.Audio) audioBytes -= it.pcm.size else controls--
                    }
                } else terminalFailure?.also { terminalFailure = null }
                    ?: terminal?.also { terminal = null }
            }
            if (next != null) {
                emit(next)
                if (next is VoiceAgentEvent.Closed) break
            } else {
                if (synchronized(lock) { ended && pending.isEmpty() && terminal == null && terminalFailure == null }) break
                wake.receiveCatching()
            }
        }
    }

    /** False means the caller must cancel the transport; overflow is an explicit failure. */
    fun offer(event: VoiceAgentEvent): Boolean = synchronized(lock) {
        if (ended) return@synchronized false
        if (event is VoiceAgentEvent.Audio && awaitingInterruption) return@synchronized true
        if (event == VoiceAgentEvent.Interrupted) {
            awaitingInterruption = false
            discardAudio()
        }
        val overflow = if (event is VoiceAgentEvent.Audio) {
            event.pcm.size > MAX_AUDIO_BYTES - audioBytes || event.pcm.size % 2 != 0 ||
                pending.size >= MAX_EVENTS
        } else controls >= MAX_CONTROLS
        if (overflow) {
            finish(VoiceAgentEndReasons.ERROR, VoiceAgentEvent.Failure(
                VoiceAgentWsClient.OVERFLOW_CODE, "Voice stream capacity exceeded", fatal = true,
            ))
            return@synchronized false
        }
        pending.addLast(event)
        if (event is VoiceAgentEvent.Audio) audioBytes += event.pcm.size else controls++
        wake.trySend(Unit)
        true
    }

    /** Cut locally now; audio already in flight stays muted until the server acknowledges. */
    fun interrupt() = synchronized(lock) {
        offer(VoiceAgentEvent.Interrupted).also { if (it) awaitingInterruption = true }
    }

    fun finish(reason: String, failure: VoiceAgentEvent.Failure? = null) = synchronized(lock) {
        if (!ended) {
            ended = true
            discardAudio()
            terminalFailure = failure
            terminal = VoiceAgentEvent.Closed(reason)
            wake.close()
        }
    }

    private fun discardAudio() {
        pending.removeAll { it is VoiceAgentEvent.Audio }
        audioBytes = 0
    }

    companion object {
        // Two seconds of S16 mono at the server's 24 kHz rate; tiny frames are bounded too.
        const val MAX_AUDIO_BYTES = VoiceAgentAudio.SERVER_SAMPLE_RATE * 2 * 2
        private const val MAX_EVENTS = 128
        private const val MAX_CONTROLS = 64
    }
}
