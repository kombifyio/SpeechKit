package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.describe
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.stt.parakeet.LocalParakeetSession
import io.kombify.speechkit.stt.parakeet.ParakeetOnDevice
import io.kombify.speechkit.stt.streaming.KeepAliveSession
import io.kombify.speechkit.stt.streaming.StreamingSttSession
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
) {

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
    suspend fun openSession(): StreamingSttSession {
        VoiceLog.i(VoiceLog.DICTATION, "open ${profile.describe()}")
        return try {
            val opened = when (profile) {
                is ConnectionProfile.SystemOnDevice ->
                    SystemSpeechRecognizerSession(
                        context = checkNotNull(context) {
                            "ConnectionProfile.SystemOnDevice requires a Context " +
                                "(DictationController(context = ...))"
                        },
                        preferOffline = profile.preferOffline,
                    )

                is ConnectionProfile.Server -> {
                    val api = SpeechKitServerApi(profile, okHttp)
                    val mint = try {
                        api.createDictationStreamSession()
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        // Server down or no network: dictate with the ready offline model instead of failing.
                        // A rejected sign-in (401/403) or a plan denial is never masked: the caller's recovery
                        // (token refresh, sign-in prompt, plan notice) must run, so those always propagate.
                        if (!offlineFallbackAllowed(e)) throw e
                        val files = context
                            ?.takeIf { OnDeviceEngine.choice(it) == OnDeviceEngine.Choice.PARAKEET }
                            ?.let(ParakeetOnDevice::usableFiles)
                            ?: throw e
                        VoiceLog.w(
                            VoiceLog.DICTATION,
                            "server session failed (${(e as? SpeechKitApiException)?.let { "HTTP ${it.httpStatus} ${it.code}" } ?: e.javaClass.simpleName}), using the offline model",
                        )
                        return finish(LocalParakeetSession(files))
                    }
                    if (mint.capabilities.streaming) {
                        // KeepAliveSession for the idle watchdog; RetryingDictationSession
                        // rescues a dead stream via one batch POST. The system tier is
                        // unwrapped — no transport, no watchdog.
                        KeepAliveSession(
                            RetryingDictationSession(
                                delegate = DictationWsClient(okHttp).connect(mint),
                                batchSessionFactory = { BatchDictationSession(api) },
                            ),
                        )
                    } else {
                        runCatching { api.deleteDictationStreamSession(mint.sessionId) }
                        VoiceLog.i(VoiceLog.DICTATION, "server has no streaming; using batch")
                        BatchDictationSession(api)
                    }
                }

                ConnectionProfile.Byok ->
                    throw NotImplementedError("BYOK provider streaming lands in B-M6")

                ConnectionProfile.Local -> {
                    val appContext = checkNotNull(context) {
                        "ConnectionProfile.Local requires a Context (DictationController(context = ...))"
                    }
                    val files = checkNotNull(ParakeetOnDevice.usableFiles(appContext)) {
                        "no usable on-device model: download one first, on a device that can run it"
                    }
                    LocalParakeetSession(files)
                }
            }
            finish(opened, profile is ConnectionProfile.SystemOnDevice || profile is ConnectionProfile.Local)
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
        is ConnectionProfile.Server -> SpeechKitServerApi(profile, okHttp).healthy()
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
