package io.kombify.speechkit.net

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Handler
import android.os.Looper
import java.nio.charset.StandardCharsets

/**
 * Browses `_speechkit._tcp` on the LAN. Found URLs come from TXT `url=`;
 * host:port is only a fallback when TXT is missing. Never reads credentials.
 */
class NsdLanFinder(
    context: Context,
    private val main: Handler = Handler(Looper.getMainLooper()),
) {
    private val nsd = context.applicationContext.getSystemService(NsdManager::class.java)
    private val found = linkedMapOf<String, LanServer>()
    private var discovery: NsdManager.DiscoveryListener? = null
    private var listener: ((List<LanServer>) -> Unit)? = null

    fun start(onUpdate: (List<LanServer>) -> Unit) {
        stop()
        listener = onUpdate
        val nsd = this.nsd ?: run {
            onUpdate(emptyList())
            return
        }
        val discovery = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(regType: String) = Unit
            override fun onDiscoveryStopped(serviceType: String) = Unit
            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) = Unit
            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) = Unit
            override fun onServiceFound(service: NsdServiceInfo) {
                if (!service.serviceType.contains("speechkit")) return
                @Suppress("DEPRECATION")
                nsd.resolveService(
                    service,
                    object : NsdManager.ResolveListener {
                        override fun onResolveFailed(serviceInfo: NsdServiceInfo, errorCode: Int) = Unit
                        override fun onServiceResolved(serviceInfo: NsdServiceInfo) {
                            val record = fromResolved(serviceInfo) ?: return
                            main.post {
                                found[record.instanceName] = record
                                listener?.invoke(found.values.toList())
                            }
                        }
                    },
                )
            }
            override fun onServiceLost(service: NsdServiceInfo) {
                main.post {
                    found.remove(service.serviceName)
                    listener?.invoke(found.values.toList())
                }
            }
        }
        this.discovery = discovery
        nsd.discoverServices(LanDiscoveryRecords.SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, discovery)
    }

    fun stop() {
        val current = discovery ?: return
        discovery = null
        listener = null
        found.clear()
        runCatching { nsd?.stopServiceDiscovery(current) }
    }

    companion object {
        internal fun fromResolved(info: NsdServiceInfo): LanServer? {
            val attributes = linkedMapOf<String, String>()
            info.attributes?.forEach { (key, bytes) ->
                if (bytes != null) {
                    attributes[key] = String(bytes, StandardCharsets.UTF_8)
                }
            }
            val named = LanDiscoveryRecords.parse(info.serviceName, attributes)
            if (named != null) return named
            val host = info.host?.hostAddress ?: return null
            if (info.port <= 0) return null
            return LanServer(
                instanceName = info.serviceName,
                url = "http://$host:${info.port}",
            )
        }
    }
}
