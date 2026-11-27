package io.kombify.speechkit.app.telemetry

import android.content.Context

/** The OSS variant has no company crash-reporting SDK or sink. */
object CrashDiagnostics {
    fun initialize(context: Context) = Unit
    fun available(): Boolean = false
    fun enabled(context: Context): Boolean = false
    fun sendingAllowed(context: Context): Boolean = false
    fun setEnabled(context: Context, enabled: Boolean): Boolean = false
    fun status(context: Context): String = "off"
    fun installOuterHandler() = Unit
}
