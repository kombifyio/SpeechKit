package io.kombify.speechkit.app.context

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.ImageDecoder
import android.graphics.Matrix
import android.net.Uri
import android.os.Build
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.io.ByteArrayInputStream
import java.nio.ByteBuffer

/** Acquired content, never an assertion about the latest message in another application. */
internal sealed interface ContextContent {
    data class Text(val text: String) : ContextContent
    data class Image(val bitmap: Bitmap, val screen: Boolean = false) : ContextContent
    data class Audio(val uri: Uri, val mimeType: String) : ContextContent
}

/** The only external intake boundary. It neither follows links nor reads private app files. */
internal object SharedContext {
    const val MAX_TEXT = 12_000
    private const val MAX_IMAGE_BYTES = 16 * 1024 * 1024

    suspend fun read(context: Context, intent: Intent): ContextContent = withContext(Dispatchers.IO) {
        require(intent.action == Intent.ACTION_SEND)
        val declared = intent.type.orEmpty().lowercase(java.util.Locale.ROOT)
        if (declared == "text/plain") {
            val text = intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString().orEmpty()
            require(text.isNotBlank() && text.length <= MAX_TEXT)
            return@withContext ContextContent.Text(text)
        }
        val uri = if (Build.VERSION.SDK_INT >= 33) intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else @Suppress("DEPRECATION") (intent.getParcelableExtra(Intent.EXTRA_STREAM) as? Uri)
        require(uri != null && uri.scheme == "content" && !uri.authority.isNullOrBlank())
        require(intent.flags and Intent.FLAG_GRANT_READ_URI_PERMISSION != 0)
        intent.clipData?.let { require(it.itemCount == 1 && it.getItemAt(0).uri == uri) }
        require(context.checkUriPermission(uri, android.os.Process.myPid(), android.os.Process.myUid(),
            Intent.FLAG_GRANT_READ_URI_PERMISSION) == android.content.pm.PackageManager.PERMISSION_GRANTED)
        val provider = context.packageManager.resolveContentProvider(uri.authority!!, 0)
        require(provider != null && provider.applicationInfo.uid != android.os.Process.myUid())
        val actual = context.contentResolver.getType(uri).orEmpty().lowercase(java.util.Locale.ROOT)
        val audio = actual.startsWith("audio/") || actual == "application/ogg"
        val image = actual.startsWith("image/")
        fun normalized(mime: String) = if (mime == "application/ogg") "audio/ogg" else mime
        require(normalized(declared) == normalized(actual) ||
            declared == "audio/*" && audio || declared == "image/*" && image)
        require((audio && (declared.startsWith("audio/") || declared == "application/ogg")) ||
            (image && declared.startsWith("image/")))
        if (audio) {
            context.contentResolver.openAssetFileDescriptor(uri, "r")?.use {
                require(it.length <= io.kombify.speechkit.app.hybrid.HybridRuntime.MAX_AUDIO_BYTES)
            } ?: error("Unreadable audio")
            return@withContext ContextContent.Audio(uri, normalized(actual))
        }
        val bytes = context.contentResolver.openInputStream(uri)?.use { input ->
            val output = ByteArrayOutputStream()
            val buffer = ByteArray(8192)
            var emptyReads = 0
            while (true) {
                currentCoroutineContext().ensureActive()
                val count = input.read(buffer)
                if (count < 0) break
                if (count == 0) { require(++emptyReads < 100); continue }
                emptyReads = 0
                require(output.size() + count <= MAX_IMAGE_BYTES)
                output.write(buffer, 0, count)
            }
            output.toByteArray()
        } ?: error("Unreadable image")
        try {
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
            require(bounds.outWidth in 1..32768 && bounds.outHeight in 1..32768 && bounds.outMimeType == actual)
            if (Build.VERSION.SDK_INT >= 28) {
                val bitmap = ImageDecoder.decodeBitmap(ImageDecoder.createSource(ByteBuffer.wrap(bytes))) { decoder, info, _ ->
                    require(info.size.width in 1..32768 && info.size.height in 1..32768)
                    decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
                    val scale = minOf(1.0, 1600.0 / maxOf(info.size.width, info.size.height))
                    decoder.setTargetSize(maxOf(1, (info.size.width * scale).toInt()), maxOf(1, (info.size.height * scale).toInt()))
                }
                return@withContext ContextContent.Image(bitmap)
            }
            var sample = 1
            while (maxOf(bounds.outWidth, bounds.outHeight) / sample > 1600) sample *= 2
            val bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size,
                BitmapFactory.Options().apply { inSampleSize = sample; inPreferredConfig = Bitmap.Config.ARGB_8888 })
                ?: error("Invalid image")
            val orientation = runCatching { android.media.ExifInterface(ByteArrayInputStream(bytes))
                .getAttributeInt(android.media.ExifInterface.TAG_ORIENTATION, 1) }.getOrDefault(1)
            val matrix = Matrix().apply {
                when (orientation) {
                    2 -> setScale(-1f, 1f)
                    3 -> setRotate(180f)
                    4 -> setScale(1f, -1f)
                    5 -> { setRotate(90f); postScale(-1f, 1f) }
                    6 -> setRotate(90f)
                    7 -> { setRotate(-90f); postScale(-1f, 1f) }
                    8 -> setRotate(-90f)
                }
            }
            val oriented = if (matrix.isIdentity) bitmap else Bitmap.createBitmap(bitmap, 0, 0, bitmap.width, bitmap.height, matrix, true)
            if (oriented !== bitmap) bitmap.recycle()
            ContextContent.Image(oriented)
        } finally { bytes.fill(0) }
    }
}
