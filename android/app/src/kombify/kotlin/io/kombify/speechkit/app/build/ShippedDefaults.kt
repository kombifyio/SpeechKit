package io.kombify.speechkit.app.build

import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.domain.ConnectionProfile

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

    /** Hosted tester origin only when the baked bearer is present. */
    fun shippedProfile(): ConnectionProfile.Server? {
        val url = serverUrl ?: return null
        val token = serverToken ?: return null
        return ConnectionProfile.Server(url, token)
    }
}
