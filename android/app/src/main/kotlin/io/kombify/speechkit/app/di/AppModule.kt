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
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.app.companion.recoverCloudSession
import io.kombify.speechkit.app.companion.voiceRenewalProfile
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.net.VoiceRenewalProfileSource
import io.kombify.speechkit.assistant.intent.CloudSessionRecovery
import io.kombify.speechkit.assistant.intent.CompanionTurnExecutor
import io.kombify.speechkit.assistant.service.NeuralReplyVoice
import io.kombify.speechkit.app.voice.NeuralVoiceEngine
import io.kombify.speechkit.net.StoredServerProfile
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
    fun provideCompanionTurnExecutor(
        @ApplicationContext context: Context,
        client: CoinstallTurnClient,
    ): CompanionTurnExecutor = object : CompanionTurnExecutor by client {
        // Privacy rule: an assistant transcript goes to the hosted Companion only while the person chose
        // the hosted Cloud connection. On-device, SpeechKit origin and self-hosted dictation keep the transcript on the
        // device (local Assist). Within Cloud, the Companion is the default route unless this device signed
        // in on its own, which may be another account. The keyboard's explicit Companion action keeps its
        // separate client.
        override fun available(): Boolean = companionRouteAllowed(context) && client.available()
    }

    /** The hosted Cloud connection is selected and its session came from the Companion, not a separate sign-in. */
    private fun companionRouteAllowed(context: Context): Boolean =
        StoredServerProfile.cloudSelected(context) &&
            StoredServerProfile.loadCloud(context) != null &&
            !StoredServerProfile.hasOwnCloudSession(context)

    @Provides
    @Singleton
    fun provideCloudSessionRecovery(
        @ApplicationContext context: Context,
        companion: CompanionProvisioner,
    ): CloudSessionRecovery =
        CloudSessionRecovery { failed -> recoverCloudSession(context, companion, failed) }

    @Provides
    @Singleton
    fun provideVoiceRenewalProfileSource(
        @ApplicationContext context: Context,
        companion: CompanionProvisioner,
        profiles: ConnectionProfileSource,
    ): VoiceRenewalProfileSource = object : VoiceRenewalProfileSource {
        override val claimNamespace: String = ShippedDefaults.tokenClaimNamespace

        override suspend fun profileForRenewal(original: ConnectionProfile): ConnectionProfile =
            voiceRenewalProfile(context, companion, profiles, original, claimNamespace)
    }

    /** The optional downloaded neural voice for assistant replies; the platform voice is its fallback. */
    @Provides
    @Singleton
    fun provideNeuralReplyVoice(@ApplicationContext context: Context): NeuralReplyVoice =
        NeuralVoiceEngine.get(context)

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
