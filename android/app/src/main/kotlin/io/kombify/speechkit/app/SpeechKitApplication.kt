package io.kombify.speechkit.app

import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import helium314.keyboard.latin.App
import helium314.keyboard.latin.SpeechKitVoiceBridge
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.app.companion.CoinstallTurnClient
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.app.keyboard.InlineVoicePanel
import io.kombify.speechkit.app.keyboard.SpeechKitStripLayout
import io.kombify.speechkit.domain.ConnectionProfileSource
import dagger.hilt.android.HiltAndroidApp
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.CloudBearer
import io.kombify.speechkit.net.CloudRefresh
import io.kombify.speechkit.net.CloudTokenLifecycle
import io.kombify.speechkit.net.KombifyCloudAuth
import io.kombify.speechkit.net.StoredCloudSession
import io.kombify.speechkit.stt.parakeet.ParakeetOnDevice
import timber.log.Timber

/**
 * Extends the keyboard's own [App] rather than [android.app.Application]:
 * this APK ships HeliBoard's IME, whose initialisation -- settings, subtypes,
 * RichInputMethodManager, the static the native dictionary loader reads --
 * all happens there, and an APK has exactly one Application class.
 */
@HiltAndroidApp
class SpeechKitApplication : App() {

    /** A backgrounded keyboard gives the on-device dictation model's memory (about 1 GB) back. */
    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        ParakeetOnDevice.onTrimMemory(level)
    }

    override fun onCreate() {
        super.onCreate()
        // On-device diagnosis: `adb logcat -s sk.voice`. VoiceLog always uses
        // that tag. DebugTree in debug; INFO+ on release so a tester APK still
        // shows mint/WS/capture failures without debug verbosity.
        if (BuildConfig.DEBUG) {
            Timber.plant(Timber.DebugTree())
        } else {
            Timber.plant(object : Timber.Tree() {
                override fun isLoggable(tag: String?, priority: Int) =
                    priority >= android.util.Log.INFO

                override fun log(priority: Int, tag: String?, message: String, t: Throwable?) {
                    val line = if (t != null) {
                        message + "\n" + android.util.Log.getStackTraceString(t)
                    } else {
                        message
                    }
                    android.util.Log.println(priority, tag ?: VoiceLog.TAG, line)
                }
            })
        }
        VoiceLog.i(VoiceLog.NET, "app start debug=${BuildConfig.DEBUG}")
        installCloudSessionLifecycle()
        SpeechKitStripLayout.apply(this)
        // Answer the keyboard's voice key in place. Registered once, for the
        // process lifetime: the panel itself is built per activation and takes
        // the InputMethodService it is given, so it holds no service across
        // keyboard restarts.
        val entry = EntryPointAccessors
            .fromApplication(this, KeyboardEntryPoint::class.java)
        SpeechKitVoiceBridge.host = InlineVoicePanel(
            application = this,
            profileSource = entry.profileSource(),
            companion = entry.companionProvisioner(),
            companionTurns = entry.companionTurns(),
        )
    }

    /**
     * One token lifecycle for every process entry point (Settings, keyboard,
     * assistant): requests ask it for the bearer, so the stored cloud
     * session is renewed shortly before it expires and signed out when its
     * refresh token is refused. Builds without a sign-in client id have no
     * session to renew and leave bearers untouched.
     */
    private fun installCloudSessionLifecycle() {
        val clientId = ShippedDefaults.cloudAuthClientId
        val auth = if (clientId.isEmpty()) {
            null
        } else {
            KombifyCloudAuth(KombifyCloudAuth.Config(clientId = clientId))
        }
        CloudBearer.install(
            CloudTokenLifecycle(
                store = StoredCloudSession(this),
                refresher = { refreshToken ->
                    auth?.refreshSession(refreshToken) ?: CloudRefresh.Unavailable
                },
            ),
        )
    }

    /**
     * The Application is injected by Hilt but cannot carry `@Inject` fields
     * itself, so the one dependency the keyboard bridge needs is pulled
     * through an entry point.
     */
    @EntryPoint
    @InstallIn(SingletonComponent::class)
    interface KeyboardEntryPoint {
        fun profileSource(): ConnectionProfileSource
        fun companionProvisioner(): CompanionProvisioner
        fun companionTurns(): CoinstallTurnClient
    }
}
