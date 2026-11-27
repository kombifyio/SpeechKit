package io.kombify.speechkit.app.telemetry

import android.content.Context
import android.content.SharedPreferences
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.domain.ExecutionMode
import io.kombify.speechkit.net.NetworkUseGuard
import io.kombify.speechkit.net.StoredConnectionSelection
import io.kombify.speechkit.net.StoredServerProfile
import io.sentry.Hint
import io.sentry.ITransportFactory
import io.sentry.RequestDetails
import io.sentry.Sentry
import io.sentry.SentryEnvelope
import io.sentry.SentryEvent
import io.sentry.SentryItemType
import io.sentry.SentryLevel
import io.sentry.SentryOptions
import io.sentry.SpanContext
import io.sentry.android.core.SentryAndroidOptions
import io.sentry.protocol.Mechanism
import io.sentry.protocol.SentryException
import io.sentry.protocol.SentryStackFrame
import io.sentry.protocol.SentryStackTrace
import io.sentry.transport.ITransport
import io.sentry.transport.NoOpEnvelopeCache
import io.sentry.util.HintUtils
import okhttp3.Call
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.ByteArrayOutputStream
import java.io.OutputStream
import java.security.MessageDigest
import java.util.IdentityHashMap
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong

/** Managed test delivery only; no raw exception content is ever submitted. */
object CrashDiagnostics {
    private const val PREFS = "speechkit_crash_diagnostics"
    private const val ENABLED = "enabled"
    // SDK typed hints survive SentryClient's Hint.clear() before transport.
    private data class CrashAdmission(val epoch: Long)
    private val lock = Any()
    private var started = false
    private var context: Context? = null
    @Volatile private var transport: PrivateTransport? = null
    private var listener: SharedPreferences.OnSharedPreferenceChangeListener? = null
    private var guardListener: java.io.Closeable? = null
    @Volatile private var reporting = false
    @Volatile private var forcedOff = false
    @Volatile private var failed = false
    private var osHandler: Thread.UncaughtExceptionHandler? = null
    private val outerCapture = ThreadLocal<Boolean>()

    fun available(): Boolean = BuildConfig.SENTRY_ENABLED
    fun enabled(context: Context): Boolean = available() && !forcedOff &&
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(ENABLED, true)

    fun setEnabled(context: Context, enabled: Boolean): Boolean {
        if (!available()) return false
        if (!enabled) { forcedOff = true; reporting = false; transport?.cancel() }
        val saved = runCatching {
            context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(ENABLED, enabled).commit()
        }.getOrDefault(false)
        if (!saved) { forcedOff = true; reporting = false; transport?.cancel(); return false }
        forcedOff = false
        runCatching { refresh() }.onFailure(::initializationFailed)
        return true
    }

    fun status(context: Context): String {
        if (!enabled(context)) return "off"
        if (failed) return "error"
        val allowed = policyState() ?: return "error"
        return if (!allowed) "local" else if (reporting) "active" else "error"
    }
    /** Called before HeliBoard installs its uncaught-handler wrapper. */
    fun initialize(context: Context) {
        runCatching { initializeSafely(context) }.onFailure(::initializationFailed)
    }

    private fun initializationFailed(error: Throwable) {
        failed = true
        reporting = false
        runCatching { transport?.cancel() }
        logFailure("initialization", error)
    }

    private fun logFailure(stage: String, error: Throwable) {
        // Local technical evidence only: never log exception messages or values.
        runCatching {
            val frames = error.stackTrace.take(8).joinToString(";") { "${it.className}.${it.methodName}:${it.lineNumber}" }
            android.util.Log.w("sk.diagnostics", "${stage}_failed type=${error.javaClass.name} frames=$frames")
        }
    }

    private fun initializeSafely(context: Context) {
        if (!available()) return
        this.context = context.applicationContext
        StoredConnectionSelection.configureLegacyFirebase(!BuildConfig.CONNECTION_SETUP_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED)
        StoredConnectionSelection.configureFirebase(BuildConfig.CONNECTION_SETUP_ENABLED && BuildConfig.HYBRID_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED)
        // snapshot initializes NetworkUseGuard from the persisted selection, rather than its permissive process default.
        runCatching { StoredConnectionSelection(context).snapshot(ShippedDefaults.shippedProfile()) }.getOrElse { return }
        guardListener = NetworkUseGuard.listen {
            reporting = false
            runCatching { transport?.cancel() }
            emptyList()
        }
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        osHandler = previous
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            if (outerCapture.get() != true) runCatching { record(error) }.onFailure { logFailure("capture", it) }
            previous?.uncaughtException(thread, error)
        }
        val change = SharedPreferences.OnSharedPreferenceChangeListener { _, _ ->
            runCatching { refresh() }.onFailure(::initializationFailed)
        }
        listener = change
        context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE).registerOnSharedPreferenceChangeListener(change)
        refresh()
    }

    /** Capture runtime crashes before HeliBoard's local report writer runs. */
    fun installOuterHandler() {
        runCatching {
            if (!available() || context == null) return
            val inner = Thread.getDefaultUncaughtExceptionHandler()
            val fallback = osHandler
            Thread.setDefaultUncaughtExceptionHandler { thread, error ->
                runCatching { record(error) }.onFailure { logFailure("capture", it) }
                outerCapture.set(true)
                try { inner?.uncaughtException(thread, error) }
                catch (_: Throwable) { runCatching { fallback?.uncaughtException(thread, error) } }
                finally { outerCapture.remove() }
            }
        }.onFailure { failed = true; reporting = false; runCatching { transport?.cancel() } }
    }
    fun sendingAllowed(context: Context): Boolean = available() && reporting && policyAllows()

    private fun policyState(): Boolean? = runCatching {
        val app = context ?: return null
        val policy = StoredConnectionSelection(app).snapshot(ShippedDefaults.shippedProfile()).policy
        policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY && enabled(app)
    }.getOrNull()
    private fun policyAllows(): Boolean = policyState() == true

    private fun refresh() = synchronized(lock) {
        val permitted = policyAllows()
        if (!permitted) {
            reporting = false
            transport?.cancel()
            return@synchronized
        }
        if (!started) {
            // The SDK requires Android options on Android. Use core init with
            // those options so no Android auto-collection integrations are installed.
            val options = SentryAndroidOptions().also { options ->
                options.setAnrEnabled(false)
                options.setEnableNdk(false)
                options.setEnableScopeSync(false)
                options.setEnableNdkAppHangTracking(false)
                options.setTombstoneEnabled(false)
                options.setReportHistoricalAnrs(false)
                options.setReportHistoricalTombstones(false)
                options.setMemoryLimiterEnabled(false)
                options.setReportHistoricalMemoryLimiterExits(false)
                options.enableAllAutoBreadcrumbs(false)
                options.setEnableAutoActivityLifecycleTracing(false)
                options.setEnableFramesTracking(false)
                options.setEnableStandaloneAppStartTracing(false)
                options.setEnableAutoTraceIdGeneration(false)
                options.setAttachScreenshot(false)
                options.setAttachViewHierarchy(false)
                options.setCollectAdditionalContext(false)
                options.setCollectExternalStorageContext(false)
                options.setEnableRootCheck(false)
                options.dsn = BuildConfig.SENTRY_DSN
                options.release = BuildConfig.SENTRY_RELEASE
                options.environment = BuildConfig.SENTRY_ENVIRONMENT
                options.dist = BuildConfig.VERSION_CODE.toString()
                // The Gradle plugin writes this asset from the actual R8 mapping.
                runCatching {
                    val properties = java.util.Properties()
                    context?.assets?.open("sentry-debug-meta.properties")?.use(properties::load)
                    properties.getProperty("io.sentry.ProguardUuids")?.takeIf { it.matches(Regex("[0-9a-fA-F-]{36}")) }
                }.getOrNull()?.let { options.proguardUuid = it }
                options.isEnableUncaughtExceptionHandler = false
                options.isEnableShutdownHook = false
                options.isEnableAutoSessionTracking = false
                options.isSendDefaultPii = false
                options.isSendClientReports = false
                options.isEnableScopePersistence = false
                options.isAttachStacktrace = false
                options.isAttachThreads = false
                options.isAttachServerName = false
                options.isSendModules = false
                options.maxBreadcrumbs = 0
                options.tracesSampleRate = 0.0
                options.setEnvelopeDiskCache(NoOpEnvelopeCache.getInstance())
                options.integrations.clear()
                options.setTransportFactory(ITransportFactory { sentryOptions, details ->
                    PrivateTransport(sentryOptions, details).also { transport = it }
                })
                options.setBeforeSend { event, hint ->
                    // Only records built by record() enter the transport; never attach scope data.
                    val admission = HintUtils.getSentrySdkHint(hint) as? CrashAdmission
                    if (admission == null || !reporting || admission.epoch != transport?.epoch() || !policyAllows()) null else event.apply {
                        val suppressed = (extras?.get("error_budget.suppressed") as? Number)?.toLong()?.coerceIn(0, 1_000_000) ?: 0L
                        user = null; request = null; breadcrumbs = null
                        setExtras(mapOf("error_budget.suppressed" to suppressed))
                        contexts.clear(); contexts.setTrace(SpanContext("speechkit.android.crash"))
                        tags = mapOf("service" to "speechkit.android",
                            "os_api" to android.os.Build.VERSION.SDK_INT.toString(),
                            "device_model" to android.os.Build.MODEL.orEmpty().replace(Regex("[^A-Za-z0-9 ._-]"), "_").take(64),
                            "manufacturer" to android.os.Build.MANUFACTURER.orEmpty().replace(Regex("[^A-Za-z0-9 ._-]"), "_").take(40),
                            "android_release" to android.os.Build.VERSION.RELEASE.orEmpty().replace(Regex("[^A-Za-z0-9 ._-]"), "_").take(32),
                            "app_version" to BuildConfig.VERSION_NAME.replace(Regex("[^A-Za-z0-9 ._+-]"), "_").take(64))
                        threads = null; setModules(null); message = null; throwable = null
                    }
                }
            }
            Sentry.init(options)
            started = true
        }
        failed = false
        reporting = true
    }

    private fun record(error: Throwable) {
        val epoch = transport?.epoch() ?: return
        if (!reporting || !policyAllows()) return
        // Android often wraps the app failure. Keep a bounded cause chain and
        // only app-owned frames; never copy exception text or frame variables.
        val seen = IdentityHashMap<Throwable, Boolean>()
        val causes = mutableListOf<Throwable>()
        var cause: Throwable? = error
        while (cause != null && causes.size < 5 && seen.put(cause, true) == null) {
            causes.add(cause)
            cause = cause.cause
        }
        val exceptions = causes.asReversed().map { failure ->
            val frames = failure.stackTrace.asSequence().filter {
                it.className.startsWith("io.kombify.speechkit.") || it.className.startsWith("helium314.keyboard.")
            }.take(32).map { frame ->
                SentryStackFrame().apply {
                    module = frame.className.take(180)
                    function = frame.methodName.take(100)
                    filename = frame.fileName?.substringAfterLast('/')?.substringAfterLast('\\')?.take(100)
                    lineno = frame.lineNumber.takeIf { it > 0 }
                    isInApp = true
                }
            }.toList().reversed()
            SentryException().apply {
                type = failure.javaClass.name.take(180)
                if (frames.isNotEmpty()) stacktrace = SentryStackTrace(frames)
                mechanism = Mechanism().apply { type = "uncaught"; isHandled = false }
            }
        }
        val signature = exceptions.joinToString("|") { exception ->
            exception.type.orEmpty() + ":" + exception.stacktrace?.frames.orEmpty().joinToString("|") {
                "${it.module}:${it.function}:${it.lineno}"
            }
        }
        val fingerprint = MessageDigest.getInstance("SHA-256").digest(signature.toByteArray()).joinToString("") { "%02x".format(it) }
        val suppressed = reserve(fingerprint) ?: run {
            android.util.Log.i("sk.diagnostics", "capture_suppressed_by_budget")
            return
        }
        val event = SentryEvent().apply {
            this.exceptions = exceptions
            level = SentryLevel.FATAL
            fingerprints = listOf(fingerprint)
            setExtra("error_budget.suppressed", suppressed)
        }
        val hint = HintUtils.createWithTypeCheckHint(CrashAdmission(epoch))
        val captured = Sentry.captureEvent(event, hint)
        android.util.Log.i("sk.diagnostics", "capture_result=$captured")
        Sentry.flush(1500)
    }

    // Hourly admission and per-fingerprint suppression are independently
    // bounded. Unreported suppression expires after 24 hours, so historical
    // fingerprints cannot permanently exhaust the current hourly budget.
    private fun reserve(fingerprint: String): Long? = synchronized(lock) {
        val prefs = context?.getSharedPreferences(PREFS, Context.MODE_PRIVATE) ?: return null
        val now = System.currentTimeMillis()
        val all = prefs.all
        val entries = all.filterKeys { it.startsWith("crash_") }
        val expired = entries.filterValues { value -> value is Long && now >= value && now - value >= 3_600_000L }
        val live = entries - expired.keys
        val suppressionKeys = all.keys.filter { it.startsWith("suppressed_") && !it.startsWith("suppressed_at_") }
        val expiredSuppressions = suppressionKeys.filter { key ->
            val timestamp = prefs.getLong("suppressed_at_" + key.removePrefix("suppressed_"), 0L)
            now >= timestamp && now - timestamp >= 86_400_000L
        }.toSet()
        val retainedSuppressionCount = suppressionKeys.size - expiredSuppressions.size
        val key = "crash_$fingerprint"
        val suppressionKey = "suppressed_$fingerprint"
        val suppressionTimeKey = "suppressed_at_$fingerprint"
        val edit = prefs.edit()
        expired.keys.forEach { edit.remove(it) }
        expiredSuppressions.forEach {
            edit.remove(it)
            edit.remove("suppressed_at_" + it.removePrefix("suppressed_"))
        }
        if (live.containsKey(key)) {
            if (suppressionKey in suppressionKeys && suppressionKey !in expiredSuppressions || retainedSuppressionCount < 128) {
                val count = if (suppressionKey in expiredSuppressions) 0L else prefs.getLong(suppressionKey, 0L).coerceIn(0, 1_000_000)
                edit.putLong(suppressionKey, (count + 1).coerceAtMost(1_000_000)).putLong(suppressionTimeKey, now)
            } else {
                val dropped = prefs.getLong("capacity_dropped", 0L).coerceIn(0, 1_000_000)
                edit.putLong("capacity_dropped", (dropped + 1).coerceAtMost(1_000_000))
            }
            edit.commit()
            return null
        }
        if (live.size >= 128) {
            val dropped = prefs.getLong("capacity_dropped", 0L).coerceIn(0, 1_000_000)
            edit.putLong("capacity_dropped", (dropped + 1).coerceAtMost(1_000_000)).commit()
            return null
        }
        val suppressed = if (suppressionKey in expiredSuppressions) 0L else prefs.getLong(suppressionKey, 0L).coerceIn(0, 1_000_000)
        if (!edit.putLong(key, now).remove(suppressionKey).remove(suppressionTimeKey).commit()) return null
        suppressed
    }
    private class PrivateTransport(private val options: SentryOptions, private val details: RequestDetails) : ITransport {
        private val calls = mutableSetOf<Call>()
        private val callLock = Any()
        private val client = OkHttpClient.Builder().followRedirects(false).followSslRedirects(false)
            .callTimeout(5, TimeUnit.SECONDS).build()
        private val worker = ThreadPoolExecutor(1, 1, 0, TimeUnit.SECONDS, ArrayBlockingQueue(1))
        @Volatile private var closed = false
        private val generation = AtomicLong()
        override fun send(envelope: SentryEnvelope, hint: Hint) {
            if (closed || !reporting || !policyAllows()) return
            val items = envelope.items.toList()
            if (items.size != 1 || items.single().header.type != SentryItemType.Event) return
            val admittedGeneration = (HintUtils.getSentrySdkHint(hint) as? CrashAdmission)?.epoch ?: return
            if (generation.get() != admittedGeneration) return
            runCatching { worker.execute {
                runCatching sendAttempt@{
                    if (closed || generation.get() != admittedGeneration || !reporting || !policyAllows()) return@sendAttempt
                    val output = LimitedOutput(64 * 1024)
                    options.serializer.serialize(envelope, output)
                    if (closed || generation.get() != admittedGeneration || !reporting || !policyAllows()) return@sendAttempt
                    val request = Request.Builder().url(details.url).apply {
                        details.headers.forEach { (key, value) -> header(key, value) }
                    }.post(output.bytes().toRequestBody("application/x-sentry-envelope".toMediaType())).build()
                    val call = NetworkUseGuard.create({ client.newCall(request) }, { it.cancel() })
                    synchronized(callLock) { if (closed || generation.get() != admittedGeneration || !reporting) call.cancel() else calls.add(call) }
                    try { if (!call.isCanceled()) call.execute().use { response ->
                        android.util.Log.i("sk.diagnostics", "delivery_status=${response.code}")
                    } } finally {
                        synchronized(callLock) { calls.remove(call) }
                        NetworkUseGuard.untrack(call)
                    }
                }.onFailure { logFailure("delivery", it) }
            } }
        }
        fun epoch(): Long = generation.get()
        fun cancel() { generation.incrementAndGet(); synchronized(callLock) { calls.forEach { it.cancel() }; calls.clear() }; worker.queue.clear() }
        override fun flush(timeoutMillis: Long) {
            runCatching { worker.submit {}.get(timeoutMillis.coerceAtMost(1500), TimeUnit.MILLISECONDS) }
        }
        override fun getRateLimiter(): io.sentry.transport.RateLimiter? = null
        override fun close() { close(false) }
        override fun close(isRestarting: Boolean) { closed = true; cancel(); worker.shutdownNow() }
    }

    private class LimitedOutput(private val limit: Int) : OutputStream() {
        private val output = ByteArrayOutputStream()
        override fun write(b: Int) { check(output.size() < limit); output.write(b) }
        override fun write(b: ByteArray, off: Int, len: Int) { check(len <= limit - output.size()); output.write(b, off, len) }
        fun bytes(): ByteArray = output.toByteArray()
    }
}
