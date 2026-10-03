package io.kombify.speechkit.audio

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

@OptIn(ExperimentalCoroutinesApi::class)
class PcmStreamPlayerTest {
    private class Track : PcmPlaybackTrack {
        val played = ArrayList<Byte>()
        var released = false
        var write: (Int) -> Int = { it }
        var failStop = false
        override fun write(pcm: ByteArray, offset: Int, size: Int): Int {
            val accepted = write(size)
            if (accepted > 0) played.addAll(pcm.copyOfRange(offset, offset + accepted).toList())
            return accepted
        }
        override fun stop() { if (failStop) error("private driver detail") }
        override fun release() { released = true }
    }

    @Test
    fun `partial writes and temporary backpressure preserve every sample`() = runTest {
        val track = Track()
        var first = true
        var pause = true
        track.write = { size ->
            when {
                first -> { first = false; 2 }
                pause -> { pause = false; 0 }
                else -> size
            }
        }
        val player = PcmStreamPlayer(24000, StandardTestDispatcher(testScheduler)) { track }
        val pcm = byteArrayOf(1, 2, 3, 4, 5, 6)
        player.play(pcm)
        assertArrayEquals(pcm, track.played.toByteArray())
        player.release()
    }

    @Test
    fun `flush and release stop stalled and waiting writes before another reply`() = runTest {
        val old = Track().apply { write = { 0 } }
        val fresh = Track()
        var target = old
        val player = PcmStreamPlayer(24000, StandardTestDispatcher(testScheduler)) { target }
        val inFlight = launch { player.play(byteArrayOf(1, 2)) }
        val waiting = launch { player.play(byteArrayOf(3, 4)) }
        runCurrent()
        player.flush()
        assertTrue(old.released)
        advanceUntilIdle()
        inFlight.join(); waiting.join()
        assertTrue(old.played.isEmpty())
        target = fresh
        fresh.write = { 0 }
        val another = launch { player.play(byteArrayOf(5, 6)) }
        runCurrent()
        player.release()
        assertTrue(fresh.released)
        advanceUntilIdle()
        another.join()
        assertTrue(fresh.played.isEmpty())
    }

    @Test
    fun `negative writes report safe failure and release even when stop fails`() = runTest {
        val track = Track().apply { write = { -6 }; failStop = true }
        val player = PcmStreamPlayer(24000, StandardTestDispatcher(testScheduler)) { track }
        val failure = async { runCatching { player.play(byteArrayOf(1, 2)) }.exceptionOrNull() }.await()
        assertTrue(failure is PcmPlaybackException)
        assertTrue(track.released)
        assertTrue(track.played.isEmpty())
    }
}
