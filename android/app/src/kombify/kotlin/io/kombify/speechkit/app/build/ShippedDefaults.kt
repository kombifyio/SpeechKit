package io.kombify.speechkit.app.build

import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.app.di.KombifyModule

/**
 * What this build was shipped with, for surfaces that need to explain where a
 * connection came from.
 *
 * Flavor-bound on purpose: BuildConfig carries these fields only on the
 * kombify flavor, and shared code must not assume they exist.
 */
object ShippedDefaults {

    /** The address this build ships with, or null when it ships without one. */
    val serverUrl: String?
        get() = BuildConfig.DEFAULT_SERVER_URL.trim().ifEmpty { null }

    val serverToken: String?
        get() = BuildConfig.DEFAULT_SERVER_TOKEN.trim().ifEmpty { null }

    val cloudConnectSupported: Boolean get() = true

    val cloudAuthClientId: String
        get() = BuildConfig.CLOUD_AUTH_CLIENT_ID.trim()

    /** Account claims of the cloud bearers this build signs in to or is provisioned with. */
    val tokenClaimNamespace: String get() = KombifyModule.TOKEN_CLAIM_NAMESPACE

    /** Dev/Voice lab tabs are not a tester surface. */
    val showLabTabs: Boolean get() = false

    /** Hosted tester origin only when the baked bearer is present. */
    fun shippedProfile(): ConnectionProfile.Server? {
        val url = serverUrl ?: return null
        val token = serverToken ?: return null
        return ConnectionProfile.Server(url, token)
    }
}
