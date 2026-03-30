package io.kombify.speechkit.app.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.kombify.speechkit.store.ListOpts
import io.kombify.speechkit.store.QuickNote
import io.kombify.speechkit.store.Stats
import io.kombify.speechkit.store.Store
import io.kombify.speechkit.store.Transcription
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import timber.log.Timber
import javax.inject.Inject

@HiltViewModel
class MainViewModel @Inject constructor(
    private val store: Store,
) : ViewModel() {

    private val _stats = MutableStateFlow(Stats())
    val stats: StateFlow<Stats> = _stats.asStateFlow()

    private val _transcriptions = MutableStateFlow<List<Transcription>>(emptyList())
    val transcriptions: StateFlow<List<Transcription>> = _transcriptions.asStateFlow()

    private val _quickNotes = MutableStateFlow<List<QuickNote>>(emptyList())
    val quickNotes: StateFlow<List<QuickNote>> = _quickNotes.asStateFlow()

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
}
