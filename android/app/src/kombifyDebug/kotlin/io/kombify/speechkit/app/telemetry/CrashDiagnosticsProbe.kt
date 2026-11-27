package io.kombify.speechkit.app.telemetry

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Debug-only shell-protected crash probe; never packaged in a release. */
class CrashDiagnosticsProbe : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == "io.kombify.speechkit.debug.CRASH_DIAGNOSTICS") {
            when (intent.getStringExtra("case")) {
                "mapping" -> throw IllegalArgumentException("Synthetic mapping diagnostics probe")
                "privacy" -> throw UnsupportedOperationException("Synthetic privacy diagnostics probe")
            }
            throw IllegalStateException("Synthetic crash diagnostics probe")
        }
    }
}
