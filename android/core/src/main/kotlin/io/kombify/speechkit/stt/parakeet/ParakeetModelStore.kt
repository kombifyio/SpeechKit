package io.kombify.speechkit.stt.parakeet

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.job
import okhttp3.Call
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.security.MessageDigest
import java.util.concurrent.TimeUnit
import kotlin.coroutines.coroutineContext

/**
 * Downloads, verifies and holds [ParakeetModel]s under [root], one directory
 * per model. Every file is checked against its pinned SHA-256 before the
 * model directory appears, so a model in [root] is always complete: a
 * cancelled or failed download leaves only a staging directory, which the
 * next [download] resumes (HTTP range requests; a server without them
 * restarts the file).
 *
 * On Android, use the process's store from `ParakeetOnDevice.modelStore(context)`;
 * the models then live in `noBackupFilesDir` and stay out of cloud backups. One
 * store per [root]: its lock is what keeps a download and a delete apart.
 */
class ParakeetModelStore(
    val root: File,
    private val client: OkHttpClient = defaultClient(),
) {
    private val lock = Mutex()

    /** Directory [model] installs into. */
    fun directory(model: ParakeetModel): File = File(root, model.dirName)

    /**
     * True when [model] is installed: every file present at its pinned size.
     * Hashes were verified at install time; this check does not rehash.
     */
    fun isInstalled(model: ParakeetModel): Boolean {
        val dir = directory(model)
        return model.files.all { File(dir, it.name).let { f -> f.isFile && f.length() == it.sizeBytes } }
    }

    /** Installed models, recommended first. */
    fun installed(): List<ParakeetModel> =
        ParakeetModels.all.filter(::isInstalled).sortedByDescending { it.recommended }

    /** Files of the preferred installed model, or null when none is installed. */
    fun preferredFiles(): ParakeetModelFiles? =
        installed().firstNotNullOfOrNull { runCatching { ParakeetModelFiles.resolve(directory(it)) }.getOrNull() }

    /**
     * Downloads and installs [model], reporting bytes done of the model's
     * total through [onProgress]. Returns the model directory. Cancelling the
     * calling coroutine stops the download; the bytes so far are kept for the
     * next call.
     *
     * @throws IOException on a network error, a checksum mismatch (the
     *   damaged file is deleted) or too little free storage.
     */
    suspend fun download(
        model: ParakeetModel,
        onProgress: (done: Long, total: Long) -> Unit = { _, _ -> },
    ): File = withContext(Dispatchers.IO) {
        lock.withLock {
            val final = directory(model)
            if (isInstalled(model)) {
                onProgress(model.sizeBytes, model.sizeBytes)
                return@withLock final
            }
            root.mkdirs()
            val staging = File(root, ".${model.dirName}.partial")
            staging.mkdirs()
            val stagedBytes = model.files.sumOf { File(staging, it.name).let { f -> if (f.isFile) f.length() else 0L } }
            val needed = model.sizeBytes - stagedBytes
            if (root.usableSpace < needed + FREE_SPACE_MARGIN) {
                throw IOException(
                    "Parakeet needs ${needed / MB} MB of free storage; ${root.usableSpace / MB} MB are available",
                )
            }
            var done = 0L
            for (file in model.files) {
                coroutineContext.ensureActive()
                val target = File(staging, file.name)
                fetch(file, target) { fileDone -> onProgress(done + fileDone, model.sizeBytes) }
                done += file.sizeBytes
            }
            if (final.exists() && !final.deleteRecursively()) {
                throw IOException("could not replace ${final.path}")
            }
            if (!staging.renameTo(final)) throw IOException("could not install ${final.path}")
            onProgress(model.sizeBytes, model.sizeBytes)
            final
        }
    }

    /** Bytes of [model] a stopped download left on disk, which the next [download] resumes from. */
    fun partialBytes(model: ParakeetModel): Long =
        File(root, ".${model.dirName}.partial").listFiles()?.sumOf { it.length() } ?: 0L

    /** Removes [model] and any partial download of it. */
    suspend fun delete(model: ParakeetModel) = withContext(Dispatchers.IO) {
        lock.withLock {
            File(root, ".${model.dirName}.partial").deleteRecursively()
            directory(model).deleteRecursively()
        }
    }

    /**
     * Brings [target] to the complete, verified content of [file], resuming
     * a partial download.
     */
    private suspend fun fetch(file: ParakeetModelFile, target: File, onFileProgress: (Long) -> Unit) {
        if (target.isFile && target.length() >= file.sizeBytes) {
            if (target.length() == file.sizeBytes && sha256(target) == file.sha256) {
                onFileProgress(file.sizeBytes)
                return
            }
            // Complete length but wrong content (or too long): nothing to
            // resume, and a range request past the end would only get a 416.
            target.delete()
        }
        val digest = MessageDigest.getInstance("SHA-256")
        var have = if (target.isFile) target.length() else 0L
        if (have > 0) FileInputStream(target).use { digest.update(it, have) }

        val request = Request.Builder().url(file.url).apply {
            if (have > 0) header("Range", "bytes=$have-")
        }.build()
        val call = client.newCall(request)
        // Cancelling the coroutine aborts a read blocked on a stalled link at
        // once instead of after the read timeout.
        val abort = coroutineContext.job.invokeOnCompletion { if (it != null) call.cancel() }
        try {
            fetchBody(call, file, target, digest, have, onFileProgress)
        } catch (e: IOException) {
            coroutineContext.ensureActive()
            throw e
        } finally {
            abort.dispose()
        }
        val got = digest.digest().toHex()
        if (got != file.sha256) {
            target.delete()
            throw IOException("${file.name} failed verification: SHA-256 $got, expected ${file.sha256}")
        }
    }

    private suspend fun fetchBody(
        call: Call,
        file: ParakeetModelFile,
        target: File,
        digest: MessageDigest,
        resumeFrom: Long,
        onFileProgress: (Long) -> Unit,
    ) {
        var have = resumeFrom
        call.execute().use { response ->
            when {
                response.code == 206 && have > 0 -> Unit
                response.isSuccessful -> {
                    // No range support (or a fresh start): begin the file again.
                    have = 0
                    digest.reset()
                }
                else -> throw IOException("download of ${file.name} failed: HTTP ${response.code}")
            }
            val body = response.body
            FileOutputStream(target, have > 0).use { out ->
                body.byteStream().use { input ->
                    val buffer = ByteArray(BUFFER_BYTES)
                    var written = have
                    onFileProgress(written)
                    while (true) {
                        coroutineContext.ensureActive()
                        val n = input.read(buffer)
                        if (n < 0) break
                        if (written + n > file.sizeBytes) {
                            target.delete()
                            throw IOException("${file.name} is larger than its pinned ${file.sizeBytes} bytes")
                        }
                        out.write(buffer, 0, n)
                        digest.update(buffer, 0, n)
                        written += n
                        onFileProgress(written)
                    }
                    if (written != file.sizeBytes) {
                        throw IOException("${file.name} ended at $written of ${file.sizeBytes} bytes")
                    }
                }
            }
        }
    }

    companion object {
        private const val MB = 1024L * 1024
        private const val FREE_SPACE_MARGIN = 64 * MB
        private const val BUFFER_BYTES = 256 * 1024

        /** A client for large downloads: no read timeout shorter than a slow mobile link needs. */
        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(30, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .callTimeout(0, TimeUnit.SECONDS)
            .build()

        private fun MessageDigest.update(input: FileInputStream, length: Long) {
            val buffer = ByteArray(BUFFER_BYTES)
            var left = length
            while (left > 0) {
                val n = input.read(buffer, 0, minOf(buffer.size.toLong(), left).toInt())
                if (n < 0) break
                update(buffer, 0, n)
                left -= n
            }
        }

        private fun sha256(file: File): String {
            val digest = MessageDigest.getInstance("SHA-256")
            FileInputStream(file).use { digest.update(it, file.length()) }
            return digest.digest().toHex()
        }

        private fun ByteArray.toHex(): String = joinToString("") { "%02x".format(it) }
    }
}
