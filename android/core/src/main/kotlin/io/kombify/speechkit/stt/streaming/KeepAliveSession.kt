package io.kombify.speechkit.stt.streaming

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.concurrent.atomic.AtomicLong

/**
 * Decorates a [StreamingSttSession] with the application-level keepalive its
 * transport needs to survive between mic presses.
 *
 * Why this exists: the server arms an idle watchdog when the WebSocket
 * attaches and ends the session if no application frame arrives inside the
 * window. A warm session held open across mic presses sends nothing between
 * them, so without this driver the socket dies while the user is simply
 * reading the screen.
 *
 * Sizing: [DEFAULT_INTERVAL_MS] is 30 s against a 120 s window — the tightest
 * deployed profile (`deploy/config/server.public-beta.toml`
 * `[server.dictation_stream] idle_timeout_sec`). The code default is 300 s
 * (`internal/config/defaults.go`), so 30 s clears both with room for a doze'd
 * or delayed coroutine to miss two ticks and still make the window.
 *
 * Debounced on activity: audio and control frames reset the server watchdog
 * too, so the driver only pings once the session has been quiet for a full
 * interval. Streaming audio therefore costs no ping traffic at all.
 *
 * The driver starts eagerly at construction because the server's watchdog
 * starts at attach, not at the first segment — the gap between connecting and
 * the first mic press is already on the clock.
 *
 * What this does NOT solve: a session still dies at the server's
 * `max_session_sec` wall clock (600 s on public beta) regardless of pinging,
 * and `max_per_identity_sessions` may be 1, so a held session blocks the next
 * mint. Consumers must still release on teardown and re-open on
 * [TranscriptEvent.Closed].
 */
class KeepAliveSession(
    private val delegate: StreamingSttSession,
    private val intervalMs: Long = DEFAULT_INTERVAL_MS,
    private val clockMs: () -> Long = { System.nanoTime() / 1_000_000L },
    parentScope: CoroutineScope? = null,
) : StreamingSttSession {

    private val scope = parentScope ?: CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val ownsScope = parentScope == null
    private val lastActivityMs = AtomicLong(clockMs())
    private val driver: Job

    init {
        driver = scope.launch {
            while (isActive) {
                val quietFor = clockMs() - lastActivityMs.get()
                if (quietFor < intervalMs) {
                    // Activity moved the deadline out; sleep the remainder.
                    delay(intervalMs - quietFor)
                    continue
                }
                if (!runCatching { delegate.keepAlive() }.getOrDefault(false)) return@launch
                markActivity()
                delay(intervalMs)
            }
        }
    }

    /**
     * Forwards the delegate's events and stops the driver once the session is
     * terminally closed. `onEach` does not consume the flow — the single
     * collector contract is unchanged.
     */
    override val events: Flow<TranscriptEvent> = delegate.events.onEach { event ->
        if (event is TranscriptEvent.Closed) stopDriver()
    }

    override val capturesOwnAudio: Boolean get() = delegate.capturesOwnAudio

    override suspend fun startSegment(options: DictationSegmentOptions) {
        markActivity()
        delegate.startSegment(options)
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        markActivity()
        delegate.sendAudio(pcm)
    }

    override suspend fun finishSegment() {
        markActivity()
        delegate.finishSegment()
    }

    override suspend fun keepAlive(): Boolean {
        markActivity()
        return delegate.keepAlive()
    }

    override suspend fun close() {
        stopDriver()
        delegate.close()
    }

    override fun toString(): String = "KeepAliveSession(${intervalMs}ms -> $delegate)"

    private fun markActivity() = lastActivityMs.set(clockMs())

    private fun stopDriver() {
        driver.cancel()
        // Only tear down a scope this instance created; a caller-supplied
        // scope belongs to the caller's lifecycle.
        if (ownsScope) scope.cancel()
    }

    companion object {
        /** 30 s — see the class KDoc for why this clears the 120 s window. */
        const val DEFAULT_INTERVAL_MS: Long = 30_000
    }
}
