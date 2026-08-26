package io.kombify.speechkit.domain

/**
 * How this install chose its backend. Companion being present is not a mode.
 *
 * @see docs/android-connect-distribution-standard.md
 */
enum class ConnectionMode(val wire: String) {
    ON_DEVICE("on_device"),
    SPEECHKIT_ORIGIN("speechkit_origin"),
    SELF_HOST("self_host"),
    KOMBIFY_CLOUD("kombify_cloud"),
    ;

    companion object {
        fun fromWire(raw: String?): ConnectionMode? {
            val value = raw?.trim().orEmpty()
            if (value.isEmpty()) return null
            return entries.firstOrNull { it.wire == value }
        }
    }
}

/** Resolves the [ConnectionProfile] a host should use right now. */
fun interface ConnectionProfileSource {
    fun currentProfile(): ConnectionProfile
}

/** Infer a mode when the user has never chosen one. Never infers cloud. */
fun inferConnectionMode(
    stored: ConnectionProfile.Server?,
    shipped: ConnectionProfile.Server?,
): ConnectionMode = when {
    stored != null -> ConnectionMode.SELF_HOST
    shipped != null -> ConnectionMode.SPEECHKIT_ORIGIN
    else -> ConnectionMode.ON_DEVICE
}

/**
 * kombify product resolution. Companion is applied only in [ConnectionMode.KOMBIFY_CLOUD].
 */
fun fallbackModeAfterDisconnect(shipped: ConnectionProfile.Server?): ConnectionMode =
    if (shipped != null) ConnectionMode.SPEECHKIT_ORIGIN else ConnectionMode.ON_DEVICE

fun resolveConnectionProfile(
    mode: ConnectionMode,
    companion: ConnectionProfile.Server?,
    stored: ConnectionProfile.Server?,
    shipped: ConnectionProfile.Server?,
): ConnectionProfile = when (mode) {
    ConnectionMode.KOMBIFY_CLOUD ->
        companion ?: ConnectionProfile.SystemOnDevice()
    ConnectionMode.SELF_HOST ->
        stored ?: ConnectionProfile.SystemOnDevice()
    ConnectionMode.SPEECHKIT_ORIGIN ->
        shipped ?: stored ?: ConnectionProfile.SystemOnDevice()
    ConnectionMode.ON_DEVICE ->
        ConnectionProfile.SystemOnDevice()
}

/**
 * Profile a Dev/Voice test surface should open.
 *
 * Settings owns persistence. An empty URL keeps [current]. A URL that matches
 * the resolved server keeps that server's token when the token field is blank,
 * so a baked origin token or Cloud JWT is not dropped.
 */
fun testSurfaceConnectProfile(
    current: ConnectionProfile,
    typedUrl: String,
    typedToken: String,
): ConnectionProfile {
    val url = typedUrl.trim()
    if (url.isEmpty()) return current
    val typed = typedToken.trim().ifEmpty { null }
    val resolved = current as? ConnectionProfile.Server
    if (resolved != null &&
        resolved.normalizedBaseUrl.equals(url.trimEnd('/'), ignoreCase = true) &&
        typed == null
    ) {
        return resolved
    }
    return ConnectionProfile.Server(url, typed ?: resolved?.bearerToken)
}
