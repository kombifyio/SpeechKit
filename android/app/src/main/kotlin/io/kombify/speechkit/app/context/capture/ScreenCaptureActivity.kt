package io.kombify.speechkit.app.context.capture

import android.app.Activity
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.graphics.Bitmap
import android.graphics.PixelFormat
import android.hardware.display.DisplayManager
import android.hardware.display.VirtualDisplay
import android.media.ImageReader
import android.media.projection.MediaProjection
import android.media.projection.MediaProjectionManager
import android.media.projection.MediaProjectionConfig
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.SystemClock
import android.util.DisplayMetrics
import android.view.WindowManager
import io.kombify.speechkit.R
import java.lang.ref.WeakReference
import java.nio.ByteBuffer
import java.util.UUID
import java.util.concurrent.CancellationException
import kotlin.math.max
import kotlin.math.roundToInt

/** One outstanding request per process. Neither consent nor pixels leave this process. */
private object CaptureRequest {
    val main = Handler(Looper.getMainLooper())
    var callback: ((Result<Bitmap>) -> Unit)? = null
    var consent: Intent? = null
    var id: String? = null
    var host: WeakReference<ScreenCaptureActivity>? = null
    var pending: Result<Bitmap>? = null
    var resumed = false
    var focused = false
    var armed = false
    fun canCapture(): Boolean = armed && resumed && focused && host?.get()?.hasWindowFocus() == true
    // A result awaiting a visible host is retained only until the original request deadline.
    val timeout = Runnable {
        if ((resumed && focused) || host?.get() == null) {
            complete(Result.failure(IllegalStateException("Screen capture timed out")))
        } else {
            val activity = host?.get()
            discard()
            activity?.finish()
        }
    }

    fun complete(result: Result<Bitmap>) {
        consent = null
        pending?.getOrNull()?.recycle()
        pending = result
        if ((resumed && focused) || host?.get() == null) deliver()
    }

    fun deliver() {
        val result = pending ?: return
        val recipient = callback
        val activity = host?.get()
        pending = null
        callback = null
        host = null
        id = null
        resumed = false
        focused = false
        armed = false
        main.removeCallbacks(timeout)
        try {
            if (recipient == null) result.getOrNull()?.recycle() else recipient(result)
        } finally {
            activity?.finish()
        }
    }

    fun discard() {
        pending?.getOrNull()?.recycle()
        pending = null
        callback = null
        consent = null
        id = null
        host = null
        resumed = false
        focused = false
        armed = false
        main.removeCallbacks(timeout)
    }
}

/** Transient consent host. The caller must remove its own visible UI before requesting capture. */
class ScreenCaptureActivity : Activity() {
    companion object {
        /** Call on the main thread; false means another capture is already pending. */
        fun request(context: Context, callback: (Result<Bitmap>) -> Unit): Boolean {
            check(Looper.myLooper() == Looper.getMainLooper())
            if (CaptureRequest.callback != null) return false
            CaptureRequest.callback = callback
            CaptureRequest.id = UUID.randomUUID().toString()
            CaptureRequest.main.postDelayed(CaptureRequest.timeout, 60_000)
            return try {
                val intent = Intent(context, ScreenCaptureActivity::class.java).putExtra("request_id", CaptureRequest.id)
                if (context !is Activity) intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                context.startActivity(intent)
                true
            } catch (error: RuntimeException) {
                CaptureRequest.complete(Result.failure(error))
                false
            }
        }

        /** Cancels a pending request and discards a later result. */
        fun cancel(context: Context) {
            check(Looper.myLooper() == Looper.getMainLooper())
            CaptureRequest.armed = false
            CaptureRequest.complete(Result.failure(CancellationException("Screen capture cancelled")))
            context.stopService(Intent(context, ScreenCaptureService::class.java))
        }
    }

    override fun onCreate(savedInstanceState: android.os.Bundle?) {
        super.onCreate(savedInstanceState)
        if (intent.getStringExtra("request_id") != CaptureRequest.id || CaptureRequest.callback == null) {
            finish()
            return
        }
        CaptureRequest.host = WeakReference(this)
        if (savedInstanceState != null) {
            if (CaptureRequest.callback != null) CaptureRequest.complete(Result.failure(CancellationException("Capture interrupted")))
            return
        }
        try {
            @Suppress("DEPRECATION")
            val manager = getSystemService(MediaProjectionManager::class.java)
            // This action captures the current display for an explicit crop; a single-app
            // chooser could put the host in the background before the review handoff.
            val captureIntent = if (Build.VERSION.SDK_INT >= 34) {
                manager.createScreenCaptureIntent(MediaProjectionConfig.createConfigForDefaultDisplay())
            } else {
                manager.createScreenCaptureIntent()
            }
            startActivityForResult(captureIntent, 1)
        } catch (error: RuntimeException) {
            CaptureRequest.complete(Result.failure(error))
        }
    }

    override fun onResume() {
        super.onResume()
        if (CaptureRequest.host?.get() === this) {
            CaptureRequest.resumed = true
            if (hasWindowFocus()) CaptureRequest.focused = true
            if (CaptureRequest.focused) {
                CaptureRequest.deliver()
                startCaptureIfReady()
            }
        }
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (CaptureRequest.host?.get() !== this) return
        CaptureRequest.focused = hasFocus
        if (!hasFocus) {
            cancelArmedCapture()
        } else if (CaptureRequest.resumed) {
            CaptureRequest.deliver()
            startCaptureIfReady()
        }
    }

    override fun onPause() {
        if (CaptureRequest.host?.get() === this) {
            CaptureRequest.resumed = false
            cancelArmedCapture()
        }
        super.onPause()
    }

    private fun cancelArmedCapture() {
        if (!CaptureRequest.armed) return
        CaptureRequest.armed = false
        CaptureRequest.complete(Result.failure(CancellationException("Screen capture interrupted")))
        stopService(Intent(this, ScreenCaptureService::class.java))
    }

    private fun startCaptureIfReady() {
        if (!CaptureRequest.resumed || !CaptureRequest.focused || CaptureRequest.armed ||
            CaptureRequest.consent == null || CaptureRequest.pending != null) return
        CaptureRequest.armed = true
        try {
            startForegroundService(Intent(this, ScreenCaptureService::class.java))
        } catch (error: RuntimeException) {
            CaptureRequest.armed = false
            CaptureRequest.complete(Result.failure(error))
        }
    }
    override fun onDestroy() {
        if (CaptureRequest.host?.get() === this) {
            CaptureRequest.discard()
            stopService(Intent(this, ScreenCaptureService::class.java))
        }
        super.onDestroy()
    }

    @Deprecated("Used to avoid persisting consent across activity recreation")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != 1 || CaptureRequest.callback == null || intent.getStringExtra("request_id") != CaptureRequest.id) { finish(); return }
        if (resultCode != RESULT_OK || data == null) {
            CaptureRequest.complete(Result.failure(CancellationException("Screen capture permission declined")))
            return
        }
        CaptureRequest.consent = data
        startCaptureIfReady()
    }
}

/** Foreground service owns exactly one projection and releases it on every exit path. */
class ScreenCaptureService : Service() {
    private val handler = Handler(Looper.getMainLooper())
    private var requestId: String? = null
    private var projection: MediaProjection? = null
    private var display: VirtualDisplay? = null
    private var reader: ImageReader? = null
    private var resized = false
    private var lastResize = 0L
    private var readyAt = 0L
    private var finished = false
    private val projectionCallback = object : MediaProjection.Callback() {
        override fun onStop() { fail("Screen capture stopped") }
        override fun onCapturedContentResize(width: Int, height: Int) {
            if (finished || width <= 0 || height <= 0) return
            resized = true
            lastResize = SystemClock.elapsedRealtime()
            try { resize(width, height) } catch (error: RuntimeException) { finishWith(Result.failure(error)) }
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (projection != null || finished) return START_NOT_STICKY
        requestId = CaptureRequest.id
        val consent = CaptureRequest.consent
        CaptureRequest.consent = null
        if (consent == null || CaptureRequest.callback == null || !CaptureRequest.canCapture()) { stopSelf(); return START_NOT_STICKY }
        try {
            val manager = getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(NotificationChannel("screen_capture", "Screen capture", NotificationManager.IMPORTANCE_LOW))
            val notification = Notification.Builder(this, "screen_capture")
                .setSmallIcon(android.R.drawable.ic_menu_camera)
                .setContentTitle(getString(R.string.context_capture_title))
                .setContentText(getString(R.string.context_capture_body))
                .setOngoing(true).build()
            if (Build.VERSION.SDK_INT >= 29) startForeground(2198, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PROJECTION)
            else startForeground(2198, notification)
            val mediaProjection = getSystemService(MediaProjectionManager::class.java).getMediaProjection(Activity.RESULT_OK, consent)
                ?: throw IllegalStateException(getString(R.string.context_capture_unavailable))
            projection = mediaProjection
            mediaProjection.registerCallback(projectionCallback, handler)
            handler.postDelayed({ startDisplay() }, 500)
            handler.postDelayed({ if (!finished) fail("Screen capture timed out") }, 15_000)
        } catch (error: RuntimeException) {
            finishWith(Result.failure(error))
        }
        return START_NOT_STICKY
    }

    private fun startDisplay() {
        if (finished) return
        if (requestId != CaptureRequest.id || !CaptureRequest.canCapture()) {
            fail("Screen capture interrupted")
            return
        }
        try {
            val mediaProjection = projection ?: throw IllegalStateException("Screen capture stopped")
            val wm = getSystemService(WindowManager::class.java)
            val width: Int
            val height: Int
            if (Build.VERSION.SDK_INT >= 30) {
                val bounds = wm.maximumWindowMetrics.bounds
                width = bounds.width(); height = bounds.height()
            } else {
                @Suppress("DEPRECATION") val metrics = DisplayMetrics().also { wm.defaultDisplay.getRealMetrics(it) }
                width = metrics.widthPixels; height = metrics.heightPixels
            }
            val (w, h) = bounded(width, height)
            val newReader = makeReader(w, h)
            reader = newReader
            readyAt = SystemClock.elapsedRealtime()
            display = mediaProjection.createVirtualDisplay("SpeechKit capture", w, h, resources.configuration.densityDpi,
                DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR, newReader.surface, null, handler)
                ?: throw IllegalStateException("Could not create screen capture")
        } catch (error: RuntimeException) {
            finishWith(Result.failure(error))
        }
    }
    private fun bounded(width: Int, height: Int): Pair<Int, Int> {
        require(width > 0 && height > 0)
        val scale = minOf(1.0, 1600.0 / max(width, height))
        return max(1, (width * scale).roundToInt()) to max(1, (height * scale).roundToInt())
    }

    private var drain: Runnable? = null

    private fun makeReader(width: Int, height: Int): ImageReader =
        ImageReader.newInstance(width, height, PixelFormat.RGBA_8888, 2).also { imageReader ->
            imageReader.setOnImageAvailableListener({ source -> scheduleDrain(source) }, handler)
        }

    // Leave a static frame queued until the surface and consent UI have settled.
    private fun scheduleDrain(source: ImageReader) {
        if (finished || source !== reader || !CaptureRequest.canCapture()) return
        if (Build.VERSION.SDK_INT >= 34 && !resized) return
        drain?.let(handler::removeCallbacks)
        val deadline = maxOf(readyAt, lastResize + 500)
        val task = Runnable { drainFrame(source) }
        drain = task
        handler.postDelayed(task, maxOf(0L, deadline - SystemClock.elapsedRealtime()))
    }

    private fun drainFrame(source: ImageReader) {
        drain = null
        if (finished || source !== reader || !CaptureRequest.canCapture()) return
        val image = try { source.acquireLatestImage() } catch (_: IllegalStateException) { null }
        if (image != null) image.use {
            try {
                val plane = it.planes[0]
                require(plane.pixelStride == 4) { "Unsupported screen capture pixel stride" }
                val rowBytes = it.width * 4
                require(plane.rowStride >= rowBytes) { "Invalid screen capture row stride" }
                val sourceBuffer = plane.buffer.duplicate()
                val firstByte = sourceBuffer.position()
                val pixels = ByteArray(rowBytes * it.height)
                for (row in 0 until it.height) {
                    val start = firstByte.toLong() + row.toLong() * plane.rowStride
                    require(start + rowBytes <= sourceBuffer.limit().toLong()) { "Incomplete screen capture frame" }
                    sourceBuffer.position(start.toInt())
                    sourceBuffer.get(pixels, row * rowBytes, rowBytes)
                }
                val bitmap = Bitmap.createBitmap(it.width, it.height, Bitmap.Config.ARGB_8888)
                bitmap.copyPixelsFromBuffer(ByteBuffer.wrap(pixels))
                finishWith(Result.success(bitmap))
            } catch (error: RuntimeException) { finishWith(Result.failure(error)) }
        }
    }
    private fun resize(width: Int, height: Int) {
        val (w, h) = bounded(width, height)
        val current = reader ?: return
        if (current.width == w && current.height == h) {
            scheduleDrain(current)
            return
        }
        val next = makeReader(w, h)
        reader = next
        display?.resize(w, h, resources.configuration.densityDpi)
        display?.surface = next.surface
        current.close()
        scheduleDrain(next)
    }

    private fun fail(message: String) = finishWith(Result.failure(IllegalStateException(message)))
    private fun finishWith(result: Result<Bitmap>) {
        if (finished) { result.getOrNull()?.recycle(); return }
        finished = true
        handler.removeCallbacksAndMessages(null)
        display?.release(); display = null
        reader?.close(); reader = null
        projection?.unregisterCallback(projectionCallback)
        projection?.stop(); projection = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
        if (requestId != null && requestId == CaptureRequest.id) CaptureRequest.complete(result)
        else result.getOrNull()?.recycle()
    }

    override fun onDestroy() {
        if (!finished) fail("Screen capture interrupted")
        super.onDestroy()
    }
}
