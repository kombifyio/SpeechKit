package io.kombify.speechkit.app.companion

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.coinstall.v1.CoinstallContract
import io.kombify.speechkit.log.VoiceLog

/**
 * Which co-installed Companion app this device has: the first of the contract's Companion ids (release, then
 * preview) that is installed and carries that channel's pinned signing certificate. A package that only
 * shares the name is no Companion, because its answer to `provision()` decides where audio and the account
 * bearer go.
 *
 * Development builds also accept a Companion signed by the same local debug key as this app.
 */
internal object CompanionPackage {
    fun resolve(
        packages: PackageManager,
        trustsSameSigner: Boolean = BuildConfig.DEBUG,
        ownPackage: String? = null,
    ): String? {
        val states = CoinstallContract.COMPANION_PACKAGES.map { name ->
            val isInstalled = installed(packages, name)
            val trusted = isInstalled &&
                (pinned(packages, name) || (trustsSameSigner && ownPackage != null && sameSigner(packages, ownPackage, name)))
            Triple(name, isInstalled, trusted)
        }
        logDetection(states)
        return states.firstOrNull { it.third }?.first
    }

    @Volatile private var lastLogged: String? = null

    /** One line per change, so a "not installed" versus "installed but certificate mismatch" report is in the log. */
    private fun logDetection(states: List<Triple<String, Boolean, Boolean>>) {
        val line = states.joinToString(" ") { (name, isInstalled, trusted) ->
            val state = when {
                !isInstalled -> "not_installed"
                trusted -> "ok"
                else -> "installed_pin_mismatch"
            }
            "$name=$state"
        }
        if (line == lastLogged) return
        lastLogged = line
        val mismatch = states.any { it.second && !it.third }
        val message = "companion detection $line"
        if (mismatch) VoiceLog.w(VoiceLog.NET, message) else VoiceLog.i(VoiceLog.NET, message)
    }

    private fun installed(packages: PackageManager, name: String): Boolean =
        runCatching { packages.getPackageInfo(name, 0) }.isSuccess

    private fun pinned(packages: PackageManager, name: String): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && VoiceAgentCallerIdentity.pinned(packages, name)

    private fun sameSigner(packages: PackageManager, own: String, name: String): Boolean =
        runCatching { packages.checkSignatures(own, name) == PackageManager.SIGNATURE_MATCH }.getOrDefault(false)
}

/**
 * [CompanionPackage.resolve] once, kept until a Companion package is installed, replaced, changed or
 * removed. The package manager is only asked from [current], which callers reach off the main thread.
 */
internal class CompanionPackageCache(private val context: Context) {
    private class Resolved(val name: String?)

    @Volatile private var resolved: Resolved? = null

    init {
        val filter = IntentFilter().apply {
            addAction(Intent.ACTION_PACKAGE_ADDED)
            addAction(Intent.ACTION_PACKAGE_REPLACED)
            addAction(Intent.ACTION_PACKAGE_CHANGED)
            addAction(Intent.ACTION_PACKAGE_REMOVED)
            addDataScheme("package")
        }
        ContextCompat.registerReceiver(
            context.applicationContext,
            object : BroadcastReceiver() {
                override fun onReceive(context: Context, intent: Intent) {
                    if (intent.data?.schemeSpecificPart in CoinstallContract.COMPANION_PACKAGES) resolved = null
                }
            },
            filter,
            ContextCompat.RECEIVER_NOT_EXPORTED,
        )
    }

    fun current(): String? = resolved?.name ?: CompanionPackage
        .resolve(context.packageManager, ownPackage = context.packageName)
        .also { resolved = Resolved(it) }
}
