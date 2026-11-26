package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ExecutionPolicy

/** Execution permissions share the connection selection's atomic authority. */
class StoredExecutionPolicy(context: Context) {
    private val selection = StoredConnectionSelection(context)
    fun isConfigured(): Boolean = true
    fun read(legacyProfile: ConnectionProfile): ExecutionPolicy = selection.policyFor(legacyProfile)
    suspend fun write(policy: ExecutionPolicy) = selection.writePolicy(policy)
}
