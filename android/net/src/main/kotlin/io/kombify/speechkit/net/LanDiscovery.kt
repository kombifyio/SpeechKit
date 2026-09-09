package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile

/**
 * A SpeechKit server found on the LAN via DNS-SD `_speechkit._tcp`.
 *
 * The TXT record is the public contract from `internal/server/discovery`:
 * `url`, `modes`, `version`. Credentials never belong here; auth happens
 * after the user picks a URL.
 */
data class LanServer(
    val instanceName: String,
    val url: String,
    val modes: List<String> = emptyList(),
    val version: String = "",
)

/** A LAN pick ready to persist as [ConnectionMode.SELF_HOST]. */
data class LanSelfHostSelection(
    val profile: ConnectionProfile.Server,
    val mode: ConnectionMode = ConnectionMode.SELF_HOST,
)

/** Parse the TXT attributes a SpeechKit announcer publishes. */
object LanDiscoveryRecords {
    const val SERVICE_TYPE: String = "_speechkit._tcp."

    private val CREDENTIAL_KEYS = setOf("token", "auth", "password", "secret", "bearer")

    fun carriesCredentials(attributes: Map<String, String>): Boolean =
        attributes.keys.any { it.lowercase() in CREDENTIAL_KEYS }

    fun parse(instanceName: String, attributes: Map<String, String>): LanServer? {
        if (carriesCredentials(attributes)) return null
        val url = attributes[TXT_URL]?.trim().orEmpty()
        if (url.isEmpty()) return null
        val modes = attributes[TXT_MODES].orEmpty()
            .split(',')
            .map { it.trim() }
            .filter { it.isNotEmpty() }
        return LanServer(
            instanceName = instanceName,
            url = url,
            modes = modes,
            version = attributes[TXT_VERSION].orEmpty().trim(),
        )
    }

    /**
     * Parse `key=value` TXT lines as hashicorp/mdns emits them. An
     * announcement that carried credentials is rejected whole.
     */
    fun parseTxtLines(instanceName: String, lines: Iterable<String>): LanServer? {
        val attributes = linkedMapOf<String, String>()
        for (line in lines) {
            val eq = line.indexOf('=')
            if (eq <= 0) continue
            val key = line.substring(0, eq).trim()
            attributes[key] = line.substring(eq + 1)
        }
        return parse(instanceName, attributes)
    }

    /**
     * A credential-free announcement becomes a self-host profile. Pairing
     * material is the caller's typed token, never the TXT record.
     */
    fun selfHostSelection(
        instanceName: String,
        attributes: Map<String, String>,
        token: String? = null,
    ): LanSelfHostSelection? {
        val found = parse(instanceName, attributes) ?: return null
        return LanSelfHostSelection(
            profile = ConnectionProfile.Server(found.url, token?.trim()?.ifEmpty { null }),
        )
    }

    const val TXT_URL: String = "url"
    const val TXT_MODES: String = "modes"
    const val TXT_VERSION: String = "version"
}
