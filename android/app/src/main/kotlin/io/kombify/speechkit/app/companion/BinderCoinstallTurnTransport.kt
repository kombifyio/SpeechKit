package io.kombify.speechkit.app.companion

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import io.kombify.speechkit.coinstall.v1.CoinstallContract
import io.kombify.speechkit.coinstall.v1.ICoinstallCallback
import io.kombify.speechkit.coinstall.v1.ICoinstallService
import io.kombify.speechkit.coinstall.v1.TurnRequest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

/** Same wait Companion provision uses before a bind is treated as unavailable. */
internal const val COMPANION_BIND_TIMEOUT_MS = 5_000L

internal const val COINSTALL_TURN_UNAVAILABLE = 3

/**
 * Keeps the Companion bind alive until [ICoinstallCallback] fires a terminal
 * result. A bind that never reaches [ServiceConnection.onServiceConnected]
 * fails closed after [COMPANION_BIND_TIMEOUT_MS] instead of hanging.
 */
class BinderCoinstallTurnTransport internal constructor(
    private val bind: (ServiceConnection) -> Boolean,
    private val unbindService: (ServiceConnection) -> Unit,
    private val bindTimeoutMs: Long = COMPANION_BIND_TIMEOUT_MS,
    private val scheduler: ScheduledExecutorService = bindTimeoutScheduler,
) : CoinstallTurnTransport {

    constructor(context: Context) : this(
        bind = { conn ->
            val intent = Intent(CoinstallContract.BIND_ACTION)
                .setPackage(CoinstallContract.COMPANION_PACKAGE)
            context.bindService(intent, conn, Context.BIND_AUTO_CREATE)
        },
        unbindService = { conn -> context.unbindService(conn) },
    )

    private val connections = ConcurrentHashMap<String, ServiceConnection>()
    private val apis = ConcurrentHashMap<String, ICoinstallService>()
    private val connectTimeouts = ConcurrentHashMap<String, ScheduledFuture<*>>()
    private val finished = ConcurrentHashMap.newKeySet<String>()

    override fun startTurn(turnId: String, text: String, callback: CoinstallTurnCallback) {
        val conn = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, service: IBinder?) {
                cancelConnectTimeout(turnId)
                if (finished.contains(turnId)) {
                    unbind(turnId)
                    return
                }
                try {
                    val api = ICoinstallService.Stub.asInterface(service)
                    apis[turnId] = api
                    val request = TurnRequest().apply {
                        this.turnId = turnId
                        this.text = text
                    }
                    api.startTurn(
                        request,
                        object : ICoinstallCallback.Stub() {
                            override fun onPartial(id: String?, partial: String?) {
                                callback.onPartial(id.orEmpty(), partial.orEmpty())
                            }

                            override fun onComplete(id: String?, complete: String?) {
                                finish(turnId) {
                                    callback.onComplete(id.orEmpty(), complete.orEmpty())
                                }
                            }

                            override fun onError(id: String?, code: Int, message: String?) {
                                finish(turnId) {
                                    callback.onError(id.orEmpty(), code, message.orEmpty())
                                }
                            }
                        },
                    )
                } catch (_: SecurityException) {
                    finish(turnId) { callback.onError(turnId, 1, "rejected") }
                } catch (e: Exception) {
                    finish(turnId) {
                        callback.onError(turnId, 2, e.message ?: "unavailable")
                    }
                }
            }

            override fun onServiceDisconnected(name: ComponentName?) {
                finish(turnId) {
                    callback.onError(turnId, COINSTALL_TURN_UNAVAILABLE, "unavailable")
                }
            }
        }
        connections[turnId] = conn
        armConnectTimeout(turnId, callback)
        val bound = runCatching { bind(conn) }.getOrDefault(false)
        if (!bound) {
            finish(turnId) {
                callback.onError(turnId, COINSTALL_TURN_UNAVAILABLE, "unavailable")
            }
        }
    }

    override fun cancelTurn(turnId: String) {
        runCatching { apis[turnId]?.cancelTurn(turnId) }
        finish(turnId) {}
    }

    private fun armConnectTimeout(turnId: String, callback: CoinstallTurnCallback) {
        val future = scheduler.schedule(
            {
                finish(turnId) {
                    callback.onError(turnId, COINSTALL_TURN_UNAVAILABLE, "unavailable")
                }
            },
            bindTimeoutMs,
            TimeUnit.MILLISECONDS,
        )
        connectTimeouts[turnId] = future
    }

    private fun cancelConnectTimeout(turnId: String) {
        connectTimeouts.remove(turnId)?.cancel(false)
    }

    private fun finish(turnId: String, notify: () -> Unit) {
        cancelConnectTimeout(turnId)
        if (!finished.add(turnId)) {
            unbind(turnId)
            return
        }
        notify()
        unbind(turnId)
    }

    private fun unbind(turnId: String) {
        apis.remove(turnId)
        val conn = connections.remove(turnId) ?: return
        runCatching { unbindService(conn) }
    }

    private companion object {
        val bindTimeoutScheduler: ScheduledExecutorService =
            Executors.newSingleThreadScheduledExecutor { task ->
                Thread(task, "speechkit-coinstall-bind-timeout").apply { isDaemon = true }
            }
    }
}
