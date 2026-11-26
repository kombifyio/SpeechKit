package io.kombify.speechkit.app.keyboard

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.R
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.app.hybrid.HybridRuntime
import io.kombify.speechkit.app.hybrid.HybridTextSource
import io.kombify.speechkit.app.ui.theme.SpeechKitTheme
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.StoredConnectionSelection
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/** Foreground model execution and explicit preview; the editor is never an activity extra. */
class KeyboardTextActivity : ComponentActivity() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var job: Job? = null
    private var nonce = ""
    private var submitted = false
    private var result by mutableStateOf<String?>(null)
    private var source by mutableStateOf<HybridTextSource?>(null)
    private var failed by mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        nonce = intent.getStringExtra(KeyboardTextActions.EXTRA_NONCE).orEmpty()
        val request = KeyboardTextActions.request(nonce) ?: run { finish(); return }
        setContent {
            SpeechKitTheme {
                Surface {
                    Column(Modifier.padding(20.dp).widthIn(max = 520.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Text(stringResource(R.string.keyboard_text_title), style = MaterialTheme.typography.titleMedium)
                        when {
                            failed -> Text(stringResource(R.string.keyboard_text_failed), style = MaterialTheme.typography.bodyMedium)
                            result == null -> {
                                LinearProgressIndicator(Modifier.fillMaxWidth())
                                Text(stringResource(R.string.keyboard_text_working), style = MaterialTheme.typography.bodyMedium)
                            }
                            else -> {
                                source?.let {
                                    Text(stringResource(when (it) {
                                        HybridTextSource.FIREBASE_DEVICE -> R.string.keyboard_text_source_device
                                        HybridTextSource.FIREBASE_CLOUD -> R.string.keyboard_text_source_firebase
                                        HybridTextSource.SPEECHKIT_ENDPOINT -> R.string.keyboard_text_source_endpoint
                                        HybridTextSource.GEMINI -> R.string.keyboard_text_source_gemini
                                    }), style = MaterialTheme.typography.labelMedium)
                                }
                                Text(result.orEmpty(), modifier = Modifier.heightIn(max = 320.dp).verticalScroll(rememberScrollState()),
                                    style = MaterialTheme.typography.bodyMedium)
                            }
                        }
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            TextButton(onClick = { finish() }) { Text(stringResource(R.string.keyboard_text_cancel)) }
                            Button(enabled = result != null && !failed, onClick = {
                                val text = result ?: return@Button
                                submitted = KeyboardTextActions.submit(nonce, text)
                                if (!submitted) failed = true else finish()
                            }) { Text(stringResource(R.string.keyboard_text_insert)) }
                        }
                    }
                }
            }
        }
        val profile = request.profile
        val instruction = when (request.action) {
            KeyboardTextAction.SUMMARIZE -> "Summarize the following text. Return only the summary."
            KeyboardTextAction.REWRITE -> "Rewrite the following text clearly, preserving its meaning. Return only the rewritten text."
            KeyboardTextAction.CORRECT -> "Correct spelling and grammar in the following text, preserving its meaning. Return only the corrected text."
        }
        val task = scope.launch(start = CoroutineStart.LAZY) {
            try {
                check(StoredConnectionSelection(this@KeyboardTextActivity).revision() == request.revision) { "Connection changed" }
                val answer = HybridRuntime.generate(this@KeyboardTextActivity, profile,
                    "$instruction Keep the original language.\n\n${request.input}", firebaseCloud = profile is ConnectionProfile.Firebase,
                    policyOverride = request.policy)
                if (answer.value.text.isBlank() || StoredConnectionSelection(this@KeyboardTextActivity).revision() != request.revision) failed = true else {
                    result = answer.value.text
                    source = answer.value.source
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                failed = true
            }
        }
        task.invokeOnCompletion { cause ->
            if (cause is CancellationException) runOnUiThread {
                if (!isFinishing && !submitted) failed = true
            }
        }
        job = task
        HybridRuntime.track(this, profile, task)
        task.start()
    }

    override fun onStop() {
        job?.cancel()
        if (!submitted) KeyboardTextActions.discard(nonce)
        super.onStop()
        if (!isFinishing) finish()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }
}
