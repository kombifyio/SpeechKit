package io.kombify.speechkit.app.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
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
}
