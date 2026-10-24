package io.kombify.speechkit.app.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.net.OnDeviceEngine
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
     * B-M2b flavor default: the OSS build always dictates on the device —
     * zero config, no server, no keys — with the platform recognizer or, once
     * chosen and downloaded in Settings, the Parakeet model
     * ([OnDeviceEngine]). Server pairing for self-hosters arrives with B-M4's
     * onboarding + SettingsRepository; until then the dev screen can still
     * exercise a server profile explicitly.
     */
    @Provides
    @Singleton
    fun provideConnectionProfileSource(
        @ApplicationContext context: Context,
    ): ConnectionProfileSource =
        ConnectionProfileSource { OnDeviceEngine.profile(context) }
}
