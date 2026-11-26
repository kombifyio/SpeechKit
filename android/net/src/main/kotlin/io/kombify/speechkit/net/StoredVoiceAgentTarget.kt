package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.storageScope

/** Target is bound to its origin and cloud-session generation; no lease or bearer is stored here. */
class StoredVoiceAgentTarget(private val context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("speechkit_voice_target", Context.MODE_PRIVATE)
    private fun key(profile: ConnectionProfile.Server): String = profile.storageScope()
    fun read(profile: ConnectionProfile): RegisteredAgentTarget? {
        if (profile !is ConnectionProfile.Server) return null
        val prefix = key(profile)
        val agent = prefs.getString("$prefix.agent", null) ?: return null
        val session = prefs.getString("$prefix.session", null) ?: return null
        val harness = prefs.getString("$prefix.harness", null) ?: return null
        val surface = prefs.getString("$prefix.surface", null) ?: return null
        return RegisteredAgentTarget(agent, session, harness, surface)
    }
    fun clear(profile: ConnectionProfile) {
        if (profile !is ConnectionProfile.Server) return
        val prefix = key(profile)
        check(prefs.edit().remove("$prefix.agent").remove("$prefix.session").remove("$prefix.harness").remove("$prefix.surface").commit()) { "Could not clear account target" }
    }
    fun write(profile: ConnectionProfile.Server, target: RegisteredAgentTarget) {
        val prefix = key(profile)
        check(StoredServerProfile.withActiveProfile(context, profile) {
            prefs.edit().putString("$prefix.agent", target.agentId).putString("$prefix.session", target.aiSessionId)
                .putString("$prefix.harness", target.harnessId).putString("$prefix.surface", target.composerSurface).apply()
        }) { "Account session changed" }
    }
}
