package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.FormBody
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException

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

    suspend fun waitForTokens(
        started: DeviceAuthorization,
        sleeper: suspend (Long) -> Unit,
    ): Tokens? {
        val deadline = System.currentTimeMillis() + started.expiresInSec * 1000L
        var intervalMs = started.intervalSec.coerceAtLeast(1) * 1000L
        while (System.currentTimeMillis() < deadline) {
            sleeper(intervalMs)
            when (val outcome = poll(started.deviceCode)) {
                is Poll.Ready -> return outcome.tokens
                Poll.Pending -> Unit
                Poll.SlowDown -> intervalMs += 1000L
                Poll.Expired, Poll.Denied -> return null
            }
        }
        return null
    }

    suspend fun refresh(refreshToken: String): Tokens = withContext(Dispatchers.IO) {
        val body = FormBody.Builder()
            .add("grant_type", "refresh_token")
            .add("refresh_token", refreshToken)
            .add("client_id", config.clientId)
            .build()
        when (val outcome = exchange(body)) {
            is Poll.Ready -> outcome.tokens
            else -> throw IOException("refresh did not return tokens")
        }
    }

    private fun exchange(body: FormBody): Poll {
        val request = Request.Builder()
            .url(config.endpoint("/oauth/token"))
            .post(body)
            .header("Accept", "application/json")
            .build()
        client.newCall(request).execute().use { response ->
            val raw = response.body?.string().orEmpty()
            if (response.isSuccessful) {
                val parsed = tokenAdapter.fromJson(raw)
                    ?: throw IOException("token endpoint returned no body")
                val access = parsed.accessToken.trim()
                if (access.isEmpty()) throw IOException("token endpoint returned no access_token")
                return Poll.Ready(
                    Tokens(
                        accessToken = access,
                        refreshToken = parsed.refreshToken?.trim()?.ifEmpty { null },
                        expiresInSec = parsed.expiresIn.takeIf { it > 0 } ?: 3600,
                    ),
                )
            }
            val err = errorAdapter.fromJson(raw)?.error.orEmpty()
            return when (err) {
                "authorization_pending" -> Poll.Pending
                "slow_down" -> Poll.SlowDown
                "expired_token" -> Poll.Expired
                "access_denied" -> Poll.Denied
                else -> throw IOException("token endpoint HTTP ${response.code}")
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
    }
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
