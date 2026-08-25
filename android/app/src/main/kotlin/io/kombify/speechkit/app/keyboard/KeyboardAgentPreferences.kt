package io.kombify.speechkit.app.keyboard

import android.content.Context

/**
 * Which Voice Agent backend the strip's single agent key opens.
 *
 * Deepgram / AssemblyAI / GPT stay reachable from the expanded more-toolbar
 * for tester work; the always-visible strip only has room for one agent key.
 */
object KeyboardAgentPreferences {

    const val PREFS_NAME: String = "speechkit_keyboard"
    const val KEY_PROVIDER: String = "voice_agent_provider"

    const val PROVIDER_DEEPGRAM: String = "deepgram"
    const val PROVIDER_ASSEMBLYAI: String = "assemblyai"
    const val PROVIDER_OPENAI: String = "openai"

    fun provider(context: Context): String {
        val stored = context
            .getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getString(KEY_PROVIDER, PROVIDER_OPENAI)
            ?: PROVIDER_OPENAI
        return when (stored) {
            PROVIDER_DEEPGRAM, PROVIDER_ASSEMBLYAI, PROVIDER_OPENAI -> stored
            else -> PROVIDER_OPENAI
        }
    }

    fun setProvider(context: Context, provider: String) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_PROVIDER, provider)
            .apply()
    }

    fun action(context: Context): KeyboardAction = when (provider(context)) {
        PROVIDER_DEEPGRAM -> KeyboardAction.AgentDeepgram
        PROVIDER_ASSEMBLYAI -> KeyboardAction.AgentAssemblyAi
        else -> KeyboardAction.AgentOpenAi
    }
}
