package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.restrictTo
import android.os.Build
import android.speech.SpeechRecognizer
import io.kombify.speechkit.domain.ExecutionLocation
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.describe
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.stt.parakeet.LocalParakeetSession
import io.kombify.speechkit.stt.parakeet.ParakeetOnDevice
import io.kombify.speechkit.stt.streaming.KeepAliveSession
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.LocalSpeechAudio
import io.kombify.speechkit.stt.LocalSpeechLimits
import io.kombify.speechkit.stt.streaming.VocabularySession
import io.kombify.speechkit.stt.system.SystemSpeechRecognizerSession
import kotlinx.coroutines.CancellationException
import okhttp3.OkHttpClient

/**
 * Entry point for dictation sessions on a [ConnectionProfile]. Picks the best
 * available tier: the sessionless system recognizer for
 * [ConnectionProfile.SystemOnDevice]; for [ConnectionProfile.Server] the
 * streaming WS when the server offers it, batch fallback otherwise; for
 * [ConnectionProfile.Local] the on-device Parakeet model. Shared by the in-app
 * test screen, the Voice IME, and the system assistant overlay.
 *
 * @param context required for [ConnectionProfile.SystemOnDevice] (the
 *   platform recognizer binds through it) and [ConnectionProfile.Local] (the
 *   model lives in app storage); unused by the network tiers.
 */
class DictationController(
    private val profile: ConnectionProfile,
    private val okHttp: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    private val context: Context? = null,
    private val requireOnDevice: Boolean = false,
    private val policyOverride: io.kombify.speechkit.domain.ExecutionPolicy? = null,
    private val localSpeech: LocalSpeechRouter? = context?.let(LocalSpeechAdapters::router),
) {

    var executionLocation: ExecutionLocation? = null
        private set
    var fallbackReason: String? = null
        private set
    var localEngineId: String? = null
        private set

    /**
     * Opens a live [StreamingSttSession] for [profile]. Callers should check
     * [StreamingSttSession.capturesOwnAudio] before starting their own
     * AudioRecord capture — the system tier records the mic itself.
     *
     * @throws SpeechKitApiException when a server session mint is rejected
     *   (unauthenticated, per-user limit, server down).
     * @throws IllegalStateException for [ConnectionProfile.SystemOnDevice]
     *   or [ConnectionProfile.Local] without a [context], and for
     *   [ConnectionProfile.Local] when no usable Parakeet model is installed
     *   ([ParakeetOnDevice.usableFiles]).
     * @throws NotImplementedError for tiers that land in later milestones.
     */
    suspend fun openSession(options: DictationSegmentOptions = DictationSegmentOptions()): StreamingSttSession {
        if (profile is ConnectionProfile.Server || profile is ConnectionProfile.Byok || profile is ConnectionProfile.Firebase) {
            context?.let { StoredConnectionSelection(it).requireCurrent(profile) }
        }
        val policy = context?.let { StoredExecutionPolicy(it).read(profile).restrictTo(policyOverride) }
        val revision = context?.let { StoredConnectionSelection(it).revision() }
        val localAllowed = policy?.mode != io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY
        val systemAvailable = localAllowed && context != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
            SpeechRecognizer.isOnDeviceRecognitionAvailable(context)
        val explicitSystem = systemAvailable && context != null && OnDeviceEngine.choice(context) == OnDeviceEngine.Choice.SYSTEM
        val bufferedLocal = localAllowed && !explicitSystem && localSpeech?.available(options) == true
        val nativeFiles = if (localAllowed && !bufferedLocal && context != null)
            ParakeetOnDevice.usableFiles(context, options.language ?: "multi") else null
        val localProfile = context?.takeIf { localAllowed }?.let { app ->
            when {
                explicitSystem -> ConnectionProfile.SystemOnDevice()
                bufferedLocal -> ConnectionProfile.Local
                (profile is ConnectionProfile.Local || OnDeviceEngine.choice(app) == OnDeviceEngine.Choice.PARAKEET) &&
                    nativeFiles != null -> ConnectionProfile.Local
                systemAvailable ->
                    ConnectionProfile.SystemOnDevice()
                nativeFiles != null -> ConnectionProfile.Local
                else -> null
            }
        }
        val firebaseOnline = if (profile is ConnectionProfile.Firebase && context != null) {
            val manager = context.getSystemService(android.net.ConnectivityManager::class.java)
            manager?.getNetworkCapabilities(manager.activeNetwork)?.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true
        } else true
        val remoteAvailable = profile is ConnectionProfile.Server || (profile is ConnectionProfile.Firebase &&
            (firebaseOnline || policy?.mode == io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY))
        val candidates = policy?.candidates(localProfile != null, remoteAvailable)
        fun requireInvocation() {
            if (context != null && StoredConnectionSelection(context).revision() != revision)
                throw CancellationException("Connection changed")
        }
        suspend fun remotePcm(pcm: ByteArray, options: DictationSegmentOptions): String {
            requireInvocation()
            val wav = WavEncoder.pcm16ToWav(pcm, 16_000)
            try {
                return when (profile) {
                    is ConnectionProfile.Firebase -> FirebaseAudioTranscriber(profile, checkNotNull(context)).transcribe(wav, "audio/wav", options.language)
                    is ConnectionProfile.Server -> SpeechKitServerApi(profile, okHttp, context = context).transcribe(wav,
                        language = options.language, model = options.model, prompt = options.promptHint, keyterms = options.keyterms).text
                    else -> throw io.kombify.speechkit.domain.ExecutionFailure(io.kombify.speechkit.domain.ExecutionFailureKind.UNAVAILABLE, "No cloud transcription adapter")
                }
            } catch (error: CancellationException) { throw error }
              catch (error: Exception) {
                if (offlineFallbackAllowed(error)) throw io.kombify.speechkit.domain.ExecutionFailure(io.kombify.speechkit.domain.ExecutionFailureKind.TRANSIENT, "Speech endpoint unavailable", error)
                throw error
            } finally { wav.fill(0) }
        }
        suspend fun localPcm(pcm: ByteArray, options: DictationSegmentOptions): String {
            requireInvocation()
            val router = localSpeech ?: throw io.kombify.speechkit.domain.ExecutionFailure(io.kombify.speechkit.domain.ExecutionFailureKind.UNAVAILABLE, "No replayable local speech adapter")
            val result = router.transcribe(LocalSpeechAudio.Pcm16(pcm), options)
            requireInvocation()
            localEngineId = result.engineId
            if (result.fallbackFrom != null) fallbackReason = "local_adapter_unavailable"
            return result.text
        }
        fun localSession(): StreamingSttSession = when {
            bufferedLocal -> BatchDictationSession.withPcmTranscriber(LocalSpeechLimits.MAX_PCM_BYTES) { pcm, segment ->
                val remote: (suspend () -> String)? = if (policy?.mode == io.kombify.speechkit.domain.ExecutionMode.PREFER_DEVICE && remoteAvailable) ({ remotePcm(pcm, segment) }) else null
                val result = io.kombify.speechkit.domain.executeGeneration(
                    if (remote != null) checkNotNull(policy) else io.kombify.speechkit.domain.ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY),
                    { localPcm(pcm, segment) }, remote,
                )
                requireInvocation()
                executionLocation = result.location
                if (result.fallbackReason != null) fallbackReason = "local_adapters_unavailable"
                BatchTranscribeResponse(text = result.value)
            }
            localProfile is ConnectionProfile.SystemOnDevice -> SystemSpeechRecognizerSession(checkNotNull(context), requireOnDevice = true)
            localProfile is ConnectionProfile.Local -> LocalParakeetSession(checkNotNull(nativeFiles))
            else -> throw IllegalStateException("No permitted local speech adapter")
        }
        fun endpointBatch(): StreamingSttSession = BatchDictationSession.withPcmTranscriber(FirebaseAudioTranscriber.MAX_AUDIO_BYTES - 44) { pcm, segment ->
            val result = io.kombify.speechkit.domain.executeGeneration(
                policy ?: io.kombify.speechkit.domain.ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY, true),
                if (localSpeech != null && policy?.mode == io.kombify.speechkit.domain.ExecutionMode.PREFER_ENDPOINT) ({ localPcm(pcm, segment) }) else null,
                { remotePcm(pcm, segment) },
            )
            requireInvocation()
            executionLocation = result.location
            if (result.fallbackReason != null) fallbackReason = "endpoint_unavailable"
            BatchTranscribeResponse(text = result.value)
        }
        val selected = when (candidates?.firstOrNull()) {
            ExecutionLocation.DEVICE -> localProfile!!
            ExecutionLocation.ENDPOINT -> profile
            null -> if (policy != null) throw IllegalStateException("No permitted dictation capability available") else profile
        }
        executionLocation = if (selected is ConnectionProfile.Server || selected is ConnectionProfile.Firebase) ExecutionLocation.ENDPOINT else ExecutionLocation.DEVICE
        fallbackReason = null
        VoiceLog.i(VoiceLog.DICTATION, "open ${selected.describe()}")
        return try {
            val opened = when (selected) {
                is ConnectionProfile.SystemOnDevice ->
                    SystemSpeechRecognizerSession(
                        context = checkNotNull(context) {
                            "ConnectionProfile.SystemOnDevice requires a Context " +
                                "(DictationController(context = ...))"
                        },
                        preferOffline = selected.preferOffline,
                        requireOnDevice = requireOnDevice || policy != null,
                    )

                is ConnectionProfile.Server -> {
                    val api = SpeechKitServerApi(selected, okHttp, context = context)
                    val mint = try {
                        api.createDictationStreamSession()
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        // Server down or no network: dictate with the ready offline model instead of failing.
                        // A rejected sign-in (401/403) or a plan denial is never masked: the caller's recovery
                        // (token refresh, sign-in prompt, plan notice) must run, so those always propagate.
                        if (!offlineFallbackAllowed(e)) throw e
                        val app = context ?: throw e
                        val fallbackProfile = if (policy == null) {
                            if (OnDeviceEngine.choice(app) == OnDeviceEngine.Choice.PARAKEET && ParakeetOnDevice.usableFiles(app) != null) ConnectionProfile.Local else null
                        } else if (candidates?.contains(ExecutionLocation.DEVICE) == true) localProfile else null
                        if (fallbackProfile == null) throw e
                        val local = localSession()
                        executionLocation = ExecutionLocation.DEVICE
                        fallbackReason = "endpoint_unavailable"
                        return finish(local, onDevice = true)
                    }
                    if (mint.capabilities.streaming) {
                        // KeepAliveSession for the idle watchdog; RetryingDictationSession
                        // rescues a dead stream via one batch POST. The system tier is
                        // unwrapped — no transport, no watchdog.
                        KeepAliveSession(
                            RetryingDictationSession(
                                delegate = DictationWsClient(okHttp).connect(mint, context?.let { StoredConnectionSelection(it) }, selected.takeIf { context != null }),
                                batchSessionFactory = { endpointBatch() },
                            ),
                        )
                    } else {
                        runCatching { api.deleteDictationStreamSession(mint.sessionId) }
                        VoiceLog.i(VoiceLog.DICTATION, "server has no streaming; using batch")
                        endpointBatch()
                    }
                }

                is ConnectionProfile.Firebase -> endpointBatch()

                is ConnectionProfile.Byok ->
                    throw IllegalStateException("A ready on-device recognizer is required for dictation with Gemini Live")

                ConnectionProfile.Local -> localSession()
            }
            finish(opened, selected is ConnectionProfile.SystemOnDevice || selected is ConnectionProfile.Local)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            VoiceLog.e(VoiceLog.DICTATION, "open failed ${profile.describe()}", e)
            throw e
        }
    }

    /**
     * Wraps [opened] with the account vocabulary: hosted tiers get it as keyterms (the server rewrites the result
     * itself); on-device tiers rewrite their own results and the system recognizer is biased with the terms.
     */
    private fun finish(
        opened: StreamingSttSession,
        onDevice: Boolean = opened is LocalParakeetSession,
    ): StreamingSttSession {
        val session = VocabularySession(
            delegate = opened,
            rewriteResults = onDevice,
            keytermsWithAliases = !onDevice,
        )
        VoiceLog.i(
            VoiceLog.DICTATION,
            "opened capturesOwnAudio=${session.capturesOwnAudio}",
        )
        return session
    }

    /** Quick reachability probe for settings/onboarding UI. */
    suspend fun serverHealthy(): Boolean = when (profile) {
        is ConnectionProfile.Server -> SpeechKitServerApi(profile, okHttp, context = context).healthy()
        else -> false
    }
}

/**
 * Only an unreachable or failing server lets dictation fall back to the offline model: a network error or
 * a 5xx. Sign-in (401/403) and plan denials stay with the caller, which knows how to recover or explain.
 */
private fun offlineFallbackAllowed(e: Exception): Boolean = when (e) {
    is SpeechKitApiException -> e.denial == null && e.httpStatus >= 500
    is java.io.IOException -> true
    else -> false
}
