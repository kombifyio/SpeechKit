package io.kombify.speechkit.ime

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow

/**
 * In-process relay for the trampoline's answer. A SharedFlow (not a result
 * callback) because requester and requestee live in different components: the
 * IME service asks, the activity answers, and neither holds a reference to
 * the other.
 */
object MicPermissionEvents {
    private val _results = MutableSharedFlow<Boolean>(extraBufferCapacity = 1)
    val results: SharedFlow<Boolean> = _results

    fun publish(granted: Boolean) {
        _results.tryEmit(granted)
    }
}

/**
 * Translucent, history-less trampoline that requests RECORD_AUDIO on behalf
 * of the IME window (services cannot show the runtime-permission dialog).
 * `noHistory` + `excludeFromRecents` + empty task affinity in the manifest
 * keep it invisible in recents and gone the moment the dialog resolves.
 */
class MicPermissionTrampolineActivity : ComponentActivity() {

    private val launcher = registerForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        MicPermissionEvents.publish(granted)
        finish()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val granted = ContextCompat.checkSelfPermission(
            this, Manifest.permission.RECORD_AUDIO,
        ) == PackageManager.PERMISSION_GRANTED
        if (granted) {
            MicPermissionEvents.publish(true)
            finish()
            return
        }
        launcher.launch(Manifest.permission.RECORD_AUDIO)
    }

    companion object {
        fun launch(context: Context) {
            context.startActivity(
                Intent(context, MicPermissionTrampolineActivity::class.java)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
    }
}
