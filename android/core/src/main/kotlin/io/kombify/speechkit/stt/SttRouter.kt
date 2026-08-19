package io.kombify.speechkit.stt

import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.withTimeoutOrNull
import timber.log.Timber
import kotlin.time.Duration.Companion.seconds

/**
 * STT provider router with dynamic strategy selection.
 *
 * Mirrors: internal/router/router.go Router struct.
 * Extended with Android-specific signals (connectivity, battery, thermal).
 */
class SttRouter(
    private var local: SttProvider? = null,
    private val cloud: MutableList<SttProvider> = mutableListOf(),
    val strategy: RoutingStrategy = RoutingStrategy.DYNAMIC,
    val preferLocalUnderSecs: Double = 10.0,
    val parallelCloud: Boolean = false,
    private val connectivityCheck: suspend () -> Boolean = { true },
) {
    enum class RoutingStrategy { DYNAMIC, LOCAL_ONLY, CLOUD_ONLY }

    fun setLocal(provider: SttProvider?) {
        local = provider
    }

    fun addCloud(provider: SttProvider) {
        cloud.add(provider)
    }

    fun setCloud(name: String, provider: SttProvider?) {
        val idx = cloud.indexOfFirst { it.name == name }
        if (idx >= 0) {
            if (provider == null) cloud.removeAt(idx) else cloud[idx] = provider
        } else if (provider != null) {
            cloud.add(provider)
        }
    }

    fun availableProviders(): List<String> = buildList {
        local?.let { add("local") }
        cloud.forEach { add(it.name) }
    }

    /** Route audio to the best available provider. Mirrors router.go Route(). */
    suspend fun route(audio: ByteArray, durationSecs: Double, opts: TranscribeOpts): Result {
        return when (strategy) {
            RoutingStrategy.LOCAL_ONLY -> transcribeLocal(audio, opts)
            RoutingStrategy.CLOUD_ONLY -> transcribeCloud(audio, opts)
            RoutingStrategy.DYNAMIC -> transcribeDynamic(audio, durationSecs, opts)
        }
    }

    private suspend fun transcribeDynamic(audio: ByteArray, durationSecs: Double, opts: TranscribeOpts): Result {
        val online = connectivityCheck()

        // Case 1: No internet -- local only
        if (!online) {
            val localProvider = local
            if (localProvider != null) {
                Timber.d("No internet, using local provider")
                return localProvider.transcribe(audio, opts)
            }
            error("No internet and local provider not ready")
        }

        // Case 2: Local ready and short audio -- prefer local
        val localProvider = local
        if (localProvider != null && durationSecs < preferLocalUnderSecs) {
            if (parallelCloud && cloud.isNotEmpty()) {
                return transcribeParallel(audio, opts)
            }
            return try {
                localProvider.transcribe(audio, opts)
            } catch (e: Exception) {
                Timber.w(e, "Local transcribe failed, falling back to cloud")
                transcribeCloud(audio, opts)
            }
        }

        // Case 3: No local or long audio -- prefer cloud
        if (cloud.isNotEmpty()) {
            return try {
                transcribeCloud(audio, opts)
            } catch (e: Exception) {
                Timber.w(e, "Cloud transcribe failed")
                localProvider?.transcribe(audio, opts) ?: throw e
            }
        }

        // Case 4: Fallback to local
        return localProvider?.transcribe(audio, opts) ?: error("No STT provider available")
    }

    private suspend fun transcribeLocal(audio: ByteArray, opts: TranscribeOpts): Result {
        return local?.transcribe(audio, opts) ?: error("Local provider not configured")
    }

    private suspend fun transcribeCloud(audio: ByteArray, opts: TranscribeOpts): Result {
        for (provider in cloud) {
            return try {
                provider.transcribe(audio, opts)
            } catch (e: Exception) {
                Timber.w(e, "${provider.name} transcribe failed")
                continue
            }
        }
        error("No cloud provider available")
    }

    private suspend fun transcribeParallel(audio: ByteArray, opts: TranscribeOpts): Result = coroutineScope {
        val localDeferred = local?.let { async { runCatching { it.transcribe(audio, opts) } } }
        val cloudDeferred = async { runCatching { transcribeCloud(audio, opts) } }

        // Return first successful result
        val localResult = localDeferred?.let { withTimeoutOrNull(15.seconds) { it.await() } }
        if (localResult?.isSuccess == true) return@coroutineScope localResult.getOrThrow()

        val cloudResult = withTimeoutOrNull(15.seconds) { cloudDeferred.await() }
        if (cloudResult?.isSuccess == true) return@coroutineScope cloudResult.getOrThrow()

        localResult?.getOrThrow() ?: cloudResult?.getOrThrow() ?: error("All providers failed")
    }
}
