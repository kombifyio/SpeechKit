package io.kombify.speechkit.app

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import io.kombify.speechkit.BuildConfig
import timber.log.Timber

@HiltAndroidApp
class SpeechKitApplication : Application() {

    override fun onCreate() {
        super.onCreate()
        if (BuildConfig.DEBUG) {
            Timber.plant(Timber.DebugTree())
        }
    }
}
