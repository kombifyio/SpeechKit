package io.kombify.speechkit.app.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.kombify.speechkit.ai.LlmRegistry
import io.kombify.speechkit.ai.TextActions
import io.kombify.speechkit.audio.AndroidAudioSession
import io.kombify.speechkit.audio.AudioSession
import io.kombify.speechkit.shortcuts.DefaultShortcutResolver
import io.kombify.speechkit.shortcuts.ShortcutResolver
import io.kombify.speechkit.app.companion.BinderCoinstallTurnTransport
import io.kombify.speechkit.app.companion.CoinstallTurnClient
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.app.companion.CompanionProvision
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.app.companion.recoverIndependentCloud
import io.kombify.speechkit.assistant.intent.CloudSessionRecovery
import io.kombify.speechkit.net.KombifyCloudAuth
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.assistant.intent.CompanionTurnExecutor
import io.kombify.speechkit.store.RoomStore
import io.kombify.speechkit.store.Store
import javax.inject.Singleton

/**
 * Shared DI bindings for both OSS and kombify flavors.
 * Flavor-specific bindings live in oss/di/ and kombify/di/.
 */
@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    @Provides
    @Singleton
    fun provideCompanionProvisioner(@ApplicationContext context: Context): CompanionProvisioner =
        CompanionProvisioner(context)

    @Provides
    @Singleton
    fun provideCoinstallTurnClient(
        @ApplicationContext context: Context,
        companion: CompanionProvisioner,
    ): CoinstallTurnClient = CoinstallTurnClient(
        sessionPresent = { companion.currentSession() != null },
        transport = BinderCoinstallTurnTransport(context),
    )

    @Provides
    @Singleton
    fun provideCompanionTurnExecutor(client: CoinstallTurnClient): CompanionTurnExecutor = client

    @Provides
    @Singleton
    fun provideCloudSessionRecovery(
        @ApplicationContext context: Context,
        companion: CompanionProvisioner,
    ): CloudSessionRecovery =
        CloudSessionRecovery { failed ->
            val fromCompanion =
                (companion.recoverFromUnauthorized(failed) as? CompanionProvision.Session)?.profile
            if (fromCompanion != null) {
                StoredServerProfile.saveKombifyCloud(
                    context,
                    fromCompanion.baseUrl,
                    fromCompanion.bearerToken,
                )
                return@CloudSessionRecovery fromCompanion
            }
            val clientId = ShippedDefaults.cloudAuthClientId
            val refreshed = recoverIndependentCloud(
                failed = failed,
                stored = StoredServerProfile.loadCloud(context),
                refreshToken = StoredServerProfile.loadCloudRefreshToken(context),
            ) { refreshToken ->
                if (clientId.isEmpty()) {
                    null
                } else {
                    runCatching {
                        kotlinx.coroutines.runBlocking {
                            KombifyCloudAuth(KombifyCloudAuth.Config(clientId = clientId))
                                .refresh(refreshToken)
                        }.accessToken
                    }.getOrNull()
                }
            }
            if (refreshed != null) {
                StoredServerProfile.saveKombifyCloud(
                    context,
                    refreshed.baseUrl,
                    refreshed.bearerToken,
                    StoredServerProfile.loadCloudRefreshToken(context),
                )
            }
            refreshed
        }

    @Provides
    @Singleton
    fun provideAudioSession(): AudioSession = AndroidAudioSession()

    @Provides
    @Singleton
    fun provideStore(@ApplicationContext context: Context): Store = RoomStore(context)

    @Provides
    @Singleton
    fun provideShortcutResolver(): ShortcutResolver = DefaultShortcutResolver()

    @Provides
    @Singleton
    fun provideLlmRegistry(): LlmRegistry = LlmRegistry()

    @Provides
    @Singleton
    fun provideTextActions(llmRegistry: LlmRegistry): TextActions = TextActions(llmRegistry)
}
