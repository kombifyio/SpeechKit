package io.kombify.speechkit.app.build

import io.kombify.speechkit.BuildConfig

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
}
