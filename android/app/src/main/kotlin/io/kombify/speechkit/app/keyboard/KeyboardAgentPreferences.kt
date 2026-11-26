package io.kombify.speechkit.app.keyboard

import android.content.Context
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.storageScope
import io.kombify.speechkit.net.StoredConnectionSelection

/**
 * Which Voice Agent backend the strip's single agent key opens.
 *
 * Deepgram / AssemblyAI / GPT stay reachable from the expanded more-toolbar
 * for tester work; the always-visible strip only has room for one agent key.
 * Grok (xAI) is reachable through that strip key when Settings selects it; it
 * needs a server with Grok Voice enabled.
 */
object KeyboardAgentPreferences {

    const val PREFS_NAME: String = "speechkit_keyboard"
    const val KEY_PROVIDER: String = "voice_agent_provider"
    const val KEY_PROFILE_ID: String = "voice_agent_profile_id"
    const val KEY_MODEL: String = "voice_agent_model"
    private const val KEY_SCOPE = "voice_agent_connection_scope"

    const val PROVIDER_DEEPGRAM: String = "deepgram"
    const val PROVIDER_ASSEMBLYAI: String = "assemblyai"
    const val PROVIDER_OPENAI: String = "openai"
    const val PROVIDER_GEMINI: String = "gemini"

    /** xAI Grok Voice; the server maps it to its realtime.xai.grok-voice profile. */
    const val PROVIDER_XAI: String = "xai"

    data class Choice(val profileId: String = "", val provider: String? = null, val model: String = "")

    /** One snapshot, scoped to the captured endpoint custody identity, never another route's choice. */
    fun choice(context: Context, connection: ConnectionProfile = StoredConnectionSelection(context).snapshot().profile): Choice {
        val stored = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE).all
        if (BuildConfig.CONNECTION_SETUP_ENABLED && (scope(connection) == null || stored[KEY_SCOPE] != scope(connection))) return Choice()
        return Choice((stored[KEY_PROFILE_ID] as? String).orEmpty(),
            storedOrDefault(stored[KEY_PROVIDER] as? String), (stored[KEY_MODEL] as? String).orEmpty())
    }

    fun provider(context: Context): String? = choice(context).provider

    /** The default remains Deepgram; catalog provider aliases are retained for future dispatch. */
    fun storedOrDefault(stored: String?): String = stored
        ?.trim()
        ?.takeIf { PROVIDER_ID.matches(it) }
        ?: PROVIDER_DEEPGRAM

    fun profileId(context: Context): String = choice(context).profileId

    fun model(context: Context): String = choice(context).model

    /** Persists the catalog identity and exact model override used by the Voice Agent start frame. */
    fun setProfile(
        context: Context,
        profile: io.kombify.speechkit.net.VoiceAgentProfile,
        model: String = profile.modelId.orEmpty(),
        connection: ConnectionProfile,
    ): Boolean {
        require(profile.mode == "voice_agent" && profile.isNativeRealtimeEndpoint) { "Profile is not a native realtime Voice Agent endpoint" }
        require(profile.id.isNotBlank()) { "Voice Agent profile has no catalog id" }
        val selectedModel = model.trim()
        require(selectedModel.isNotEmpty()) { "Voice Agent profile has no model to select" }
        require(selectedModel == profile.modelId || profile.variants.any { it.modelId == selectedModel }) {
            "Model is not a variant of the selected Voice Agent profile"
        }
        val provider = requireNotNull(profile.provider).trim()
        require(PROVIDER_ID.matches(provider)) { "Voice Agent profile has an invalid provider alias" }
        val identity = requireNotNull(scope(connection)) { "Voice Agent choice needs an admitted connection" }
        return runCatching { StoredConnectionSelection(context).withCurrent(connection) {
            context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_SCOPE, identity)
            .putString(KEY_PROFILE_ID, profile.id)
            .putString(KEY_PROVIDER, provider)
            .putString(KEY_MODEL, selectedModel)
            .commit()
        } }.getOrDefault(false)
    }

    fun setProvider(context: Context, provider: String) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_PROVIDER, provider)
            .remove(KEY_PROFILE_ID)
            .remove(KEY_MODEL)
            .remove(KEY_SCOPE)
            .apply()
    }

    fun action(context: Context): KeyboardAction = when (provider(context)) {
        PROVIDER_ASSEMBLYAI, "assembly-ai" -> KeyboardAction.AgentAssemblyAi
        PROVIDER_OPENAI, "gpt-live", "foundry", "foundry-gpt-live", "foundry-voicelive" -> KeyboardAction.AgentOpenAi
        PROVIDER_XAI -> KeyboardAction.AgentXai
        PROVIDER_GEMINI, "google" -> KeyboardAction.AgentGemini
        else -> KeyboardAction.AgentDeepgram
    }

    private val PROVIDER_ID = Regex("[a-z0-9][a-z0-9_-]{0,63}")

    private fun scope(connection: ConnectionProfile): String? = when (connection) {
        is ConnectionProfile.Server -> connection.storageScope()
        is ConnectionProfile.Byok -> "byok:${connection.credentialId}:${connection.model}"
        is ConnectionProfile.Firebase -> "firebase:${connection.applicationId}:${connection.projectId}:${connection.liveModel}"
        else -> null
    }
}
