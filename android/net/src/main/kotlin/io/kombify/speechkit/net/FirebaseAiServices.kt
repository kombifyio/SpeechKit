package io.kombify.speechkit.net

import android.content.Context
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions
import com.google.firebase.ai.FirebaseAI
import com.google.firebase.ai.type.GenerativeBackend
import com.google.firebase.appcheck.FirebaseAppCheck
import com.google.firebase.appcheck.playintegrity.PlayIntegrityAppCheckProviderFactory

/** Firebase client identifiers, not an end-user Gemini Developer API key. */
data class FirebaseAiProject(val applicationId: String, val projectId: String, val apiKey: String,
    val allowCloud: Boolean = false, val debugAppCheck: Boolean = false)

object FirebaseAiServices {
    private val appCheckInstalled = mutableSetOf<String>()
    fun appName(config: FirebaseAiProject): String {
        val identity = listOf(config.applicationId, config.projectId, config.apiKey).joinToString("\u0000")
        val digest = java.security.MessageDigest.getInstance("SHA-256").digest(identity.toByteArray()).take(16).joinToString("") { "%02x".format(it) }
        return "speechkit-hybrid-${if (config.allowCloud) "cloud" else "device"}-$digest"
    }

    @Synchronized fun get(context: Context, config: FirebaseAiProject): FirebaseAI {
        require(config.applicationId.isNotBlank() && config.projectId.isNotBlank() && config.apiKey.isNotBlank())
        val name = appName(config)
        val app = FirebaseApp.getApps(context).firstOrNull { it.name == name }
            ?: FirebaseApp.initializeApp(context.applicationContext,
                FirebaseOptions.Builder().setApplicationId(config.applicationId).setProjectId(config.projectId).setApiKey(config.apiKey).build(), name)
        require(app.options.applicationId == config.applicationId && app.options.projectId == config.projectId && app.options.apiKey == config.apiKey) {
            "The active Firebase project differs from the requested project"
        }
        app.setDataCollectionDefaultEnabled(false)
        if (config.allowCloud && appCheckInstalled.add(name)) {
            val check = FirebaseAppCheck.getInstance(app)
            if (config.debugAppCheck) installDebugAppCheck(check)
            else check.installAppCheckProviderFactory(PlayIntegrityAppCheckProviderFactory.getInstance(), false)
        }
        return FirebaseAI.getInstance(app, GenerativeBackend.googleAI())
    }
}
