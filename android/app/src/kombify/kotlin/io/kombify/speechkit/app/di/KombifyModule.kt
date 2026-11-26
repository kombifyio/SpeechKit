package io.kombify.speechkit.app.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.domain.ConnectionProfileSource
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

    /**
     * Prefix of the account claims (tenant_id, org_id, organization_id) in
     * the bearers of the kombify login, read through ShippedDefaults.
     */
    const val TOKEN_CLAIM_NAMESPACE: String = "https://kombify.io/"

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
     * Persisted connection mode. Companion is applied only after an explicit
     * Connect. Re-resolved on every session open.
     */
    @Provides
    @Singleton
    fun provideConnectionProfileSource(
        @ApplicationContext context: Context,
        companion: CompanionProvisioner,
    ): ConnectionProfileSource = connectionProfiles(context, companion)

}
