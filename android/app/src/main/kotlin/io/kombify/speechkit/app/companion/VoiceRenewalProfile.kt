package io.kombify.speechkit.app.companion

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.net.sameVoiceRenewalAccount

/** Called off the main thread, before any budget reservation is requested. */
suspend fun voiceRenewalProfile(
    context: Context,
    companion: CompanionProvisioner,
    profiles: ConnectionProfileSource,
    original: ConnectionProfile,
    claimNamespace: String,
): ConnectionProfile {
    val initial = original as? ConnectionProfile.Server ?: return profiles.currentProfile()
    val stored = StoredServerProfile.loadCloud(context)
    if (!StoredServerProfile.cloudSelected(context) || StoredServerProfile.hasOwnCloudSession(context) ||
        stored == null || stored.normalizedBaseUrl != initial.normalizedBaseUrl ||
        !sameVoiceRenewalAccount(initial.bearerToken, stored.bearerToken, claimNamespace)
    ) return profiles.currentProfile()

    // Fetch without altering the shared Companion cache. A failed bind leaves
    // the last profile untouched, and a changed account never enters the cache.
    val provisioned = when (val result = companion.provisionForVoiceRenewal()) {
        is CompanionProvision.Session -> result.profile
        CompanionProvision.Empty -> return ConnectionProfile.SystemOnDevice()
        CompanionProvision.Rejected, CompanionProvision.Unavailable -> return profiles.currentProfile()
    }
    if (!StoredServerProfile.cloudSelected(context) || StoredServerProfile.hasOwnCloudSession(context) ||
        StoredServerProfile.loadCloud(context) != stored ||
        provisioned.normalizedBaseUrl != initial.normalizedBaseUrl ||
        !sameVoiceRenewalAccount(initial.bearerToken, provisioned.bearerToken, claimNamespace)
    ) return ConnectionProfile.SystemOnDevice()
    return provisioned
}
