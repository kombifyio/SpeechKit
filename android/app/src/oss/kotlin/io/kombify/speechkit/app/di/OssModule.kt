package io.kombify.speechkit.app.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
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
}
