package io.kombify.speechkit.app.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.stt.SttRouter
import javax.inject.Singleton

/**
 * OSS flavor DI bindings.
 * No cloud integration, no auth, offline-only routing.
 */
@Module
@InstallIn(SingletonComponent::class)
object OssModule {

    @Provides
    @Singleton
    fun provideSttRouter(): SttRouter = SttRouter(
        strategy = SttRouter.RoutingStrategy.LOCAL_ONLY,
        connectivityCheck = { false },
    )

    /**
     * B-M2b flavor default: the OSS build always dictates through the
     * platform's on-device recognizer — zero config, no server, no keys.
     * Server pairing for self-hosters arrives with B-M4's onboarding +
     * SettingsRepository; until then the dev screen can still exercise a
     * server profile explicitly.
     */
    @Provides
    @Singleton
    fun provideConnectionProfileSource(): ConnectionProfileSource =
        ConnectionProfileSource { ConnectionProfile.SystemOnDevice() }
}
