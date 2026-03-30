package io.kombify.speechkit.engine

import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.time.Instant

/**
 * Default Engine implementation with hook-based lifecycle.
 *
 * Mirrors: pkg/speechkit/runtime.go Runtime struct.
 * Go's buffered channel (cap 64) maps to MutableSharedFlow with replay/buffer.
 */
class Runtime(
    initial: Snapshot = Snapshot(),
    private val hooks: Hooks = Hooks(),
) : Engine {

    data class Hooks(
        val onStart: (suspend () -> Unit)? = null,
        val onStop: (suspend () -> Unit)? = null,
        val onCommand: (suspend (Command) -> Unit)? = null,
    )

    private val mutex = Mutex()
    private val _state = MutableStateFlow(initial)
    private val _events = MutableSharedFlow<Event>(extraBufferCapacity = 64)
    private var closed = false

    override val events: SharedFlow<Event> = _events.asSharedFlow()
    override val state: StateFlow<Snapshot> = _state.asStateFlow()
    override val commands: CommandBus = RuntimeCommandBus()

    override suspend fun start() {
        hooks.onStart?.invoke()
    }

    override suspend fun stop() {
        hooks.onStop?.invoke()
    }

    /** Thread-safe state update. Returns the new snapshot. */
    suspend fun updateState(update: (Snapshot) -> Snapshot): Snapshot = mutex.withLock {
        val newState = update(_state.value)
        _state.value = newState
        newState
    }

    /** Publish an event to all subscribers. Returns false if closed. */
    fun publish(event: Event): Boolean {
        if (closed) return false
        val timestamped = if (event.time == Instant.EPOCH) event.copy(time = Instant.now()) else event
        return _events.tryEmit(timestamped)
    }

    fun close() {
        closed = true
    }

    private inner class RuntimeCommandBus : CommandBus {
        override suspend fun dispatch(command: Command) {
            val handler = hooks.onCommand
                ?: throw IllegalStateException("speechkit: no command handler configured")
            handler(command)
        }
    }
}
