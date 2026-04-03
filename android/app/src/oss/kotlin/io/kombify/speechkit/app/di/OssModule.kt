package io.kombify.speechkit.app.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.app.stt.AndroidSttRouterConfigurator
import io.kombify.speechkit.stt.SttRouter
import javax.inject.Singleton

/**
 * OSS flavor DI bindings.
 * Uses a user-supplied HuggingFace token for hosted STT without managed integrations.
 */
@Module
@InstallIn(SingletonComponent::class)
object OssModule {

    @Provides
    @Singleton
    fun provideSttRouter(configurator: AndroidSttRouterConfigurator): SttRouter =
        configurator.createRouter(SttRouter.RoutingStrategy.CLOUD_ONLY)
}
