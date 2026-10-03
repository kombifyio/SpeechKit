package io.kombify.speechkit.audio

import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.channels.Channel

/** Bounded PCM handoff between a host's event loop and its playback coroutine.
 * Capacity includes the chunk currently being played. [offer] never blocks control
 * handling and returns false on overflow; the host must surface a failure or stop.
 * [clear] invalidates queued chunks; the host also calls its player's flush to cut
 * the in-flight chunk. The host owns and cancels one [consume] coroutine per queue.
 */
class PcmPlaybackQueue(sampleRateHz: Int, maxBufferedMillis: Int = 2000) {
    init { require(sampleRateHz > 0 && maxBufferedMillis > 0) }
    private data class Frame(val generation: Long, val pcm: ByteArray)
    private val lock = Any()
    private val frames = Channel<Frame>(64)
    private val maxBytes = sampleRateHz.toLong() * AudioFormat.BYTES_PER_SAMPLE * maxBufferedMillis / 1000
    private var pendingBytes = 0L
    private var generation = 0L
    private var closed = false
    private var activePlayback: Job? = null

    fun offer(pcm: ByteArray): Boolean = synchronized(lock) {
        if (closed || pcm.isEmpty() || pcm.size % AudioFormat.BYTES_PER_SAMPLE != 0 ||
            pcm.size > maxBytes - pendingBytes) return@synchronized false
        pendingBytes += pcm.size
        if (frames.trySend(Frame(generation, pcm.copyOf())).isSuccess) true else {
            pendingBytes -= pcm.size
            false
        }
    }

    suspend fun consume(play: suspend (ByteArray) -> Unit) = coroutineScope {
        for (frame in frames) {
            val playback = synchronized(lock) {
                if (frame.generation != generation || closed) null else {
                    // Register before dispatch: clear can cancel a handoff that has not
                    // reached the player yet, without canceling this consumer.
                    launch(start = CoroutineStart.LAZY) { play(frame.pcm) }
                        .also { activePlayback = it }
                }
            } ?: continue
            try {
                playback.start()
                playback.join()
            } finally {
                synchronized(lock) {
                    if (activePlayback === playback) activePlayback = null
                    if (frame.generation == generation) pendingBytes -= frame.pcm.size
                }
            }
        }
    }

    fun clear() = synchronized(lock) {
        generation++
        activePlayback?.cancel()
        activePlayback = null
        pendingBytes = 0
        while (frames.tryReceive().isSuccess) { /* discard the interrupted reply */ }
    }

    fun close() = synchronized(lock) {
        closed = true
        clear()
        frames.close()
        Unit
    }
}
