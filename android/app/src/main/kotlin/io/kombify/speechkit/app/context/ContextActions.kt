package io.kombify.speechkit.app.context

import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.widget.Toast
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.R
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.app.context.capture.ScreenCaptureActivity
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ExecutionPolicy
import io.kombify.speechkit.net.StoredConnectionSelection
import java.util.UUID

/** One-use handoff. Pixels and per-gesture authority are never trusted from intent extras. */
object ContextActions {
    internal data class Request(val content: ContextContent, val profile: ConnectionProfile,
        val policy: ExecutionPolicy, val revision: Long, val createdAt: Long = SystemClock.elapsedRealtime())
    private val main = Handler(Looper.getMainLooper())
    private var pending: Pair<String, Request>? = null
    private val expire = Runnable { discard() }
    internal const val EXTRA_REQUEST = "context_request"

    fun capture(context: Context, policy: ExecutionPolicy, expectedRevision: Long) {
        if (!BuildConfig.CONTEXT_ACTIONS_ENABLED) return
        val route = StoredConnectionSelection(context).snapshot(ShippedDefaults.shippedProfile())
        if (route.revision != expectedRevision) { unavailable(context); return }
        val accepted = ScreenCaptureActivity.request(context) { result ->
            val bitmap = result.getOrNull()
            if (bitmap == null) {
                if (result.exceptionOrNull() !is java.util.concurrent.CancellationException) unavailable(context)
            } else if (StoredConnectionSelection(context).revision() != route.revision) {
                bitmap.recycle(); unavailable(context)
            } else {
                discard()
                val nonce = UUID.randomUUID().toString()
                pending = nonce to Request(ContextContent.Image(bitmap, screen = true), route.profile, policy, route.revision)
                main.postDelayed(expire, 60_000)
                try {
                    context.startActivity(Intent(context, ContentAnalysisActivity::class.java)
                        .putExtra(EXTRA_REQUEST, nonce).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                } catch (_: RuntimeException) { discard(); unavailable(context) }
            }
        }
        if (!accepted) unavailable(context)
    }

    internal fun consume(nonce: String): Request? {
        val current = pending ?: return null
        if (current.first != nonce) return null
        pending = null
        main.removeCallbacks(expire)
        if (SystemClock.elapsedRealtime() - current.second.createdAt > 60_000) {
            (current.second.content as? ContextContent.Image)?.bitmap?.recycle()
            return null
        }
        return current.second
    }

    private fun discard() {
        (pending?.second?.content as? ContextContent.Image)?.bitmap?.recycle()
        pending = null
        main.removeCallbacks(expire)
    }

    private fun unavailable(context: Context) = Toast.makeText(context, R.string.context_capture_unavailable, Toast.LENGTH_LONG).show()
}
