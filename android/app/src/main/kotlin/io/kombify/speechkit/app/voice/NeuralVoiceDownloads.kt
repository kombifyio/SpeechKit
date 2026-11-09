package io.kombify.speechkit.app.voice

import android.content.Context
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.stt.parakeet.ParakeetModelStore
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.apache.commons.compress.archivers.tar.TarArchiveInputStream
import org.apache.commons.compress.compressors.bzip2.BZip2CompressorInputStream
import java.io.BufferedInputStream
import java.io.File
import java.io.IOException

/**
 * The neural voices on disk. Reuses the resumable, SHA-256-checking [ParakeetModelStore] with an unpacker for the
 * voices' `.tar.bz2` archives; the voices install under `noBackupFilesDir`, out of cloud backups.
 */
internal object NeuralVoiceStore {
    @Volatile
    private var store: ParakeetModelStore? = null

    fun get(context: Context): ParakeetModelStore =
        store ?: synchronized(this) {
            store ?: ParakeetModelStore(
                root = File(context.applicationContext.noBackupFilesDir, "speechkit/models/neural-voice"),
                unpacker = { archive, dir, _ -> unpack(archive, dir) },
            ).also { store = it }
        }

    /**
     * Unpacks the whole voice archive into [dir], dropping the archive's single top-level directory. The store
     * checks the model and the tokens against their pinned hashes afterwards; the archive as a whole was verified
     * before this runs. Everything is unpacked beside the target first and moved in with the model and the tokens
     * last, so an interrupted unpack never leaves those two in place with the phoneme data missing: the store
     * treats a present, verified model as a finished unpack.
     */
    private fun unpack(archive: File, dir: File) {
        val root = dir.canonicalFile
        val scratch = File(root, ".unpack").canonicalFile
        scratch.deleteRecursively()
        scratch.mkdirs()
        TarArchiveInputStream(BZip2CompressorInputStream(BufferedInputStream(archive.inputStream(), 1 shl 16))).use { tar ->
            while (true) {
                val entry = tar.nextEntry ?: break
                val relative = entry.name.trimStart('/').substringAfter('/', missingDelimiterValue = "")
                if (relative.isEmpty()) continue
                val target = File(scratch, relative).canonicalFile
                // An entry that resolves outside the voice directory is refused rather than skipped.
                if (!target.path.startsWith(scratch.path + File.separator)) {
                    throw IOException("archive entry escapes the voice directory")
                }
                when {
                    entry.isDirectory -> target.mkdirs()
                    entry.isFile -> {
                        target.parentFile?.mkdirs()
                        target.outputStream().use { tar.copyTo(it) }
                    }
                    // Links and special files have no place in a voice.
                    else -> Unit
                }
            }
        }
        val children = scratch.listFiles().orEmpty()
            .sortedBy { it.name.endsWith(".onnx") || it.name == NeuralVoice.TOKENS_FILE }
        for (child in children) {
            val destination = File(root, child.name)
            destination.deleteRecursively()
            if (!child.renameTo(destination)) throw IOException("could not install ${child.name}")
        }
        scratch.deleteRecursively()
    }
}

/**
 * The one neural-voice download a process runs, observable from any screen; the same shape as
 * `ParakeetDownloads`. It runs in a process-wide scope, so leaving Settings does not stop it, and a download the
 * process did not finish resumes from the bytes on disk.
 */
internal object NeuralVoiceDownloads {
    sealed interface State {
        /** The model id of the voice the state is about; null for [Idle]. */
        val modelId: String? get() = null

        data object Idle : State
        data class Running(override val modelId: String, val doneBytes: Long, val totalBytes: Long) : State
        data class Failed(override val modelId: String, val message: String) : State
        data class Done(override val modelId: String) : State
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val _state = MutableStateFlow<State>(State.Idle)
    private var job: Job? = null

    val state: StateFlow<State> = _state.asStateFlow()

    /** Starts downloading [voice] unless a download is already running. */
    @Synchronized
    fun start(context: Context, voice: NeuralVoice) {
        if (job?.isActive == true) return
        val model = voice.model
        val store = NeuralVoiceStore.get(context)
        _state.value = State.Running(model.id, 0, model.sizeBytes)
        job = scope.launch {
            try {
                var reported = -1L
                store.download(model) { done, total ->
                    if (done - reported >= PROGRESS_STEP || done == total) {
                        reported = done
                        _state.value = State.Running(model.id, done, total)
                    }
                }
                writeLicense(context, store, voice)
                // A voice that failed to load before may load now that its files are new.
                NeuralVoiceEngine.get(context).forget(voice)
                VoiceLog.i(VoiceLog.AUDIO, "neural voice download done voice=${voice.id}")
                _state.value = State.Done(model.id)
            } catch (e: CancellationException) {
                _state.value = State.Idle
                throw e
            } catch (e: Exception) {
                VoiceLog.w(VoiceLog.AUDIO, "neural voice download failed voice=${voice.id}", e)
                _state.value = State.Failed(model.id, e.message ?: e.javaClass.simpleName)
            }
        }
    }

    /**
     * Puts the voice's licence notice (the dataset's copyright notice and conditions, which the M-AILABS licence
     * requires to travel with the data) next to the installed voice as `LICENSE`. The text is the app asset
     * `voice-licenses/<voice id>.LICENSE.txt`, the same file the Windows installer ships beside the voice.
     */
    private fun writeLicense(context: Context, store: ParakeetModelStore, voice: NeuralVoice) {
        val text = context.assets.open("$LICENSE_ASSET_DIR/${voice.id}.LICENSE.txt").use { it.readBytes() }
        File(store.directory(voice.model), LICENSE_FILE).writeBytes(text)
    }

    private const val LICENSE_ASSET_DIR = "voice-licenses"
    private const val LICENSE_FILE = "LICENSE"

    /** Stops a running download; the bytes so far stay for the next [start]. */
    @Synchronized
    fun cancel() {
        job?.cancel()
        job = null
        if (_state.value is State.Failed) _state.value = State.Idle
    }

    /** Removes [voice] and any partial download of it, and lets go of the loaded voice. */
    suspend fun delete(context: Context, voice: NeuralVoice) {
        if ((_state.value as? State.Running)?.modelId == voice.model.id) cancel()
        NeuralVoiceEngine.get(context).forget(voice)
        NeuralVoiceStore.get(context).delete(voice.model)
        if (_state.value.modelId == voice.model.id) _state.value = State.Idle
        VoiceLog.i(VoiceLog.AUDIO, "neural voice deleted voice=${voice.id}")
    }

    private const val PROGRESS_STEP = 1L shl 20
}
