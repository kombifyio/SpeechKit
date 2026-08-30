package io.kombify.speechkit.app.companion

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import android.provider.Settings
import io.kombify.speechkit.coinstall.v1.CoinstallContract
import io.kombify.speechkit.coinstall.v1.ICoinstallService
import io.kombify.speechkit.coinstall.v1.ProvisionRequest
import io.kombify.speechkit.domain.ConnectionProfile
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/**
 * Asks kombify Companion for the logged-in user's SpeechKit session.
 *
 * The bind runs off the caller thread. The keyboard reads the last known
 * session immediately; a cold Companion is given several seconds in the
 * background instead of stalling the IME for 800 ms and missing the JWT.
 */
class CompanionProvisioner internal constructor(
    private val installed: () -> Boolean,
    private val binder: () -> CompanionProvision,
    private val cache: CompanionSessionCache = CompanionSessionCache(),
    private val executor: Executor = companionProvisionExecutor,
) {
    constructor(context: Context) : this(
        installed = { companionInstalled(context) },
        binder = { bindCompanion(context) },
    )

    private val refreshing = AtomicBoolean(false)

    fun currentSession(): ConnectionProfile.Server? {
        if (!installed()) return null
        if (cache.needsRefresh()) refresh()
        return cache.current()
    }

    fun isCompanionInstalled(): Boolean = installed()

    /** Drop a leftover user session after Disconnect. The next currentSession() re-binds if Companion is still signed in. */
    fun forgetSession() {
        cache.clear()
    }

    /**
     * Bind now for an explicit Connect. Updates the cache so the next
     * session open can already see the user JWT.
     */
    fun provisionNow(): CompanionProvision {
        if (!installed()) return CompanionProvision.Unavailable
        val outcome = binder()
        when (outcome) {
            is CompanionProvision.Session -> cache.offer(outcome.profile)
            CompanionProvision.Empty -> cache.clear()
            CompanionProvision.Rejected, CompanionProvision.Unavailable -> Unit
        }
        return outcome
    }

    /** Start a bind so the first dictation can already see a user session. */
    fun warm() {
        if (installed()) refresh()
    }

    private fun refresh() {
        if (!refreshing.compareAndSet(false, true)) return
        executor.execute {
            try {
                when (val outcome = binder()) {
                    is CompanionProvision.Session -> cache.offer(outcome.profile)
                    CompanionProvision.Empty -> cache.clear()
                    CompanionProvision.Rejected, CompanionProvision.Unavailable -> Unit
                }
            } finally {
                refreshing.set(false)
            }
        }
    }

}

private const val BIND_TIMEOUT_MS = 5_000L

private val companionProvisionExecutor: Executor = Executors.newSingleThreadExecutor { task ->
    Thread(task, "speechkit-companion-provision").apply { isDaemon = true }
}

private fun companionInstalled(context: Context): Boolean =
    runCatching {
        context.packageManager.getPackageInfo(CoinstallContract.COMPANION_PACKAGE, 0)
    }.isSuccess

private fun bindCompanion(context: Context): CompanionProvision {
    val ready = CountDownLatch(1)
    val outcome = AtomicReference<CompanionProvision>(CompanionProvision.Unavailable)
    val conn = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName?, service: IBinder?) {
            try {
                val api = ICoinstallService.Stub.asInterface(service)
                if (api.getContractVersion() < CoinstallContract.VERSION) {
                    outcome.set(CompanionProvision.Unavailable)
                    return
                }
                val provisioned = api.provision(
                    ProvisionRequest().apply {
                        deviceId = Settings.Secure.getString(
                            context.contentResolver,
                            Settings.Secure.ANDROID_ID,
                        ).orEmpty()
                    },
                )
                val url = provisioned?.serverUrl?.trim().orEmpty()
                val token = provisioned?.bearerToken?.trim().orEmpty()
                outcome.set(
                    if (url.isNotEmpty() && token.isNotEmpty()) {
                        CompanionProvision.Session(ConnectionProfile.Server(url, token))
                    } else {
                        CompanionProvision.Empty
                    },
                )
            } catch (_: SecurityException) {
                // coinstall_caller_rejected: Companion could not match this
                // APK's signing certificate against its pinned list.
                outcome.set(CompanionProvision.Rejected)
            } finally {
                ready.countDown()
            }
        }

        override fun onServiceDisconnected(name: ComponentName?) = Unit
    }
    val intent = Intent(CoinstallContract.BIND_ACTION)
        .setPackage(CoinstallContract.COMPANION_PACKAGE)
    val bound = runCatching {
        context.bindService(intent, conn, Context.BIND_AUTO_CREATE)
    }.getOrDefault(false)
    if (!bound) return CompanionProvision.Unavailable
    try {
        if (!ready.await(BIND_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
            return CompanionProvision.Unavailable
        }
    } finally {
        runCatching { context.unbindService(conn) }
    }
    return outcome.get()
}

sealed interface CompanionProvision {
    data class Session(val profile: ConnectionProfile.Server) : CompanionProvision
    data object Empty : CompanionProvision

    /**
     * Companion answered, but refused this caller: our signing certificate is
     * not in its pinned allow-list. This is a configuration mismatch between
     * the two apps, not something the user can fix by signing in again, so it
     * is reported separately from a plain Unavailable.
     */
    data object Rejected : CompanionProvision
    data object Unavailable : CompanionProvision
}
