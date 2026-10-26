package io.kombify.speechkit.stt.parakeet

import android.app.ActivityManager
import android.content.ComponentCallbacks2
import android.content.Context
import android.os.Process
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.stt.phonon.PhononNative
import java.io.File

/**
 * Android entry points of the on-device tier (Parakeet on ONNX Runtime, Phonon-2.5 on SpeechKit's five-value
 * engine): where models live, which model is selected, whether this device can run it, and the process's
 * [recognizers].
 *
 * A loaded model holds about 1 GB (Parakeet) or 350 MB (Phonon), so it loads when a segment starts (while the user
 * is still speaking) and is released two minutes after the last transcription, or right away from [onTrimMemory].
 */
object ParakeetOnDevice {
    /**
     * Total memory below which [support] reports the device as too small for a Parakeet model: the loaded model
     * takes about 1 GB, which a 4 GB phone cannot spare next to a foreground app without the system killing
     * processes. Phones sold as 6 GB report a little under 6 GiB, hence 5.5 GiB. Phonon models need far less
     * ([ParakeetModels.PHONON_MIN_TOTAL_MEMORY_BYTES]).
     */
    const val MIN_TOTAL_MEMORY_BYTES: Long = 5L * 1024 * 1024 * 1024 + 512L * 1024 * 1024

    /** The process's one recognizer pool. */
    val recognizers = ParakeetRecognizerPool()

    @Volatile
    private var store: ParakeetModelStore? = null

    /** The process's store for models under `noBackupFilesDir`, out of cloud backups. */
    fun modelStore(context: Context): ParakeetModelStore =
        store ?: synchronized(this) {
            store ?: ParakeetModelStore(
                File(context.applicationContext.noBackupFilesDir, "speechkit/models/parakeet"),
            ).also { store = it }
        }

    /** Whether this device can run Parakeet, and why not. */
    sealed interface Support {
        data object Supported : Support
        data class Unsupported(val reason: String) : Support
    }

    /**
     * Whether this device can run [model]: a 64-bit process (the model needs more address space than a 32-bit one
     * reliably has), the model's memory, and its engine in this build for the process's ABI (a host may package it
     * for some ABIs only) – ONNX Runtime for Parakeet; for Phonon the five-value engine and a processor with the
     * instructions it is built for (ARM dot product, x86-64-v3).
     */
    fun support(context: Context, model: ParakeetModel = selectedModel(context)): Support {
        if (!Process.is64Bit()) {
            return Support.Unsupported("on-device ${model.name} needs a 64-bit device and app")
        }
        val minTotalMemory = model.minTotalMemoryBytes
        val memory = ActivityManager.MemoryInfo()
        val activityManager = context.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
            ?: return Support.Unsupported("memory information unavailable")
        activityManager.getMemoryInfo(memory)
        if (memory.totalMem < minTotalMemory) {
            return Support.Unsupported(
                "on-device ${model.name} needs about ${"%.1f".format(minTotalMemory / GIB)} GiB of memory; " +
                    "this device has ${"%.1f".format(memory.totalMem / GIB)} GiB",
            )
        }
        when (model.engine) {
            OnDeviceEngineKind.ONNX -> if (!runtimeAvailable()) {
                return Support.Unsupported("the on-device speech runtime is not available in this app on this device")
            }
            OnDeviceEngineKind.FIVE_VALUE -> {
                if (!PhononNative.available()) {
                    return Support.Unsupported("the Phonon engine is not available in this app on this device")
                }
                if (!PhononNative.cpuSupported()) {
                    return Support.Unsupported("this processor lacks the dot-product instructions Phonon needs")
                }
            }
        }
        return Support.Supported
    }

    /** The models this device can run, in catalog order (recommended first). */
    fun supportedModels(context: Context): List<ParakeetModel> =
        ParakeetModels.all.filter { support(context, it) == Support.Supported }

    private const val PREFS = "speechkit_on_device"
    private const val KEY_MODEL = "on_device_model"

    /**
     * The model the user chose for on-device dictation. Before any choice: the first installed model this device
     * can run (so someone who already dictates with Parakeet Ultra keeps it), otherwise the first of Phonon-2.5 and
     * Parakeet Ultra it can run, otherwise Phonon-2.5.
     */
    fun selectedModel(context: Context): ParakeetModel {
        val stored = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(KEY_MODEL, null)
        ParakeetModels.byId(stored.orEmpty())?.let { return it }
        return modelStore(context).installed().firstOrNull { support(context, it) == Support.Supported }
            ?: listOf(ParakeetModels.PHONON, ParakeetModels.ULTRA).firstOrNull { support(context, it) == Support.Supported }
            ?: ParakeetModels.PHONON
    }

    fun setSelectedModel(context: Context, model: ParakeetModel) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putString(KEY_MODEL, model.id).apply()
    }

    /**
     * The installed model this device can use now, the selected one first, then any other installed one: supported,
     * installed, and not known to fail loading. Null means a host should dictate with something else.
     */
    fun usableFiles(context: Context): OnDeviceModelFiles? {
        val store = modelStore(context)
        // The file check first: without a model, the probe in [support] would load a native engine for nothing.
        val installed = store.installed()
        if (installed.isEmpty()) return null
        val selected = selectedModel(context)
        val candidates = listOfNotNull(selected.takeIf { it in installed }) + installed.filter { it != selected }
        for (model in candidates) {
            if (support(context, model) != Support.Supported) continue
            val files = runCatching { OnDeviceModelFiles.resolve(store.directory(model)) }.getOrNull() ?: continue
            if (!recognizers.loadFailed(files.dir)) return files
        }
        return null
    }

    /** Releases the loaded model once no segment holds it. */
    fun release() = recognizers.release()

    /**
     * Deletes [model] and any partial download of it, stopping a running download first. A segment that already
     * holds the model finishes with its open files; later dictation falls back until the model is downloaded again.
     */
    suspend fun deleteModel(context: Context, model: ParakeetModel) {
        ParakeetDownloads.cancel()
        release()
        val store = modelStore(context)
        store.delete(model)
        recognizers.clearFailure(store.directory(model))
    }

    /** Call from `Application.onTrimMemory`: a backgrounded process gives the model's memory back. */
    fun onTrimMemory(level: Int) {
        @Suppress("DEPRECATION") // TRIM_MEMORY_BACKGROUND is the documented threshold for "app went to background".
        if (level >= ComponentCallbacks2.TRIM_MEMORY_BACKGROUND) release()
    }

    @Volatile
    private var runtimeLoaded = false

    /**
     * Whether ONNX Runtime's native libraries load in this process. It is the load ORT's own initialisation does on
     * Android (its JNI library pulls in `libonnxruntime.so`), so a host that left them out for this ABI is never
     * offered a 670 MB download it cannot run. Only success is remembered: a failed load may be transient.
     */
    private fun runtimeAvailable(): Boolean {
        if (runtimeLoaded) return true
        return try {
            System.loadLibrary("onnxruntime4j_jni")
            runtimeLoaded = true
            true
        } catch (e: UnsatisfiedLinkError) {
            VoiceLog.w(VoiceLog.DICTATION, "onnxruntime natives unavailable for this process", e)
            false
        }
    }

    private const val GIB = 1024.0 * 1024 * 1024
}
