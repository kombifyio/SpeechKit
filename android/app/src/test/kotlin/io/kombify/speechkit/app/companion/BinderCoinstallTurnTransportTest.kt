package io.kombify.speechkit.app.companion

import android.content.ServiceConnection
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

class BinderCoinstallTurnTransportTest {

    private class RecordingCallback : CoinstallTurnCallback {
        val errors = AtomicInteger(0)
        val last = AtomicReference<CoinstallTurnResult?>(null)
        val done = CountDownLatch(1)

        override fun onPartial(turnId: String, text: String) = Unit

        override fun onComplete(turnId: String, text: String) {
            last.set(CoinstallTurnResult.Complete(text))
            done.countDown()
        }

        override fun onError(turnId: String, code: Int, message: String) {
            errors.incrementAndGet()
            last.set(CoinstallTurnResult.Failed(code, message))
            done.countDown()
        }
    }

    @Test
    fun `a bind that never connects errors instead of hanging`() {
        val scheduler = Executors.newSingleThreadScheduledExecutor()
        try {
            val transport = BinderCoinstallTurnTransport(
                bind = { true },
                unbindService = {},
                bindTimeoutMs = 40,
                scheduler = scheduler,
            )
            val callback = RecordingCallback()
            transport.startTurn("turn-1", "hello", callback)
            assertTrue(callback.done.await(2, TimeUnit.SECONDS))
            val failed = callback.last.get() as CoinstallTurnResult.Failed
            assertEquals(COINSTALL_TURN_UNAVAILABLE, failed.code)
            assertEquals(1, callback.errors.get())
        } finally {
            scheduler.shutdownNow()
        }
    }

    @Test
    fun `a failed bind is unavailable without waiting for the timeout`() {
        val scheduler = Executors.newSingleThreadScheduledExecutor()
        try {
            val transport = BinderCoinstallTurnTransport(
                bind = { false },
                unbindService = {},
                bindTimeoutMs = 10_000,
                scheduler = scheduler,
            )
            val callback = RecordingCallback()
            transport.startTurn("turn-1", "hello", callback)
            assertTrue(callback.done.await(2, TimeUnit.SECONDS))
            val failed = callback.last.get() as CoinstallTurnResult.Failed
            assertEquals(COINSTALL_TURN_UNAVAILABLE, failed.code)
        } finally {
            scheduler.shutdownNow()
        }
    }

    @Test
    fun `disconnect before a terminal callback is unavailable`() {
        val scheduler = Executors.newSingleThreadScheduledExecutor()
        val held = AtomicReference<ServiceConnection?>(null)
        try {
            val transport = BinderCoinstallTurnTransport(
                bind = { conn ->
                    held.set(conn)
                    true
                },
                unbindService = {},
                bindTimeoutMs = 10_000,
                scheduler = scheduler,
            )
            val callback = RecordingCallback()
            transport.startTurn("turn-1", "hello", callback)
            held.get()!!.onServiceDisconnected(null)
            assertTrue(callback.done.await(2, TimeUnit.SECONDS))
            val failed = callback.last.get() as CoinstallTurnResult.Failed
            assertEquals(COINSTALL_TURN_UNAVAILABLE, failed.code)
        } finally {
            scheduler.shutdownNow()
        }
    }
}
