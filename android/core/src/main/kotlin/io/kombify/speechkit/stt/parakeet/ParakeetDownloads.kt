package io.kombify.speechkit.stt.parakeet

import android.content.Context
import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * The one Parakeet model download a process runs, observable from any screen.
 * It runs in a process-wide scope, so leaving the screen that started it does
 * not stop it; if the process dies, the next [start] resumes from the bytes on
 * disk ([ParakeetModelStore]).
 */
object ParakeetDownloads {
    sealed interface State {
        data object Idle : State
        data class Running(val modelId: String, val doneBytes: Long, val totalBytes: Long) : State
        data class Failed(val modelId: String, val message: String) : State
        data class Done(val modelId: String) : State
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val _state = MutableStateFlow<State>(State.Idle)
    private var job: Job? = null

    val state: StateFlow<State> = _state.asStateFlow()

    /**
     * Starts downloading [model] unless a download is already running. A device that cannot run Parakeet
     * ([ParakeetOnDevice.support]) gets [State.Failed] instead of a download it could never use.
     */
    @Synchronized
    fun start(context: Context, model: ParakeetModel = ParakeetModels.ULTRA) {
        if (job?.isActive == true) return
        val support = ParakeetOnDevice.support(context)
        if (support is ParakeetOnDevice.Support.Unsupported) {
            _state.value = State.Failed(model.id, support.reason)
            return
        }
        val store = ParakeetOnDevice.modelStore(context)
        _state.value = State.Running(model.id, 0, model.sizeBytes)
        job = scope.launch {
            try {
                var reported = -1L
                store.download(model) { done, total ->
                    // About one update per megabyte keeps the UI smooth without
                    // flooding it.
                    if (done - reported >= PROGRESS_STEP || done == total) {
                        reported = done
                        _state.value = State.Running(model.id, done, total)
                    }
                }
                ParakeetOnDevice.recognizers.clearFailure(store.directory(model))
                VoiceLog.i(VoiceLog.DICTATION, "parakeet download done model=${model.id}")
                _state.value = State.Done(model.id)
            } catch (e: CancellationException) {
                _state.value = State.Idle
                throw e
            } catch (e: Exception) {
                VoiceLog.w(VoiceLog.DICTATION, "parakeet download failed model=${model.id}", e)
                _state.value = State.Failed(model.id, e.message ?: e.javaClass.simpleName)
            }
        }
    }

    /**
     * Stops a running download; the bytes so far stay for the next [start]. A failure reported earlier is cleared,
     * so a paused or removed download does not keep showing it.
     */
    @Synchronized
    fun cancel() {
        job?.cancel()
        job = null
        if (_state.value is State.Failed) _state.value = State.Idle
    }

    private const val PROGRESS_STEP = 1L shl 20
}
