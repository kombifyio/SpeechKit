package io.kombify.speechkit.app.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.kombify.speechkit.app.config.HuggingFaceTokenStore
import io.kombify.speechkit.app.stt.AndroidSttRouterConfigurator
import io.kombify.speechkit.store.ListOpts
import io.kombify.speechkit.store.QuickNote
import io.kombify.speechkit.store.Stats
import io.kombify.speechkit.store.Store
import io.kombify.speechkit.store.Transcription
import io.kombify.speechkit.stt.SttRouter
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import javax.inject.Inject

@HiltViewModel
class MainViewModel @Inject constructor(
    private val store: Store,
    private val tokenStore: HuggingFaceTokenStore,
    private val sttRouter: SttRouter,
    private val sttConfigurator: AndroidSttRouterConfigurator,
) : ViewModel() {

    private val _stats = MutableStateFlow(Stats())
    val stats: StateFlow<Stats> = _stats.asStateFlow()

    private val _transcriptions = MutableStateFlow<List<Transcription>>(emptyList())
    val transcriptions: StateFlow<List<Transcription>> = _transcriptions.asStateFlow()

    private val _quickNotes = MutableStateFlow<List<QuickNote>>(emptyList())
    val quickNotes: StateFlow<List<QuickNote>> = _quickNotes.asStateFlow()

    private val _hfToken = MutableStateFlow(tokenStore.getToken().orEmpty())
    val hfToken: StateFlow<String> = _hfToken.asStateFlow()

    private val _hfTokenSaved = MutableStateFlow(_hfToken.value.isNotEmpty())
    val hfTokenSaved: StateFlow<Boolean> = _hfTokenSaved.asStateFlow()

    private val _hfTokenError = MutableStateFlow<String?>(null)
    val hfTokenError: StateFlow<String?> = _hfTokenError.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            try {
                _stats.value = store.stats()
                _transcriptions.value = store.listTranscriptions(ListOpts(limit = 100))
                _quickNotes.value = store.listQuickNotes(ListOpts(limit = 100))
            } catch (e: Exception) {
                Timber.e(e, "Failed to load store data")
            }
        }
    }

    fun deleteQuickNote(id: Long) {
        viewModelScope.launch {
            try {
                store.deleteQuickNote(id)
                refresh()
            } catch (e: Exception) {
                Timber.e(e, "Failed to delete quick note %d", id)
            }
        }
    }

    fun togglePinQuickNote(note: QuickNote) {
        viewModelScope.launch {
            try {
                store.pinQuickNote(note.id, !note.pinned)
                refresh()
            } catch (e: Exception) {
                Timber.e(e, "Failed to toggle pin for quick note %d", note.id)
            }
        }
    }

    fun updateHuggingFaceToken(value: String) {
        _hfToken.value = value
        _hfTokenSaved.value = false
        _hfTokenError.value = null
    }

    fun saveHuggingFaceToken() {
        viewModelScope.launch {
            val normalized = _hfToken.value.trim()
            try {
                tokenStore.saveToken(normalized)
                sttConfigurator.refreshCloudProviders(sttRouter)
                _hfToken.value = normalized
                _hfTokenSaved.value = normalized.isNotEmpty()
                _hfTokenError.value = null
            } catch (e: Exception) {
                Timber.e(e, "Failed to save HuggingFace token")
                _hfTokenSaved.value = false
                _hfTokenError.value = "Token konnte nicht sicher gespeichert werden"
            }
        }
    }

    fun clearHuggingFaceToken() {
        viewModelScope.launch {
            try {
                tokenStore.clearToken()
                sttConfigurator.refreshCloudProviders(sttRouter)
                _hfToken.value = ""
                _hfTokenSaved.value = false
                _hfTokenError.value = null
            } catch (e: Exception) {
                Timber.e(e, "Failed to clear HuggingFace token")
                _hfTokenError.value = "Token konnte nicht entfernt werden"
            }
        }
    }
}
