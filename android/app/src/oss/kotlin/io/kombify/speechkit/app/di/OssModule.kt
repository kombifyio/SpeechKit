package io.kombify.speechkit.app.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.stt.SttRouter
import javax.inject.Singleton

/**
 * OSS flavor DI bindings.
 * Fresh installs stay local; explicitly configured connections use the shared profile source.
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

    /** Device-local by default; explicitly configured routes use the shared selection. */
    @Provides
    @Singleton
    fun provideConnectionProfileSource(
        @ApplicationContext context: Context,
        companion: io.kombify.speechkit.app.companion.CompanionProvisioner,
    ): ConnectionProfileSource =
        connectionProfiles(context, companion)
}
