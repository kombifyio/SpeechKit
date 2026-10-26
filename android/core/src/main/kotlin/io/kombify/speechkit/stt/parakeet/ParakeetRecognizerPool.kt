package io.kombify.speechkit.stt.parakeet

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import io.kombify.speechkit.stt.phonon.PhononRecognizer
import java.io.File
import kotlin.time.Duration
import kotlin.time.Duration.Companion.minutes

/** Hands out the recognizer of a model directory for one dictation segment at a time. */
fun interface ParakeetRecognizers {
    /** Leases the recognizer of [files], starting its load in the background when needed. */
    suspend fun lease(files: OnDeviceModelFiles): ParakeetLease
}

/** One holder's claim on a loaded (or loading) recognizer; released exactly once. */
interface ParakeetLease {
    /** The recognizer, once loaded. Throws when the model failed to load. */
    suspend fun recognizer(): OnDeviceRecognizer

    /** Ends the claim. Safe to call more than once and from any thread. */
    fun release()
}

/**
 * Keeps at most one on-device model (Parakeet or Phonon) loaded per process and closes it only when nobody holds it.
 *
 * A load runs in the pool's own scope: a caller that is cancelled while the model loads (the keyboard closed
 * mid-load) does not abandon a loaded model, which would leak about 1 GB of native memory. A model is released
 * [idleRelease] after its last lease ended, or on [release], and never while a lease is held, so a segment in
 * progress or one waiting to transcribe always finds its recognizer open. A model whose load failed is reported by
 * [loadFailed] until it is loaded successfully, so a host can fall back instead of failing every dictation.
 */
class ParakeetRecognizerPool(
    var idleRelease: Duration = 2.minutes,
    private val load: (OnDeviceModelFiles) -> OnDeviceRecognizer = ::loadRecognizer,
) : ParakeetRecognizers {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val lock = Mutex()
    private var current: Entry? = null
    private var idleJob: Job? = null
    private val failed = mutableSetOf<File>()

    private class Entry(val dir: File, val recognizer: Deferred<OnDeviceRecognizer>) {
        var leases = 0
        var retired = false
    }

    override suspend fun lease(files: OnDeviceModelFiles): ParakeetLease {
        val entry = lock.withLock {
            idleJob?.cancel()
            entryFor(files).also { it.leases += 1 }
        }
        return Lease(entry)
    }

    /** Starts loading [files] so the first segment does not wait for it; released after [idleRelease] if unused. */
    fun warmUp(files: OnDeviceModelFiles) {
        scope.launch {
            lock.withLock {
                val entry = entryFor(files)
                if (entry.leases == 0) scheduleIdle()
            }
        }
    }

    /** Releases the loaded model as soon as no lease holds it, e.g. under memory pressure or before a delete. */
    fun release() {
        scope.launch {
            lock.withLock {
                idleJob?.cancel()
                current?.let(::retire)
            }
        }
    }

    /** True while the last load of [dir] failed; cleared by a successful load. */
    fun loadFailed(dir: File): Boolean = synchronized(failed) { dir in failed }

    /** Forgets a failed load of [dir], e.g. after the model was downloaded again. */
    fun clearFailure(dir: File) {
        synchronized(failed) { failed.remove(dir) }
    }

    // Call with lock held.
    private fun entryFor(files: OnDeviceModelFiles): Entry {
        current?.let { entry ->
            val loadError = entry.recognizer.takeIf { it.isCompleted }?.getCompletionExceptionOrNull()
            if (entry.dir == files.dir && loadError == null) return entry
            retire(entry)
        }
        val recognizer = scope.async {
            runCatching { load(files) }
                .onSuccess { synchronized(failed) { failed.remove(files.dir) } }
                .onFailure { synchronized(failed) { failed.add(files.dir) } }
                .getOrThrow()
        }
        return Entry(files.dir, recognizer).also { current = it }
    }

    // Call with lock held.
    private fun retire(entry: Entry) {
        if (current === entry) current = null
        entry.retired = true
        if (entry.leases == 0) close(entry)
    }

    // Call with lock held.
    private fun scheduleIdle() {
        idleJob?.cancel()
        idleJob = scope.launch {
            delay(idleRelease)
            lock.withLock {
                val entry = current
                if (entry != null && entry.leases == 0) retire(entry)
            }
        }
    }

    /** Closes [entry]'s recognizer once its load has finished, off the caller's thread. */
    private fun close(entry: Entry) {
        scope.launch { runCatching { entry.recognizer.await().close() } }
    }

    private inner class Lease(private val entry: Entry) : ParakeetLease {
        @Volatile
        private var released = false

        override suspend fun recognizer(): OnDeviceRecognizer = entry.recognizer.await()

        override fun release() {
            synchronized(this) {
                if (released) return
                released = true
            }
            scope.launch {
                lock.withLock {
                    entry.leases -= 1
                    if (entry.leases > 0) return@withLock
                    if (entry.retired) close(entry) else if (current === entry) scheduleIdle()
                }
            }
        }
    }
}

/** Loads [files] with the engine its directory needs. */
private fun loadRecognizer(files: OnDeviceModelFiles): OnDeviceRecognizer = when (files) {
    is PhononModelFiles -> PhononRecognizer(files)
    is ParakeetModelFiles -> ParakeetRecognizer(files)
}
