package io.kombify.speechkit.app.companion

import android.content.Intent

/** Inbound finish URI for Companion-driven or Settings Connect. */
object ConnectCloudIntent {
    const val SCHEME: String = "speechkit"
    const val HOST: String = "connect"
    const val PATH_KOMBIFY: String = "kombify"
    const val HTTPS_HOST: String = "speechkit.kombify.io"

    fun matches(scheme: String?, host: String?, path: String?): Boolean {
        val normalizedPath = path?.trim('/') ?: ""
        if (scheme == SCHEME && host == HOST) {
            return normalizedPath.isEmpty() || normalizedPath == PATH_KOMBIFY
        }
        if (scheme == "https" && host == HTTPS_HOST) {
            return normalizedPath == "connect" || normalizedPath.startsWith("connect/")
        }
        return false
    }

    fun matches(intent: Intent?): Boolean {
        val data = intent?.data ?: return false
        return matches(data.scheme, data.host, data.path)
    }
}
