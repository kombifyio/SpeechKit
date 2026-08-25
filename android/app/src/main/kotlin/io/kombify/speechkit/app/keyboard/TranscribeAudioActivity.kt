package io.kombify.speechkit.app.keyboard

import android.content.ClipboardManager
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.OpenableColumns
import android.webkit.MimeTypeMap
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.R
import io.kombify.speechkit.app.ui.theme.SpeechKitTheme
import io.kombify.speechkit.net.ConnectionProfile
import io.kombify.speechkit.net.ConnectionProfileSource
import io.kombify.speechkit.net.SpeechKitServerApi
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import timber.log.Timber
import java.util.Locale
import javax.inject.Inject

/**
 * Relays a finished audio-file transcript to the keyboard, if it is still up.
 * Share-from-messenger has no editor to insert into; those callers copy.
 */
object TranscribeAudioEvents {
    private val _transcripts = MutableSharedFlow<String>(extraBufferCapacity = 1)
    val transcripts: SharedFlow<String> = _transcripts

    fun publish(text: String) {
        _transcripts.tryEmit(text)
    }
}

/**
 * Transcribe or summarise a voice note. WhatsApp/Telegram share it here;
 * the keyboard more-page opens a picker. The system recognizer cannot read
 * files, so a paired SpeechKit server is required. Summarise is Assist on
 * that same server, not a new cloud path.
 */
@AndroidEntryPoint
class TranscribeAudioActivity : ComponentActivity() {

    @Inject lateinit var profileSource: ConnectionProfileSource

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val ui = MutableStateFlow<Ui>(Ui.Idle)
    private var insertIntoEditor = false
    private var summarize = false

    private val picker = registerForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri == null) {
            finish()
            return@registerForActivityResult
        }
        transcribe(uri)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        insertIntoEditor = intent.getBooleanExtra(EXTRA_INSERT, false)
        summarize = intent.getBooleanExtra(EXTRA_SUMMARIZE, false) ||
            intent.component?.className?.contains(SUMMARIZE_ALIAS_MARKER) == true
        val titleRes = if (summarize) {
            R.string.shortcut_summarize_audio
        } else {
            R.string.shortcut_transcribe_audio
        }
        setContent {
            SpeechKitTheme {
                val state by ui.collectAsState()
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(24.dp)
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(stringResource(titleRes), style = MaterialTheme.typography.titleMedium)
                    when (val s = state) {
                        Ui.Idle, Ui.Working -> CircularProgressIndicator()
                        is Ui.Done -> Text(s.text, style = MaterialTheme.typography.bodyLarge)
                        is Ui.Failed -> Text(s.reason, color = MaterialTheme.colorScheme.error)
                    }
                    Button(onClick = ::finish) { Text(stringResource(R.string.shortcuts_close)) }
                }
            }
        }
        val shared = sharedAudioUri()
        when {
            shared != null -> transcribe(shared)
            insertIntoEditor || intent.action == Intent.ACTION_MAIN -> picker.launch("audio/*")
            else -> finish()
        }
    }

    private fun sharedAudioUri(): Uri? {
        if (intent.action != Intent.ACTION_SEND) return null
        return if (Build.VERSION.SDK_INT >= 33) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra(Intent.EXTRA_STREAM)
        }
    }

    private fun transcribe(uri: Uri) {
        val profile = profileSource.currentProfile()
        if (profile !is ConnectionProfile.Server) {
            ui.value = Ui.Failed(getString(R.string.speechkit_action_blocked_no_server))
            return
        }
        ui.value = Ui.Working
        scope.launch {
            val api = SpeechKitServerApi(profile)
            val transcript = runCatching {
                val bytes = withContext(Dispatchers.IO) {
                    contentResolver.openInputStream(uri)?.use { it.readBytes() }
                        ?: error("unreadable")
                }
                val name = displayName(uri)
                val mime = contentResolver.getType(uri)
                    ?: MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "ogg"))
                    ?: "audio/ogg"
                api.transcribe(
                    wav = bytes,
                    filename = name,
                    mediaType = mime,
                ).text.trim()
            }
            val text = transcript.getOrNull().orEmpty()
            if (text.isEmpty()) {
                val reason = transcript.exceptionOrNull()?.message
                    ?: getString(R.string.transcribe_audio_empty)
                Timber.w(transcript.exceptionOrNull(), "Audio transcription failed")
                ui.value = Ui.Failed(reason)
                return@launch
            }
            val delivered = if (summarize) summarizeTranscript(api, text) else text
            val copiedLabel = if (summarize && delivered != text) {
                R.string.summarize_audio_copied
            } else {
                R.string.transcribe_audio_copied
            }
            TranscribeAudioEvents.publish(delivered)
            (getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager)
                .setPrimaryClip(ClipData.newPlainText("transcript", delivered))
            Toast.makeText(this@TranscribeAudioActivity, copiedLabel, Toast.LENGTH_SHORT).show()
            ui.value = Ui.Done(delivered)
            if (insertIntoEditor) finish()
        }
    }

    /**
     * Assist summarize on the paired server. If Assist has no model, keep
     * the transcript so the share is not empty.
     */
    private suspend fun summarizeTranscript(api: SpeechKitServerApi, transcript: String): String {
        val locale = Locale.getDefault()
        val command = if (locale.language == "de") "zusammenfassen" else "summarize this"
        val summary = runCatching {
            api.processAssist(
                text = command,
                locale = locale.toLanguageTag(),
                selection = transcript,
            ).text.trim()
        }
        val text = summary.getOrNull().orEmpty()
        if (text.isEmpty()) {
            Timber.w(summary.exceptionOrNull(), "Voice-note summarize fell back to transcript")
            Toast.makeText(this, R.string.summarize_audio_fallback, Toast.LENGTH_LONG).show()
            return transcript
        }
        return text
    }

    private fun displayName(uri: Uri): String {
        contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                val name = c.getString(0)
                if (!name.isNullOrBlank()) return name
            }
        }
        return uri.lastPathSegment?.substringAfterLast('/') ?: "voice.ogg"
    }

    private sealed class Ui {
        data object Idle : Ui()
        data object Working : Ui()
        data class Done(val text: String) : Ui()
        data class Failed(val reason: String) : Ui()
    }

    companion object {
        const val EXTRA_INSERT: String = "insert"
        const val EXTRA_SUMMARIZE: String = "summarize"
        const val SUMMARIZE_ALIAS_MARKER: String = "SummarizeAudio"

        fun launchPicker(context: Context, summarize: Boolean = false) {
            context.startActivity(
                Intent(context, TranscribeAudioActivity::class.java)
                    .putExtra(EXTRA_INSERT, true)
                    .putExtra(EXTRA_SUMMARIZE, summarize)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
    }
}
