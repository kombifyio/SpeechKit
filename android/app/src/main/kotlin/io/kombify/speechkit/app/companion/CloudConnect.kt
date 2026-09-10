package io.kombify.speechkit.app.companion

import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile

/**
 * Companion is optional. A provisioned session is used when it exists.
 * Pin mismatch, signed-out, and missing Companion are not connect failures:
 * SpeechKit signs in to kombify itself.
 */
fun cloudModeAfterProvision(outcome: CompanionProvision): ConnectionMode? =
    when (outcome) {
        is CompanionProvision.Session -> ConnectionMode.KOMBIFY_CLOUD
        CompanionProvision.Empty,
        CompanionProvision.Rejected,
        CompanionProvision.Unavailable,
        -> null
    }

fun companionCloudProfile(outcome: CompanionProvision): ConnectionProfile.Server? =
    (outcome as? CompanionProvision.Session)?.profile

/** Resume must not start a second sign-in after Cloud is already connected. */
fun shouldStartConnectOnResume(
    requested: Boolean,
    alreadyConnected: Boolean,
    connecting: Boolean,
): Boolean = requested && !alreadyConnected && !connecting

/**
 * A 401 on the independently stored Gateway JWT. Companion may already have
 * failed; a refresh token is the remaining path. Tester/self-host bearers
 * are not this session.
 */
fun recoverIndependentCloud(
    failed: ConnectionProfile,
    stored: ConnectionProfile.Server?,
    refreshToken: String?,
    refresh: (String) -> String?,
): ConnectionProfile.Server? {
    val server = failed as? ConnectionProfile.Server ?: return null
    if (stored == null || stored.bearerToken != server.bearerToken) return null
    val token = refreshToken?.trim().orEmpty()
    if (token.isEmpty()) return null
    val access = refresh(token)?.trim().orEmpty()
    if (access.isEmpty()) return null
    return ConnectionProfile.Server(stored.baseUrl, access)
}
