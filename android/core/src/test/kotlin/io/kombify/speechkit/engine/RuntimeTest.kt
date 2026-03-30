package io.kombify.speechkit.engine

import app.cash.turbine.test
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant

class RuntimeTest {

    @Test
    fun `initial state matches provided snapshot`() = runTest {
        val initial = Snapshot(status = "ready", activeMode = "dictate", transcriptions = 5)
        val runtime = Runtime(initial)

        assertEquals("ready", runtime.state.value.status)
        assertEquals("dictate", runtime.state.value.activeMode)
        assertEquals(5, runtime.state.value.transcriptions)
    }

    @Test
    fun `updateState modifies snapshot atomically`() = runTest {
        val runtime = Runtime()

        val result = runtime.updateState { it.copy(status = "recording", transcriptions = 1) }

        assertEquals("recording", result.status)
        assertEquals(1, result.transcriptions)
        assertEquals("recording", runtime.state.value.status)
    }

    @Test
    fun `publish emits events to flow`() = runTest {
        val runtime = Runtime()

        runtime.events.test {
            val event = Event(type = EventType.RECORDING_STARTED, message = "test")
            assertTrue(runtime.publish(event))

            val received = awaitItem()
            assertEquals(EventType.RECORDING_STARTED, received.type)
            assertEquals("test", received.message)

            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `publish sets timestamp if missing`() = runTest {
        val runtime = Runtime()

        runtime.events.test {
            val event = Event(type = EventType.STATE_CHANGED, time = Instant.EPOCH)
            runtime.publish(event)

            val received = awaitItem()
            assertTrue(received.time.isAfter(Instant.EPOCH))

            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `publish returns false after close`() = runTest {
        val runtime = Runtime()
        runtime.close()

        val event = Event(type = EventType.STATE_CHANGED)
        assertFalse(runtime.publish(event))
    }

    @Test
    fun `commands dispatch calls hook`() = runTest {
        var dispatched: Command? = null

        val runtime = Runtime(
            hooks = Runtime.Hooks(
                onCommand = { cmd -> dispatched = cmd },
            ),
        )

        val cmd = Command(type = CommandType.START_DICTATION, text = "hello")
        runtime.commands.dispatch(cmd)

        assertEquals(CommandType.START_DICTATION, dispatched?.type)
        assertEquals("hello", dispatched?.text)
    }

    @Test
    fun `commands dispatch throws without handler`() = runTest {
        val runtime = Runtime()

        try {
            runtime.commands.dispatch(Command(type = CommandType.START_DICTATION))
            throw AssertionError("Expected exception")
        } catch (e: IllegalStateException) {
            assertTrue(e.message?.contains("no command handler") == true)
        }
    }

    @Test
    fun `start and stop call hooks`() = runTest {
        var started = false
        var stopped = false

        val runtime = Runtime(
            hooks = Runtime.Hooks(
                onStart = { started = true },
                onStop = { stopped = true },
            ),
        )

        runtime.start()
        assertTrue(started)

        runtime.stop()
        assertTrue(stopped)
    }

    @Test
    fun `snapshot is immutable copy`() = runTest {
        val runtime = Runtime(Snapshot(providers = listOf("local", "cloud")))

        val snap1 = runtime.state.value
        runtime.updateState { it.copy(providers = listOf("local")) }
        val snap2 = runtime.state.value

        assertEquals(2, snap1.providers.size)
        assertEquals(1, snap2.providers.size)
    }
}
