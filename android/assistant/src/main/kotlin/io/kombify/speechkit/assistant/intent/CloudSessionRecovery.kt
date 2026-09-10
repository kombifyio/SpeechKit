package io.kombify.speechkit.assistant.intent

import io.kombify.speechkit.domain.ConnectionProfile

/**
 * Recovers a Cloud session after the provisioned origin rejected the bearer.
 *
 * Implemented in `:app` by Companion `provision()`. `:assistant` must not
 * import that GPL host.
 */
fun interface CloudSessionRecovery {
    fun recoverFromUnauthorized(failed: ConnectionProfile): ConnectionProfile.Server?
}
