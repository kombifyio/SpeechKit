package io.kombify.speechkit.app.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.net.ConnectionProfile
import io.kombify.speechkit.net.ConnectionProfileSource
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.net.resolveConnectionProfile
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
     * Companion (logged-in user) → typed Settings override → tester hosted
     * SpeechKit → on-device. Re-resolved on every session open.
     */
    @Provides
    @Singleton
    fun provideConnectionProfileSource(
        @ApplicationContext context: Context,
    ): ConnectionProfileSource {
        val companion = CompanionProvisioner(context)
        companion.warm()
        return ConnectionProfileSource {
            resolveConnectionProfile(
                companion = companion.currentSession(),
                stored = StoredServerProfile.load(context),
                shipped = shippedDefaultProfile(),
            )
        }
    }

    /**
     * Hosted SpeechKit this kombify build dials when nobody has typed a
     * server and Companion has not provisioned a user token. Firebase
     * tester APKs also bake a shared, revocable bearer; a developer build
     * without that env var still has the origin so Settings can explain
     * where traffic would go.
     */
    private fun shippedDefaultProfile(): ConnectionProfile.Server? {
        val url = BuildConfig.DEFAULT_SERVER_URL.trim()
        if (url.isEmpty()) return null
        return ConnectionProfile.Server(
            url,
            BuildConfig.DEFAULT_SERVER_TOKEN.trim().ifEmpty { null },
        )
    }
}
