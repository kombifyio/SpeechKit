package io.kombify.speechkit.app.di

import android.content.Context
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.net.StoredConnectionSelection
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.net.sameVoiceRenewalAccount

/** The same explicit connection choice applies to every distribution and host. */
internal fun connectionProfiles(context: Context, companion: CompanionProvisioner): ConnectionProfileSource {
    val selection = StoredConnectionSelection(context)
    if (StoredServerProfile.cloudSelected(context) && StoredServerProfile.loadCloud(context) != null &&
        !StoredServerProfile.hasOwnCloudSession(context)
    ) companion.warm()
    return ConnectionProfileSource {
        val snapshot = selection.snapshot(ShippedDefaults.shippedProfile())
        if (!StoredServerProfile.cloudSelected(context) || StoredServerProfile.hasOwnCloudSession(context)) {
            snapshot.profile
        } else {
            val cloud = StoredServerProfile.loadCloud(context)
            val renewed = cloud?.let { captured ->
                companion.currentSession()?.takeIf { candidate ->
                    candidate.normalizedBaseUrl == captured.normalizedBaseUrl &&
                        sameVoiceRenewalAccount(captured.bearerToken, candidate.bearerToken, ShippedDefaults.tokenClaimNamespace)
                }
            }
            // Refresh custody without selecting Cloud again. A concurrent switch wins.
            if (cloud != null && renewed != null && renewed.bearerToken != cloud.bearerToken &&
                selection.revision() == snapshot.revision
            ) StoredServerProfile.saveCompanionCloud(
                context, renewed.baseUrl, renewed.bearerToken.orEmpty(),
                expectedAccessToken = cloud.bearerToken, expectedCustodyId = cloud.custodyId,
            )
            selection.snapshot(ShippedDefaults.shippedProfile()).profile
        }
    }
}
