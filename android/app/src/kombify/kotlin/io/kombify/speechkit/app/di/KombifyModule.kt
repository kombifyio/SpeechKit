package io.kombify.speechkit.app.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.app.stt.AndroidSttRouterConfigurator
import io.kombify.speechkit.stt.SttRouter
import javax.inject.Singleton

/**
 * kombify product flavor DI bindings.
 * Uses the shared secure token store and keeps dynamic routing semantics
 * so local/on-device STT can be reintroduced without rewiring callers.
 */
@Module
@InstallIn(SingletonComponent::class)
object KombifyModule {

    @Provides
    @Singleton
    fun provideSttRouter(configurator: AndroidSttRouterConfigurator): SttRouter =
        configurator.createRouter(SttRouter.RoutingStrategy.DYNAMIC)
}
