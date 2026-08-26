package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.describe
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.stt.streaming.KeepAliveSession
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.system.SystemSpeechRecognizerSession
import kotlinx.coroutines.CancellationException
import okhttp3.OkHttpClient

/**
 * Entry point for dictation sessions on a [ConnectionProfile]. Picks the best
 * available tier: the sessionless system recognizer for
 * [ConnectionProfile.SystemOnDevice]; for [ConnectionProfile.Server] the
 * streaming WS when the server offers it, batch fallback otherwise. Shared by
 * the in-app test screen, the Voice IME, and the system assistant overlay.
 *
 * @param context required for [ConnectionProfile.SystemOnDevice] (the
 *   platform recognizer binds through it); unused by the network tiers.
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
     *   without a [context].
     * @throws NotImplementedError for tiers that land in later milestones.
     */
    suspend fun openSession(): StreamingSttSession {
        VoiceLog.i(VoiceLog.DICTATION, "open ${profile.describe()}")
        return try {
            val session = when (profile) {
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
                    val mint = api.createDictationStreamSession()
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

                ConnectionProfile.Local ->
                    throw NotImplementedError("Local in-app model recognition lands in B-M7")
            }
            VoiceLog.i(
                VoiceLog.DICTATION,
                "opened capturesOwnAudio=${session.capturesOwnAudio}",
            )
            session
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            VoiceLog.e(VoiceLog.DICTATION, "open failed ${profile.describe()}", e)
            throw e
        }
    }

    /** Quick reachability probe for settings/onboarding UI. */
    suspend fun serverHealthy(): Boolean = when (profile) {
        is ConnectionProfile.Server -> SpeechKitServerApi(profile, okHttp).healthy()
        else -> false
    }
}
