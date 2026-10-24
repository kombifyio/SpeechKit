package io.kombify.speechkit.stt.parakeet

import android.app.ActivityManager
import android.content.ComponentCallbacks2
import android.content.Context
import android.os.Process
import io.kombify.speechkit.log.VoiceLog
import java.io.File

/**
 * Android entry points of the on-device Parakeet tier: where models live, whether this device can run one, and
 * the process's [recognizers].
 *
 * The loaded model holds about 1 GB, so it loads when a segment starts (while the user is still speaking) and is
 * released two minutes after the last transcription, or right away from [onTrimMemory].
 */
object ParakeetOnDevice {
    /**
     * Total memory below which [support] reports the device as too small: the loaded model takes about 1 GB, which
     * a 4 GB phone cannot spare next to a foreground app without the system killing processes. Phones sold as 6 GB
     * report a little under 6 GiB, hence 5.5 GiB.
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
     * A 64-bit process (the model needs more address space than a 32-bit one reliably has), enough RAM, and ONNX
     * Runtime's native library in this build for the process's ABI (a host may package it for some ABIs only).
     */
    fun support(context: Context, minTotalMemory: Long = MIN_TOTAL_MEMORY_BYTES): Support {
        if (!Process.is64Bit()) {
            return Support.Unsupported("on-device Parakeet needs a 64-bit device and app")
        }
        val memory = ActivityManager.MemoryInfo()
        val activityManager = context.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
            ?: return Support.Unsupported("memory information unavailable")
        activityManager.getMemoryInfo(memory)
        if (memory.totalMem < minTotalMemory) {
            return Support.Unsupported(
                "on-device Parakeet needs about ${"%.1f".format(minTotalMemory / GIB)} GiB of memory; " +
                    "this device has ${"%.1f".format(memory.totalMem / GIB)} GiB",
            )
        }
        if (!runtimeAvailable()) {
            return Support.Unsupported("the on-device speech runtime is not available in this app on this device")
        }
        return Support.Supported
    }

    /**
     * The installed model this device can use now: supported, installed, and not known to fail loading. Null
     * means a host should dictate with something else.
     */
    fun usableFiles(context: Context): ParakeetModelFiles? {
        // The file check first: without a model, the runtime probe in [support] would load ONNX Runtime for nothing.
        val files = modelStore(context).preferredFiles() ?: return null
        if (support(context) != Support.Supported) return null
        return files.takeUnless { recognizers.loadFailed(it.dir) }
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
