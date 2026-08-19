package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.stt.streaming.KeepAliveSession
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.system.SystemSpeechRecognizerSession
import okhttp3.OkHttpClient

/**
 * Entry point for dictation sessions on a [ConnectionProfile]. Picks the best
 * available tier: the sessionless system recognizer for
 * [ConnectionProfile.SystemOnDevice]; for [ConnectionProfile.Server] the
 * streaming WS when the server offers it, batch fallback otherwise. Shared by
 * the in-app test screen and the Voice IME (B-M2/M2b).
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
    suspend fun openSession(): StreamingSttSession = when (profile) {
        is ConnectionProfile.SystemOnDevice ->
            // Deliberately returned unwrapped. KeepAliveSession exists for
            // the server's idle watchdog — this tier is sessionless and its
            // keepAlive() is already a no-op returning true. And
            // RetryingDictationSession rescues a transport-level
            // `ws_failure` through a batch POST — there is no transport here
            // and no server to post a rescue to.
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
                // Wrapped here rather than in each consumer: this is the one
                // factory both the dev screen and the Voice IME (B-M2) go
                // through. RetryingDictationSession rescues a segment whose
                // stream died mid-flight via one batch POST; KeepAliveSession
                // keeps a warm socket alive between mic presses, which the
                // server's idle watchdog would otherwise end.
                KeepAliveSession(
                    RetryingDictationSession(
                        delegate = DictationWsClient(okHttp).connect(mint),
                        batchSessionFactory = { BatchDictationSession(api) },
                    ),
                )
            } else {
                // The minted stream session will never be attached — release
                // it immediately or it pins a per-user concurrency slot on
                // the server until the ticket-TTL sweep.
                runCatching { api.deleteDictationStreamSession(mint.sessionId) }
                BatchDictationSession(api)
            }
        }

        ConnectionProfile.Byok ->
            throw NotImplementedError("BYOK provider streaming lands in B-M6")

        ConnectionProfile.Local ->
            throw NotImplementedError("Local in-app model recognition lands in B-M7")
    }

    /** Quick reachability probe for settings/onboarding UI. */
    suspend fun serverHealthy(): Boolean = when (profile) {
        is ConnectionProfile.Server -> SpeechKitServerApi(profile, okHttp).healthy()
        else -> false
    }
}
