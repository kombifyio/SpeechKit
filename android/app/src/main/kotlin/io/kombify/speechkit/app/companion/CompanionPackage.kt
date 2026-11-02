package io.kombify.speechkit.app.companion

import android.content.pm.PackageManager
import android.os.Build
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.coinstall.v1.CoinstallContract

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
    ): String? = CoinstallContract.COMPANION_PACKAGES.firstOrNull { name ->
        installed(packages, name) &&
            (pinned(packages, name) || (trustsSameSigner && ownPackage != null && sameSigner(packages, ownPackage, name)))
    }

    private fun installed(packages: PackageManager, name: String): Boolean =
        runCatching { packages.getPackageInfo(name, 0) }.isSuccess

    private fun pinned(packages: PackageManager, name: String): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && VoiceAgentCallerIdentity.pinned(packages, name)

    private fun sameSigner(packages: PackageManager, own: String, name: String): Boolean =
        runCatching { packages.checkSignatures(own, name) == PackageManager.SIGNATURE_MATCH }.getOrDefault(false)
}
