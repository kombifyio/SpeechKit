package io.kombify.speechkit.app.build

import io.kombify.speechkit.domain.ConnectionProfile

/**
 * The oss flavor ships no connection at all: it is the zero-config build, and
 * a default server would contradict that.
 */
object ShippedDefaults {

    val serverUrl: String? get() = null
    val serverToken: String? get() = null
    val cloudConnectSupported: Boolean get() = io.kombify.speechkit.BuildConfig.CONNECTION_SETUP_ENABLED

    val cloudAuthClientId: String get() = io.kombify.speechkit.BuildConfig.CLOUD_AUTH_CLIENT_ID.trim()

    /** Claims used by an explicitly connected Kombify account. */
    val tokenClaimNamespace: String get() = io.kombify.speechkit.net.KombifyCloudAuth.DEFAULT_CLAIM_NAMESPACE

    val showLabTabs: Boolean get() = true

    fun shippedProfile(): ConnectionProfile.Server? = null
}
