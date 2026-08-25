package io.kombify.speechkit.app

import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import helium314.keyboard.latin.App
import helium314.keyboard.latin.SpeechKitVoiceBridge
import io.kombify.speechkit.app.keyboard.InlineVoicePanel
import io.kombify.speechkit.app.keyboard.SpeechKitStripLayout
import io.kombify.speechkit.net.ConnectionProfileSource
import dagger.hilt.android.HiltAndroidApp
import io.kombify.speechkit.BuildConfig
import timber.log.Timber

/**
 * Extends the keyboard's own [App] rather than [android.app.Application]:
 * this APK ships HeliBoard's IME, whose initialisation -- settings, subtypes,
 * RichInputMethodManager, the static the native dictionary loader reads --
 * all happens there, and an APK has exactly one Application class.
 */
@HiltAndroidApp
class SpeechKitApplication : App() {

    override fun onCreate() {
        super.onCreate()
        if (BuildConfig.DEBUG) {
            Timber.plant(Timber.DebugTree())
        }
        SpeechKitStripLayout.apply(this)
        // Answer the keyboard's voice key in place. Registered once, for the
        // process lifetime: the panel itself is built per activation and takes
        // the InputMethodService it is given, so it holds no service across
        // keyboard restarts.
        SpeechKitVoiceBridge.host = InlineVoicePanel(
            application = this,
            profileSource = EntryPointAccessors
                .fromApplication(this, KeyboardEntryPoint::class.java)
                .profileSource(),
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
    }
}
