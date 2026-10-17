package io.kombify.speechkit.app.companion

import android.content.Context
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.CloudBearer
import io.kombify.speechkit.net.StoredServerProfile
import kotlinx.coroutines.runBlocking

/**
 * Companion is optional. It can bootstrap a session before SpeechKit signs in.
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
 * device's own refresh token when it owns the session, otherwise Companion.
 * A refused independent login never switches to Companion's account. The renewed
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
    val server = failed as? ConnectionProfile.Server ?: return null
    val rejected = server.bearerToken ?: return null
    return recoverCloudSession(
        hasOwnSession = StoredServerProfile.hasOwnCloudSession(context),
        recoverOwn = {
            val renewed = runBlocking { CloudBearer.recoverRejected(rejected) }
            val stored = StoredServerProfile.loadCloud(context)
            if (renewed != null && stored != null) ConnectionProfile.Server(stored.baseUrl, renewed) else null
        },
        recoverCompanion = {
            val cached = companion.currentSession()
            if (cached != null && cached.normalizedBaseUrl == server.normalizedBaseUrl &&
                cached.bearerToken != rejected
            ) {
                CompanionProvision.Session(cached)
            } else {
                companion.recoverFromUnauthorized(failed)
            }
        },
        saveCompanion = { session ->
            StoredServerProfile.saveCompanionCloud(
                context,
                session.profile.baseUrl,
                session.profile.bearerToken.orEmpty(),
                session.expiresAtEpochMs,
                expectedAccessToken = rejected,
            )
        },
    )
}

internal fun recoverCloudSession(
    hasOwnSession: Boolean,
    recoverOwn: () -> ConnectionProfile.Server?,
    recoverCompanion: () -> CompanionProvision,
    saveCompanion: (CompanionProvision.Session) -> Boolean,
): ConnectionProfile.Server? {
    if (hasOwnSession) return recoverOwn()
    val session = recoverCompanion() as? CompanionProvision.Session ?: return null
    return session.profile.takeIf { saveCompanion(session) }
}
