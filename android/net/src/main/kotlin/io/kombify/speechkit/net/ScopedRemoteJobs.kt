package io.kombify.speechkit.net

import kotlinx.coroutines.Job

/** Jobs carrying one captured account are cancelled when its custody is revoked. */
internal object ScopedRemoteJobs {
    private val jobs = mutableMapOf<String, MutableSet<Job>>()
    @Synchronized fun register(scope: String, job: Job) {
        jobs.getOrPut(scope) { mutableSetOf() }.add(job)
        job.invokeOnCompletion { synchronized(this) { jobs[scope]?.remove(job) } }
    }
    @Synchronized fun cancel(scope: String) { jobs.remove(scope)?.toList()?.forEach { it.cancel() } }
}
