package io.kombify.speechkit.audio

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import kotlin.coroutines.CoroutineContext

@OptIn(ExperimentalCoroutinesApi::class)
class PcmPlaybackQueueTest {
    private class PausedDispatcher : CoroutineDispatcher() {
        private val dispatched = ArrayDeque<Runnable>()
        override fun dispatch(context: CoroutineContext, block: Runnable) { dispatched.addLast(block) }
        fun resume() { while (dispatched.isNotEmpty()) dispatched.removeFirst().run() }
    }

    @Test
    fun `clearing a handed-off frame cannot restart old speech after player flush`() = runTest {
        val queue = PcmPlaybackQueue(24000)
        val paused = PausedDispatcher()
        val heard = ArrayList<Byte>()
        val track = object : PcmPlaybackTrack {
            override fun write(pcm: ByteArray, offset: Int, size: Int): Int {
                heard.addAll(pcm.copyOfRange(offset, offset + size).toList())
                return size
            }
            override fun stop() = Unit
            override fun release() = Unit
        }
        val player = PcmStreamPlayer(24000, UnconfinedTestDispatcher(testScheduler)) { track }
        val consumer = launch { queue.consume { pcm -> withContext(paused) { player.play(pcm) } } }
        assertTrue(queue.offer(byteArrayOf(1, 2)))
        runCurrent() // Handoff is waiting to dispatch into the player.
        queue.clear()
        player.flush()
        val nextReply = byteArrayOf(3, 4)
        assertTrue(queue.offer(nextReply))
        paused.resume()
        runCurrent()
        paused.resume()
        runCurrent()
        assertArrayEquals(nextReply, heard.toByteArray())
        queue.close()
        player.release()
        consumer.join()
    }

    @Test
    fun `backpressure includes active playback and clearing drops only the old backlog`() = runTest {
        val queue = PcmPlaybackQueue(24000)
        val unblock = CompletableDeferred<Unit>()
        val heard = ArrayList<Byte>()
        val consumer = launch {
            queue.consume { pcm ->
                if (pcm.size > 2) unblock.await() else heard.addAll(pcm.toList())
            }
        }
        assertTrue(queue.offer(ByteArray(24000 * 2 * 2)))
        runCurrent()
        assertFalse(queue.offer(byteArrayOf(1, 2)))
        queue.clear()
        assertTrue(queue.offer(byteArrayOf(3, 4)))
        queue.clear()
        val nextReply = byteArrayOf(5, 6)
        assertTrue(queue.offer(nextReply))
        unblock.complete(Unit)
        runCurrent()
        assertArrayEquals(nextReply, heard.toByteArray())
        queue.close()
        consumer.join()
    }
}
