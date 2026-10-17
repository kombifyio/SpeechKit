package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.FormBody
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * RFC 8628 device grant against the kombify Auth0 tenant. SpeechKit signs
 * in through the user's browser; Companion is optional, never required.
 */
class KombifyCloudAuth(
    private val config: Config,
    private val client: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    moshi: Moshi = DictationStreamCodec.defaultMoshi(),
) {
    private val deviceAdapter = moshi.adapter(DeviceCodeResponse::class.java)
    private val tokenAdapter = moshi.adapter(TokenResponse::class.java)
    private val errorAdapter = moshi.adapter(OAuthError::class.java)

    // The refresh exchange gets a short total deadline; sign-in polling keeps
    // the shared client's timeouts.
    private val refreshClient: OkHttpClient = client.newBuilder()
        .callTimeout(REFRESH_CALL_TIMEOUT_SEC, TimeUnit.SECONDS)
        .build()

    data class Config(
        val clientId: String,
        val domain: String = DEFAULT_DOMAIN,
        val audience: String = DEFAULT_AUDIENCE,
        val scope: String = DEFAULT_SCOPE,
        val serverUrl: String = DEFAULT_SERVER_URL,
    ) {
        val configured: Boolean get() = clientId.trim().isNotEmpty()
        fun endpoint(path: String): String {
            val host = domain.trim().trimEnd('/')
            val origin = when {
                host.startsWith("https://") || host.startsWith("http://") -> host
                else -> "https://$host"
            }
            return origin + path
        }
    }

    data class DeviceAuthorization(
        val deviceCode: String,
        val userCode: String,
        val verificationUri: String,
        val verificationUriComplete: String,
        val expiresInSec: Int,
        val intervalSec: Int,
    )

    data class Tokens(
        val accessToken: String,
        val refreshToken: String?,
        val expiresInSec: Int,
    )

    sealed class Poll {
        data class Ready(val tokens: Tokens) : Poll()
        data object Pending : Poll()
        data object SlowDown : Poll()
        data object Expired : Poll()
        data object Denied : Poll()
    }

    /** How a whole device-grant wait ended. */
    sealed class SignIn {
        data class Done(val tokens: Tokens) : SignIn()

        /** The user declined in the browser. */
        data object Declined : SignIn()

        /** The device code ran out before the user finished. */
        data object Expired : SignIn()

        /** The token endpoint stayed unreachable or kept failing. */
        data object Unreachable : SignIn()
    }

    suspend fun start(): DeviceAuthorization = withContext(Dispatchers.IO) {
        if (!config.configured) throw IOException("kombify Cloud client id is not configured")
        val body = FormBody.Builder()
            .add("client_id", config.clientId)
            .add("scope", config.scope)
            .add("audience", config.audience)
            .build()
        val parsed = post(config.endpoint("/oauth/device/code"), body, deviceAdapter)
            ?: throw IOException("device endpoint returned no body")
        val deviceCode = parsed.deviceCode.trim()
        if (deviceCode.isEmpty()) throw IOException("device endpoint returned no device_code")
        val verify = parsed.verificationUriComplete.trim().ifEmpty { parsed.verificationUri.trim() }
        DeviceAuthorization(
            deviceCode = deviceCode,
            userCode = parsed.userCode.trim(),
            verificationUri = parsed.verificationUri.trim(),
            verificationUriComplete = verify,
            expiresInSec = parsed.expiresIn.takeIf { it > 0 } ?: 600,
            intervalSec = parsed.interval.takeIf { it > 0 } ?: 5,
        )
    }

    suspend fun poll(deviceCode: String): Poll = withContext(Dispatchers.IO) {
        val body = FormBody.Builder()
            .add("grant_type", DEVICE_GRANT)
            .add("device_code", deviceCode)
            .add("client_id", config.clientId)
            .build()
        exchange(body)
    }

    /**
     * Polls until the user finishes, declines or the code expires. One failed
     * poll (phone briefly offline, a 5xx) is retried at the normal interval;
     * only [MAX_CONSECUTIVE_POLL_FAILURES] in a row end the wait.
     */
    suspend fun awaitSignIn(
        started: DeviceAuthorization,
        sleeper: suspend (Long) -> Unit,
    ): SignIn {
        val deadline = System.currentTimeMillis() + started.expiresInSec * 1000L
        var intervalMs = started.intervalSec.coerceAtLeast(1) * 1000L
        var failures = 0
        while (System.currentTimeMillis() < deadline) {
            sleeper(intervalMs)
            val outcome = try {
                poll(started.deviceCode)
            } catch (e: CancellationException) {
                throw e
            } catch (e: IOException) {
                if (++failures >= MAX_CONSECUTIVE_POLL_FAILURES) return SignIn.Unreachable
                continue
            }
            failures = 0
            when (outcome) {
                is Poll.Ready -> return SignIn.Done(outcome.tokens)
                Poll.Pending -> Unit
                Poll.SlowDown -> intervalMs += 1000L
                Poll.Expired -> return SignIn.Expired
                Poll.Denied -> return SignIn.Declined
            }
        }
        return SignIn.Expired
    }

    /**
     * Trades a refresh token for fresh tokens. A rotated refresh token comes
     * back in [Tokens.refreshToken] and must replace the stored one. A
     * definitive refusal is [CloudRefresh.Rejected], including the policy
     * refusals `access_denied` and `expired_token` a login rule can answer
     * with; every transport or server problem is [CloudRefresh.Unavailable] so
     * a flaky network never signs the user out. The whole call is bounded by
     * [REFRESH_CALL_TIMEOUT_SEC]: the lifecycle holds a lock meanwhile, and a
     * stalled identity provider must not stall every request behind it.
     */
    suspend fun refreshSession(refreshToken: String): CloudRefresh = withContext(Dispatchers.IO) {
        val body = FormBody.Builder()
            .add("grant_type", "refresh_token")
            .add("refresh_token", refreshToken)
            .add("client_id", config.clientId)
            .build()
        try {
            when (val outcome = exchange(body, refreshClient)) {
                is Poll.Ready -> CloudRefresh.Refreshed(outcome.tokens)
                Poll.Denied, Poll.Expired -> CloudRefresh.Rejected
                Poll.Pending, Poll.SlowDown -> CloudRefresh.Unavailable
            }
        } catch (e: TokenEndpointException) {
            if (e.definitive) CloudRefresh.Rejected else CloudRefresh.Unavailable
        } catch (e: IOException) {
            CloudRefresh.Unavailable
        }
    }

    private fun exchange(body: FormBody, http: OkHttpClient = client): Poll {
        val request = Request.Builder()
            .url(config.endpoint("/oauth/token"))
            .post(body)
            .header("Accept", "application/json")
            .build()
        http.newCall(request).execute().use { response ->
            val raw = response.body?.string().orEmpty()
            if (response.isSuccessful) {
                val parsed = tokenAdapter.fromJson(raw)
                    ?: throw IOException("token endpoint returned no body")
                val access = parsed.accessToken.trim()
                if (access.isEmpty()) throw IOException("token endpoint returned no access_token")
                return Poll.Ready(
                    Tokens(
                        accessToken = access,
                        refreshToken = parsed.refreshToken?.trim()?.takeIf { it.isNotEmpty() },
                        expiresInSec = parsed.expiresIn.takeIf { it > 0 } ?: 3600,
                    ),
                )
            }
            val err = runCatching { errorAdapter.fromJson(raw)?.error }.getOrNull().orEmpty()
            return when (err) {
                "authorization_pending" -> Poll.Pending
                "slow_down" -> Poll.SlowDown
                "expired_token" -> Poll.Expired
                "access_denied" -> Poll.Denied
                else -> throw TokenEndpointException(response.code, err)
            }
        }
    }

    private fun <T> post(url: String, body: FormBody, adapter: com.squareup.moshi.JsonAdapter<T>): T? {
        val request = Request.Builder()
            .url(url)
            .post(body)
            .header("Accept", "application/json")
            .build()
        client.newCall(request).execute().use { response ->
            val raw = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                throw IOException("device endpoint HTTP ${response.code}")
            }
            return adapter.fromJson(raw)
        }
    }

    companion object {
        const val DEFAULT_DOMAIN: String = "login.kombify.io"
        const val DEFAULT_AUDIENCE: String = "https://api.kombify.io"
        const val DEFAULT_SERVER_URL: String = "https://api.kombify.io/v1/speechkit"
        const val DEFAULT_SCOPE: String = "openid profile email offline_access"
        const val DEVICE_GRANT: String = "urn:ietf:params:oauth:grant-type:device_code"
        const val MAX_CONSECUTIVE_POLL_FAILURES: Int = 3

        /** Total deadline of one refresh-token exchange. */
        const val REFRESH_CALL_TIMEOUT_SEC: Long = 10L

        /** Account pages opened in the system browser; they carry no token. */
        const val PLANS_URL: String = "https://app.kombify.io/dashboard/billing/compare"
        const val BILLING_URL: String = "https://app.kombify.io/dashboard/billing"
        const val DASHBOARD_URL: String = "https://app.kombify.io/dashboard"

        /** Account API on the gateway origin, called with the user's own bearer. */
        const val PROFILE_PATH: String = "/v1/cloud/profile"
        const val BUDGETS_PATH: String = "/v1/cloud/usage/budgets"
        const val FEATURE_DECISION_PATH: String = "/v1/entitlements/decide"

        /**
         * Gateway error codes meaning the plan refused the request, not a
         * fault. A budget refusal counts only as a 4xx: the same budget code
         * with a 5xx is the budget service being down.
         */
        val PLAN_ERROR_CODES: Set<String> = setOf(
            "speechkit_feature_not_entitled",
            "entitlement_denied",
            "speechkit_budget_unavailable",
            "speechkit_budget_exhausted",
        )
    }
}

/**
 * The token endpoint answered with a failure that is not part of the device
 * grant dance. A 4xx with an OAuth error (`invalid_grant` and friends) means
 * the credential itself is dead; 429 and 5xx are the service having a bad
 * moment.
 */
internal class TokenEndpointException(
    val httpStatus: Int,
    val oauthError: String,
) : IOException("token endpoint HTTP $httpStatus") {
    val definitive: Boolean
        get() = httpStatus in 400..403 && oauthError.isNotEmpty()
}

@JsonClass(generateAdapter = true)
internal data class DeviceCodeResponse(
    @Json(name = "device_code") val deviceCode: String = "",
    @Json(name = "user_code") val userCode: String = "",
    @Json(name = "verification_uri") val verificationUri: String = "",
    @Json(name = "verification_uri_complete") val verificationUriComplete: String = "",
    @Json(name = "expires_in") val expiresIn: Int = 0,
    val interval: Int = 0,
)

@JsonClass(generateAdapter = true)
internal data class TokenResponse(
    @Json(name = "access_token") val accessToken: String = "",
    @Json(name = "refresh_token") val refreshToken: String? = null,
    @Json(name = "expires_in") val expiresIn: Int = 0,
)

@JsonClass(generateAdapter = true)
internal data class OAuthError(val error: String = "")
