package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import java.io.IOException

/** A Voice Agent profile projected from the canonical SpeechKit catalog. */
@JsonClass(generateAdapter = true)
data class VoiceAgentProfile(
    val id: String = "",
    val mode: String = "",
    val name: String = "",
    val provider: String? = null,
    val providerKind: String = "",
    val executionMode: String? = null,
    val modelId: String? = null,
    val adapterKind: String? = null,
    val capabilities: List<String> = emptyList(),
    val authRequirement: String? = null,
    val transport: String? = null,
    @Json(name = "inferenceAllowed") val inferenceAllowed: Boolean = false,
    val variants: List<VoiceAgentVariant> = emptyList(),
) {
    /** Whether the profile declares an actual native realtime WebSocket provider. */
    val isNativeRealtimeEndpoint: Boolean
        get() = providerKind == PROVIDER_KIND_DIRECT &&
            transport == TRANSPORT_WEBSOCKET &&
            inferenceAllowed &&
            CAPABILITY_REALTIME_AUDIO in capabilities &&
            !provider.isNullOrBlank()

    companion object {
        const val PROVIDER_KIND_DIRECT = "direct_provider"
        const val TRANSPORT_WEBSOCKET = "websocket"
        const val CAPABILITY_REALTIME_AUDIO = "realtime_audio"
    }
}

/** A selectable model or deployment variant of a Voice Agent profile. */
@JsonClass(generateAdapter = true)
data class VoiceAgentVariant(
    val id: String = "",
    val name: String = "",
    val modelId: String = "",
    val description: String? = null,
    val recommended: Boolean = false,
)

/** The profile catalog uses the same `{profiles:[...]}` envelope as the REST API. */
@JsonClass(generateAdapter = true)
data class VoiceAgentProfileCatalog(
    val profiles: List<VoiceAgentProfile> = emptyList(),
)

/** Runtime readiness for one profile on the selected SpeechKit server. */
@JsonClass(generateAdapter = true)
data class VoiceAgentReadiness(
    val schemaVersion: String? = null,
    val profileId: String = "",
    val mode: String = "",
    val providerKind: String = "",
    val active: Boolean = false,
    val default: Boolean = false,
    val configured: Boolean = false,
    val credentialsReady: Boolean = false,
    val runtimeReady: Boolean = false,
    val capabilityReady: Boolean = false,
    val ready: Boolean = false,
    val blockedByScope: Boolean = false,
    val disabledReasonId: String? = null,
    val missing: List<String> = emptyList(),
)

@JsonClass(generateAdapter = true)
data class VoiceAgentReadinessCatalog(
    val readiness: List<VoiceAgentReadiness> = emptyList(),
)

/** Local copy of the canonical profile projection for account-free setup. */
object VoiceAgentCatalog {
    private const val RESOURCE = "/speechkit-voice-profiles.json"
    private val adapter = DictationStreamCodec.defaultMoshi().adapter(VoiceAgentProfileCatalog::class.java)

    fun bundled(): List<VoiceAgentProfile> {
        val stream = VoiceAgentCatalog::class.java.getResourceAsStream(RESOURCE)
            ?: throw IllegalStateException("Bundled SpeechKit Voice Agent catalog is missing")
        return stream.use { input ->
            val catalog = adapter.fromJson(input.readBytes().toString(Charsets.UTF_8))
                ?: throw IOException("Bundled SpeechKit Voice Agent catalog is empty")
            require(catalog.profiles.isNotEmpty()) { "Bundled SpeechKit Voice Agent catalog has no profiles" }
            catalog.profiles
        }
    }

    /**
     * Filters a server's catalog to native realtime profiles that the same
     * server currently reports ready. Catalog membership alone is metadata,
     * not evidence that an endpoint is configured or usable.
     */
    fun eligibleEndpointProfiles(
        profiles: List<VoiceAgentProfile>,
        readiness: List<VoiceAgentReadiness>,
    ): List<VoiceAgentProfile> {
        val readyIds = readiness.asSequence()
            .filter { it.mode == "voice_agent" && it.ready }
            .map { it.profileId }
            .toSet()
        return profiles.filter { it.isNativeRealtimeEndpoint && it.id in readyIds }
    }
}
