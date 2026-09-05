package io.kombify.speechkit.app.build

import io.kombify.speechkit.domain.ConnectionProfile

/**
 * The oss flavor ships no connection at all: it is the zero-config build, and
 * a default server would contradict that.
 */
object ShippedDefaults {

    val serverUrl: String? get() = null
    val serverToken: String? get() = null
    val cloudConnectSupported: Boolean get() = false

    val showLabTabs: Boolean get() = true

    fun shippedProfile(): ConnectionProfile.Server? = null
}
