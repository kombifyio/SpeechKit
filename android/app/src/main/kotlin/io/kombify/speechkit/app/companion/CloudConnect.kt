package io.kombify.speechkit.app.companion

import android.content.Context
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.CloudBearer
import io.kombify.speechkit.net.StoredServerProfile
import kotlinx.coroutines.runBlocking

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
 * The one recovery path for a Cloud bearer the server rejected (401): the
 * Companion session first, then this device's own refresh token. The renewed
 * session is already persisted when this returns. A bearer that is neither
 * (self-host, tester origin) is left alone and yields null.
 *
 * Blocks on the network and waits for the Companion bind, which the system
 * delivers on the main thread: never call it on the main thread (it would
 * time out and freeze the UI). Use `withContext(Dispatchers.IO)`.
 */
fun recoverCloudSession(
    context: Context,
    companion: CompanionProvisioner,
    failed: ConnectionProfile,
): ConnectionProfile.Server? {
    val fromCompanion = companion.recoverFromUnauthorized(failed) as? CompanionProvision.Session
    if (fromCompanion != null) {
        // Companion hands out an access token only. It must not erase the
        // refresh token of this device's own sign-in.
        val profile = fromCompanion.profile
        StoredServerProfile.replaceCloudAccess(
            context,
            profile.baseUrl,
            profile.bearerToken.orEmpty(),
            fromCompanion.expiresAtEpochMs,
        )
        return profile
    }
    val rejected = (failed as? ConnectionProfile.Server)?.bearerToken ?: return null
    val renewed = runBlocking { CloudBearer.recoverRejected(rejected) } ?: return null
    val stored = StoredServerProfile.loadCloud(context) ?: return null
    return ConnectionProfile.Server(stored.baseUrl, renewed)
}
