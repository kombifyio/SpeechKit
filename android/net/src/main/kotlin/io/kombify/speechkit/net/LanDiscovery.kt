package io.kombify.speechkit.net

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

/** Parse the TXT attributes a SpeechKit announcer publishes. */
object LanDiscoveryRecords {
    const val SERVICE_TYPE: String = "_speechkit._tcp."

    private val CREDENTIAL_KEYS = setOf("token", "auth", "password", "secret", "bearer")

    fun parse(instanceName: String, attributes: Map<String, String>): LanServer? {
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
     * Parse `key=value` TXT lines as hashicorp/mdns emits them. Credential
     * keys are dropped so a misconfigured announcer cannot land a secret in
     * the found-server list.
     */
    fun parseTxtLines(instanceName: String, lines: Iterable<String>): LanServer? {
        val attributes = linkedMapOf<String, String>()
        for (line in lines) {
            val eq = line.indexOf('=')
            if (eq <= 0) continue
            val key = line.substring(0, eq).trim()
            if (key.lowercase() in CREDENTIAL_KEYS) continue
            attributes[key] = line.substring(eq + 1)
        }
        return parse(instanceName, attributes)
    }

    const val TXT_URL: String = "url"
    const val TXT_MODES: String = "modes"
    const val TXT_VERSION: String = "version"
}
