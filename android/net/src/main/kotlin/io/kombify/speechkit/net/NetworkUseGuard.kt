package io.kombify.speechkit.net

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.joinAll
import kotlinx.coroutines.withContext
import java.io.Closeable

/** Owns active network transports and capture owners during a policy transition. */
object NetworkUseGuard {
    private val lock = Any()
    private var allowed = true
    private var initialized = false
    private var missingInitialRoute = false
    private val transports = mutableMapOf<Any, () -> Unit>()
    private val stoppers = mutableSetOf<() -> List<Job>>()

    fun initialize(enabled: Boolean) {
        synchronized(lock) {
            if (enabled && missingInitialRoute) allowed = true
            initialized = true
            missingInitialRoute = false
        }
        if (!enabled) {
            val (calls, _) = restrictNow()
            calls.forEach { it() }
        }
    }

    /** A route-less read fails closed at startup without overriding a live host's initialization. */
    fun initializeMissingRoute() {
        val callbacks = synchronized(lock) {
            if (initialized) emptyList() else {
                initialized = true
                missingInitialRoute = true
                allowed = false
                transports.values.toList().also { transports.clear() }
            }
        }
        callbacks.forEach { it() }
    }

    fun checkAllowed() = synchronized(lock) { check(allowed) { "Network destination is not permitted" } }

    fun <T : Any> create(factory: () -> T, cancel: (T) -> Unit): T = synchronized(lock) {
        check(allowed) { "Network destination is not permitted" }
        val resource = factory()
        transports[resource] = { cancel(resource) }
        resource
    }

    fun track(key: Any, cancel: () -> Unit): Boolean = synchronized(lock) {
        if (!allowed) { cancel(); false } else { transports[key] = cancel; true }
    }
    fun untrack(key: Any) { synchronized(lock) { transports.remove(key) } }
    fun listenIfAllowed(stop: () -> List<Job>): Closeable? = synchronized(lock) {
        if (!allowed) null else { stoppers.add(stop); Closeable { synchronized(lock) { stoppers.remove(stop) } } }
    }
    fun listen(stop: () -> List<Job>): Closeable {
        synchronized(lock) { stoppers.add(stop) }
        return Closeable { synchronized(lock) { stoppers.remove(stop) } }
    }

    private fun restrictNow(): Pair<List<() -> Unit>, List<() -> List<Job>>> = synchronized(lock) {
        initialized = true
        missingInitialRoute = false
        allowed = false
        val callbacks = transports.values.toList()
        transports.clear()
        callbacks to stoppers.toList()
    }

    /** Used while holding the selection lock so commit and admission open together. */
    internal fun enable() { synchronized(lock) { initialized = true; missingInitialRoute = false; allowed = true } }

    /** Block new admissions, cancel transports and mic owners, then await capture cleanup. */
    suspend fun setAllowed(enabled: Boolean) {
        if (enabled) { enable(); return }
        val (transportsToStop, listeners) = restrictNow()
        transportsToStop.forEach { it() }
        val jobs = withContext(Dispatchers.Main.immediate) { listeners.flatMap { it() }.distinct() }
        jobs.forEach { it.cancel() }
        jobs.joinAll()
    }
}
