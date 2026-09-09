package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile

/**
 * `speechkit.pairing.v1` as `/setup` emits it. Consuming a valid payload
 * yields a [ConnectionMode.SELF_HOST] session. Missing URL or token is not
 * a Cloud or tester-origin fallback.
 */
@JsonClass(generateAdapter = true)
data class PairingPayload(
    val v: String = "",
    @Json(name = "server_url") val serverUrl: String = "",
    val auth: String = "",
    val token: String = "",
    val name: String = "",
)

object PairingRecords {
    const val VERSION: String = "speechkit.pairing.v1"
    const val AUTH_BEARER: String = "bearer"

    private val adapter = Moshi.Builder().build().adapter(PairingPayload::class.java)

    fun consume(raw: String): LanSelfHostSelection? {
        val trimmed = raw.trim()
        if (trimmed.isEmpty()) return null
        val payload = runCatching { adapter.fromJson(trimmed) }.getOrNull() ?: return null
        return consume(payload)
    }

    fun consume(payload: PairingPayload): LanSelfHostSelection? {
        if (payload.v.trim() != VERSION) return null
        if (!payload.auth.trim().equals(AUTH_BEARER, ignoreCase = true)) return null
        val url = payload.serverUrl.trim().trimEnd('/')
        val token = payload.token.trim()
        if (url.isEmpty() || token.isEmpty()) return null
        return LanSelfHostSelection(
            profile = ConnectionProfile.Server(url, token),
            mode = ConnectionMode.SELF_HOST,
        )
    }
}
