package io.kombify.speechkit.app.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.net.ConnectionProfile
import io.kombify.speechkit.net.ConnectionProfileSource
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.stt.HuggingFaceProvider
import io.kombify.speechkit.stt.SttRouter
import javax.inject.Singleton

/**
 * kombify product flavor DI bindings.
 * Includes cloud providers, auth, and hybrid routing.
 *
 * HF token is injected at runtime from Doppler/SecureStorage,
 * not hardcoded. This module sets up the router with cloud support.
 */
@Module
@InstallIn(SingletonComponent::class)
object KombifyModule {

    @Provides
    @Singleton
    fun provideSttRouter(): SttRouter {
        val router = SttRouter(
            strategy = SttRouter.RoutingStrategy.DYNAMIC,
            preferLocalUnderSecs = 10.0,
            parallelCloud = false,
        )
        // Cloud providers are added at runtime when tokens become available.
        // See SpeechKitEngineService for token resolution.
        return router
    }

    /**
     * B-M2b flavor default: system on-device STT until a speechkit-server is
     * configured/paired (the dev screen writes the same prefs today; B-M4's
     * SettingsRepository replaces the prefs read). Resolved on every session
     * open, so a server configured while the IME service is alive wins
     * without a rebind.
     */
    @Provides
    @Singleton
    fun provideConnectionProfileSource(
        @ApplicationContext context: Context,
    ): ConnectionProfileSource = ConnectionProfileSource {
        StoredServerProfile.load(context) ?: ConnectionProfile.SystemOnDevice()
    }
}
