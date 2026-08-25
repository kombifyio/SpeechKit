package io.kombify.speechkit.net

/**
 * Where dictation audio goes. Tier order per the Android track:
 * system on-device recognizer (zero-config floor, B-M2b) → own
 * speechkit-server → BYOK provider streaming (B-M6) → local in-app models
 * (B-M7).
 */
sealed interface ConnectionProfile {
    /**
     * Zero-config default floor (B-M2b): the platform `SpeechRecognizer`,
     * on-device where available. No server, no keys; the session records its
     * own audio (`StreamingSttSession.capturesOwnAudio`).
     *
     * @param preferOffline sets `RecognizerIntent.EXTRA_PREFER_OFFLINE` on
     *   the pre-API-31 fallback path; the API 31+ on-device recognizer is
     *   offline by construction.
     */
    data class SystemOnDevice(val preferOffline: Boolean = true) : ConnectionProfile

    /**
     * A speechkit-server reachable over the network.
     *
     * @param baseUrl e.g. "https://speechkit.example.com" or "http://192.168.1.10:8080"
     * @param bearerToken service token for auth modes bearer / bearer_or_edge /
     *   bearer_or_oidc; null for auth mode "none" (LAN setups).
     */
    data class Server(
        val baseUrl: String,
        val bearerToken: String? = null,
    ) : ConnectionProfile {
        /** Base URL without trailing slashes, ready for path concatenation. */
        val normalizedBaseUrl: String get() = baseUrl.trim().trimEnd('/')

        /**
         * REST path on this profile. Origin SpeechKit is `/v1/dictation/...`.
         * A Companion-provisioned Gateway root ends with `/v1/speechkit`, so
         * the same origin path becomes `/v1/speechkit/dictation/...`.
         */
        fun endpoint(originPath: String): String {
            val base = normalizedBaseUrl
            val path = originPath.trimStart('/')
            return if (base.endsWith("/v1/speechkit")) {
                "$base/${path.removePrefix("v1/")}"
            } else {
                "$base/$path"
            }
        }
    }

    /** Direct provider streaming with user-supplied keys. Lands in B-M6. */
    data object Byok : ConnectionProfile

    /** Fully on-device recognition. Lands in B-M7. */
    data object Local : ConnectionProfile
}
