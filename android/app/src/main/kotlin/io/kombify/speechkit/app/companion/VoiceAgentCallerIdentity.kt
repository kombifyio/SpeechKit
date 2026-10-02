package io.kombify.speechkit.app.companion

import android.content.pm.PackageManager
import androidx.annotation.RequiresApi

/** Pins are app-signing certificates, never upload keys or caller-provided package names. */
internal class VoiceAgentCallerIdentity(private val packages: PackageManager) {
    @RequiresApi(28)
    fun attested(uid: Int): Boolean {
        val resolved = packages.getPackagesForUid(uid)?.toList().orEmpty()
        return resolved.isNotEmpty() && resolved.all { name ->
            val expected = PINS[name] ?: return@all false
            runCatching { packages.hasSigningCertificate(name, expected.chunked(2).map { it.toInt(16).toByte() }.toByteArray(),
                PackageManager.CERT_INPUT_SHA256) }.getOrDefault(false)
        }
    }
    companion object {
        // Play readback: Companion0.8.3 run37057452288, source7b5c5acd079ec699c2fc9e16d955af6f26cc2fc9.
        // Preview app certificate: verified public application links, read2026-10-02.
        private val PINS = mapOf(
            "io.kombify.companion" to "fa16ae10d7333a6006a4d93cd321e0593ba5b1d7010d2f3c91ed3e6e36bea4d3",
            "io.kombify.companion.preview" to "9f53bd22452cf53269c100ffdd00b033caa379621a63375bc316bd78180e19d6",
        )
    }
}
