package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ConnectionProfile
import org.json.JSONObject
import java.util.Base64

/** Resolves a session's renewal profile on a background thread. */
interface VoiceRenewalProfileSource {
    /** Claim namespace of the bearers this source returns, see [sameVoiceRenewalAccount]. */
    val claimNamespace: String

    suspend fun profileForRenewal(original: ConnectionProfile): ConnectionProfile
}

/**
 * Restricts credential substitution using token claims. This does not verify a
 * bearer; the server that accepts it verifies it and the voice server checks
 * session ownership.
 *
 * [claimNamespace] is the prefix an issuer puts on its custom claims, such as
 * `https://issuer.example/` for `https://issuer.example/tenant_id`. A present
 * namespaced claim wins over the plain one; with "" only the plain claims
 * `tenant_id`, `org_id` and `organization_id` are read. There is no default:
 * a caller that passes "" for an issuer that only sends namespaced claims
 * compares issuer and subject alone (see android/CONTRACT.md).
 */
fun sameVoiceRenewalAccount(original: String?, next: String?, claimNamespace: String): Boolean {
    if (original == next) return true
    if (original.isNullOrBlank() || next.isNullOrBlank()) return false
    val identity = voiceRenewalIdentity(original, claimNamespace) ?: return false
    return identity == voiceRenewalIdentity(next, claimNamespace)
}

private fun voiceRenewalIdentity(token: String, claimNamespace: String): List<String?>? = runCatching {
    val payload = token.split('.').getOrNull(1) ?: return null
    val claims = JSONObject(String(Base64.getUrlDecoder().decode(payload), Charsets.UTF_8))
    fun claim(key: String): String? = (claims.opt(key) as? String)?.trim()?.takeIf { it.isNotEmpty() }
    fun projected(plain: String): String? {
        val namespaced = claimNamespace + plain
        return if (claimNamespace.isNotEmpty() && claims.has(namespaced) && !claims.isNull(namespaced)) {
            claim(namespaced)
        } else {
            claim(plain)
        }
    }
    val issuer = claim("iss") ?: return null
    val subject = claim("sub") ?: return null
    val tenant = projected("tenant_id")
    val organization = projected("org_id") ?: projected("organization_id") ?: tenant
    listOf(issuer, subject, organization, tenant)
}.getOrNull()
