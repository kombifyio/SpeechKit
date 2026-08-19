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
