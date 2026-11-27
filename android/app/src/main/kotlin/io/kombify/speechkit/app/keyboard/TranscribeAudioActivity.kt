package io.kombify.speechkit.app.keyboard

import android.Manifest
import android.content.ClipboardManager
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
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
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.R
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.app.hybrid.HybridRuntime
import io.kombify.speechkit.hybrid.LocalAudioPermissionRequired
import io.kombify.speechkit.hybrid.MlKitLocalSpeechAdapter
import kotlinx.coroutines.cancel
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.CancellationException
import io.kombify.speechkit.app.ui.theme.SpeechKitTheme
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.domain.ExecutionPolicy
import io.kombify.speechkit.net.KombifyLinks
import io.kombify.speechkit.net.R as NetR
import io.kombify.speechkit.net.SpeechKitApiException
import io.kombify.speechkit.net.SpeechKitServerApi
import io.kombify.speechkit.net.StoredConnectionSelection
import io.kombify.speechkit.net.userText
import io.kombify.speechkit.stt.parakeet.ParakeetModels
import io.kombify.speechkit.stt.parakeet.ParakeetLanguages
import io.kombify.speechkit.stt.LANGUAGE_MULTI
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
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
 * the keyboard more-page opens a picker. The shared hybrid runtime applies
 * the explicit invocation policy to replayable local adapters or the selected
 * endpoint. The microphone-owning platform recognizer cannot read files.
 */
@AndroidEntryPoint
class TranscribeAudioActivity : ComponentActivity() {

    @Inject lateinit var profileSource: ConnectionProfileSource

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val ui = MutableStateFlow<Ui>(Ui.Idle)
    private var insertIntoEditor = false
    private var summarize = false
    private var invocation: Invocation? = null
    private var invalidInvocation = false
    private val deviceLocale = Locale.getDefault()
    private var pendingPermission: AudioImport? = null
    private data class AudioImport(val uri: Uri, val language: String, val revision: Long)

    private val picker = registerForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri == null) {
            finish()
            return@registerForActivityResult
        }
        ui.value = Ui.Ready(uri)
    }
    private val audioPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        val pending = pendingPermission
        pendingPermission = null
        if (pending != null && !isFinishing) {
            if (StoredConnectionSelection(this).revision() != pending.revision) {
                ui.value = Ui.Failed(getString(R.string.hybrid_failed))
            } else if (granted) {
                transcribe(pending.uri, pending.language, pending.revision)
            } else {
                ui.value = Ui.Ready(pending.uri, getString(R.string.hybrid_audio_permission_required))
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        insertIntoEditor = intent.getBooleanExtra(EXTRA_INSERT, false)
        val nonce = if (intent.action != Intent.ACTION_SEND) intent.getStringExtra(EXTRA_INVOCATION) else null
        invocation = nonce?.let(::consumeInvocation)
        invalidInvocation = nonce != null && invocation == null
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
                var language by remember { mutableStateOf(LANGUAGE_MULTI) }
                var languageMenu by remember { mutableStateOf(false) }
                val languageOptions = remember {
                    (ParakeetLanguages.codes + MlKitLocalSpeechAdapter.supportedLanguageTags)
                        .sortedBy { Locale.forLanguageTag(it).getDisplayName(deviceLocale) }
                }
                fun languageName(tag: String): String = if (tag == LANGUAGE_MULTI) getString(R.string.audio_language_multi)
                    else Locale.forLanguageTag(tag).getDisplayName(deviceLocale).ifBlank { tag } + " ($tag)"
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(24.dp)
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(stringResource(titleRes), style = MaterialTheme.typography.titleMedium)
                    Text(stringResource(R.string.audio_recognition_language, languageName(language)),
                        style = MaterialTheme.typography.bodySmall)
                    when (val s = state) {
                        Ui.Idle, Ui.Working -> CircularProgressIndicator()
                        is Ui.Ready -> {
                            Box {
                                OutlinedButton(onClick = { languageMenu = true }) { Text(languageName(language)) }
                                DropdownMenu(expanded = languageMenu, onDismissRequest = { languageMenu = false },
                                    modifier = Modifier.heightIn(max = 320.dp)) {
                                    DropdownMenuItem(text = { Text(stringResource(R.string.audio_language_multi)) },
                                        onClick = { language = LANGUAGE_MULTI; languageMenu = false })
                                    languageOptions.forEach { tag ->
                                        DropdownMenuItem(text = { Text(languageName(tag)) },
                                            onClick = { language = tag; languageMenu = false })
                                    }
                                }
                            }
                            Text(stringResource(R.string.audio_language_hint), style = MaterialTheme.typography.bodySmall)
                            s.notice?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                            Button(onClick = { transcribe(s.uri, language) }) { Text(stringResource(R.string.audio_start_transcription)) }
                        }
                        is Ui.Done -> {
                            Text(s.text, style = MaterialTheme.typography.bodyLarge)
                            if (s.source.isNotBlank()) Text(s.source, style = MaterialTheme.typography.bodySmall)
                            Button(onClick = {
                                (getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager)
                                    .setPrimaryClip(ClipData.newPlainText("transcript", s.text))
                            }) { Text(stringResource(R.string.hybrid_copy)) }
                        }
                        is Ui.Failed -> Text(s.reason, color = MaterialTheme.colorScheme.error)
                        is Ui.PlanDenied -> {
                            Text(s.title, style = MaterialTheme.typography.titleSmall)
                            Text(s.body, color = MaterialTheme.colorScheme.error)
                            Button(onClick = { KombifyLinks.open(this@TranscribeAudioActivity, KombifyLinks.PLANS_URL) }) {
                                Text(stringResource(NetR.string.plan_denial_see_plans))
                            }
                        }
                    }
                    Button(onClick = ::finish) { Text(stringResource(R.string.shortcuts_close)) }
                }
            }
        }
        if (invalidInvocation) { ui.value = Ui.Failed(getString(R.string.hybrid_failed)); return }
        val shared = sharedAudioUri()
        when {
            shared != null -> ui.value = Ui.Ready(shared)
            insertIntoEditor || intent.action == Intent.ACTION_MAIN -> picker.launch("audio/*")
            else -> finish()
        }
    }

    private fun sharedAudioUri(): Uri? {
        if (intent.action != Intent.ACTION_SEND) return null
        val shared = if (Build.VERSION.SDK_INT >= 33) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM)
        }
        return shared?.takeIf { it.scheme == "content" }
    }

    override fun onDestroy() {
        pendingPermission = null
        scope.cancel()
        super.onDestroy()
    }

    private fun transcribe(uri: Uri, language: String, permissionRevision: Long? = null) {
        if (BuildConfig.HYBRID_ENABLED) {
            ui.value = Ui.Working
            val selection = StoredConnectionSelection(this)
            val route = selection.snapshot(io.kombify.speechkit.app.build.ShippedDefaults.shippedProfile())
            val expectedRevision = permissionRevision ?: invocation?.revision ?: route.revision
            if (route.revision != expectedRevision) {
                ui.value = Ui.Failed(getString(R.string.hybrid_failed)); return
            }
            val policy = invocation?.policy
            val profile = if (invocation != null) route.profile else profileSource.currentProfile()
            fun requireSameRoute() {
                check(selection.revision() == expectedRevision) { "Connection changed" }
            }
            val importJob = scope.launch(start = kotlinx.coroutines.CoroutineStart.LAZY) {
                try {
                    requireSameRoute()
                    val transcription = HybridRuntime.transcribe(this@TranscribeAudioActivity, profile, uri, policy,
                        DictationSegmentOptions(language = language))
                    val transcript = transcription.value
                    require(transcript.isNotBlank())
                    requireSameRoute()
                    val summary = if (summarize) HybridRuntime.generate(this@TranscribeAudioActivity, profile,
                        getString(R.string.hybrid_summary_prompt) + "\n\n" + transcript, false, policy) else null
                    requireSameRoute()
                    val nativeModel = ParakeetModels.all.firstOrNull { it.id == transcription.adapterId }
                    val speechSource = when {
                        transcription.location == io.kombify.speechkit.domain.ExecutionLocation.DEVICE && nativeModel != null ->
                            getString(R.string.audio_source_local_model, nativeModel.name)
                        transcription.location == io.kombify.speechkit.domain.ExecutionLocation.DEVICE &&
                            transcription.adapterId in setOf("google_mlkit_basic", "google_mlkit_advanced") ->
                            getString(R.string.audio_source_google_local)
                        transcription.location == io.kombify.speechkit.domain.ExecutionLocation.DEVICE -> getString(R.string.hybrid_source_device)
                        transcription.adapterId == "firebase_cloud" -> getString(R.string.hybrid_source_firebase)
                        else -> getString(R.string.hybrid_source_endpoint)
                    }
                    val textSource = summary?.value?.source?.let { source -> getString(when (source) {
                        io.kombify.speechkit.app.hybrid.HybridTextSource.FIREBASE_DEVICE -> R.string.hybrid_source_device
                        io.kombify.speechkit.app.hybrid.HybridTextSource.FIREBASE_CLOUD -> R.string.hybrid_source_firebase
                        io.kombify.speechkit.app.hybrid.HybridTextSource.SPEECHKIT_ENDPOINT -> R.string.hybrid_source_endpoint
                        io.kombify.speechkit.app.hybrid.HybridTextSource.GEMINI -> R.string.hybrid_source_gemini
                    }) }
                    // Editor focus may have changed during the foreground handoff; require an explicit copy.
                    val resultText = summary?.value?.text ?: transcript
                    val receiptScope = HybridRuntime.scope(profile)
                    val sourceKey = when (summary?.value?.source) {
                        io.kombify.speechkit.app.hybrid.HybridTextSource.FIREBASE_DEVICE -> "firebase_on_device"
                        io.kombify.speechkit.app.hybrid.HybridTextSource.FIREBASE_CLOUD -> "firebase_cloud"
                        io.kombify.speechkit.app.hybrid.HybridTextSource.SPEECHKIT_ENDPOINT -> "speechkit_endpoint"
                        io.kombify.speechkit.app.hybrid.HybridTextSource.GEMINI -> "gemini"
                        null -> transcription.adapterId ?: when {
                            transcription.location == io.kombify.speechkit.domain.ExecutionLocation.DEVICE -> "device_audio"
                            profile is ConnectionProfile.Firebase -> "firebase_cloud"
                            else -> "speechkit_endpoint"
                        }
                    }
                    val store = io.kombify.speechkit.store.RoomStore(this@TranscribeAudioActivity)
                    try {
                        val receipt = io.kombify.speechkit.store.OperationReceipt(receiptScope, java.util.UUID.randomUUID().toString(), sourceKey, "completed", resultText, System.currentTimeMillis())
                        if (receiptScope == "device") store.saveOperation(receipt) else store.saveScopedOperation(receipt)
                    }
                    finally { store.close() }
                    ui.value = Ui.Done(resultText, listOfNotNull(speechSource, textSource).joinToString(" / "))
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (failure: Exception) {
                    val needsAudioPermission = checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED &&
                        generateSequence(failure as Throwable?) { it.cause }.take(8).any { it is LocalAudioPermissionRequired }
                    if (needsAudioPermission && !isFinishing && selection.revision() == expectedRevision) {
                        pendingPermission = AudioImport(uri, language, expectedRevision)
                        audioPermission.launch(Manifest.permission.RECORD_AUDIO)
                    } else {
                        ui.value = Ui.Failed(getString(R.string.hybrid_failed))
                    }
                }
            }
            HybridRuntime.track(this, profile, importJob)
            importJob.invokeOnCompletion { failure ->
                if (failure is CancellationException) scope.launch {
                    if (ui.value == Ui.Working && pendingPermission == null) ui.value = Ui.Failed(getString(R.string.hybrid_failed))
                }
            }
            importJob.start()
            return
        }
        val profile = profileSource.currentProfile()
        val policy = io.kombify.speechkit.net.StoredExecutionPolicy(this).read(profile)
        if (!policy.allowNetwork || policy.mode == io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY) {
            ui.value = Ui.Failed(getString(R.string.speechkit_action_blocked_no_server))
            return
        }
        if (profile !is ConnectionProfile.Server) {
            ui.value = Ui.Failed(getString(R.string.speechkit_action_blocked_no_server))
            return
        }
        ui.value = Ui.Working
        scope.launch {
            val api = SpeechKitServerApi(profile, context = this@TranscribeAudioActivity)
            val transcript = runCatching {
                val bytes = withContext(Dispatchers.IO) {
                    contentResolver.openInputStream(uri)?.use { stream ->
                        val bytes = java.io.ByteArrayOutputStream()
                        val buffer = ByteArray(8192)
                        while (true) {
                            kotlinx.coroutines.currentCoroutineContext().ensureActive()
                            val count = stream.read(buffer)
                            if (count < 0) break
                            require(bytes.size() + count <= HybridRuntime.MAX_AUDIO_BYTES)
                            bytes.write(buffer, 0, count)
                        }
                        bytes.toByteArray()
                    } ?: error("unreadable")
                }
                val name = displayName(uri)
                val mime = contentResolver.getType(uri)
                    ?: MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "ogg"))
                    ?: "audio/ogg"
                api.transcribe(
                    wav = bytes,
                    language = language.takeUnless { it == LANGUAGE_MULTI },
                    filename = name,
                    mediaType = mime,
                ).text.trim()
            }
            (transcript.exceptionOrNull() as? CancellationException)?.let { throw it }
            val text = transcript.getOrNull().orEmpty()
            val denial = (transcript.exceptionOrNull() as? SpeechKitApiException)?.denial
            if (denial != null) {
                val message = denial.userText(this@TranscribeAudioActivity)
                ui.value = Ui.PlanDenied(message.title, message.body)
                return@launch
            }
            if (text.isEmpty()) {
                val reason = transcript.exceptionOrNull()?.message
                    ?: getString(R.string.transcribe_audio_empty)
                Timber.w(transcript.exceptionOrNull(), "Audio transcription failed")
                ui.value = Ui.Failed(reason)
                return@launch
            }
            val delivered = try {
                if (summarize) summarizeTranscript(api, text) else text
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: SpeechKitApiException) {
                val denial = failure.denial
                ui.value = if (denial != null) denial.userText(this@TranscribeAudioActivity).let { Ui.PlanDenied(it.title, it.body) }
                    else Ui.Failed(getString(R.string.hybrid_failed))
                return@launch
            } catch (_: Exception) { ui.value = Ui.Failed(getString(R.string.hybrid_failed)); return@launch }
            // A foreground handoff can change editors. The person chooses Copy from the result.
            ui.value = Ui.Done(delivered)
        }
    }

    /**
     * Assist summarize on the paired server. If Assist has no model, keep
     * the transcript so the share is not empty.
     */
    private suspend fun summarizeTranscript(api: SpeechKitServerApi, transcript: String): String {
        val locale = Locale.getDefault()
        val command = if (locale.language == "de") "zusammenfassen" else "summarize this"
        return api.processAssist(
            text = command,
            locale = locale.toLanguageTag(),
            selection = transcript,
        ).text.trim().ifBlank { transcript }
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
        data class Ready(val uri: Uri, val notice: String? = null) : Ui()
        data class Done(val text: String, val source: String = "") : Ui()
        data class Failed(val reason: String) : Ui()
        data class PlanDenied(val title: String, val body: String) : Ui()
    }

    companion object {
        const val EXTRA_INSERT: String = "insert"
        const val EXTRA_SUMMARIZE: String = "summarize"
        const val SUMMARIZE_ALIAS_MARKER: String = "SummarizeAudio"
        private const val EXTRA_INVOCATION = "speechkit.invocation_nonce"
        private const val INVOCATION_TTL_MS = 5 * 60 * 1000L
        private data class Invocation(val nonce: String, val policy: ExecutionPolicy, val revision: Long, val createdAt: Long)
        private var pendingInvocation: Invocation? = null

        @Synchronized private fun mintInvocation(policy: ExecutionPolicy, revision: Long): String {
            val nonce = java.util.UUID.randomUUID().toString()
            pendingInvocation = Invocation(nonce, policy, revision, android.os.SystemClock.elapsedRealtime())
            return nonce
        }
        @Synchronized private fun consumeInvocation(nonce: String): Invocation? {
            val pending = pendingInvocation ?: return null
            if (pending.nonce != nonce) return null
            pendingInvocation = null
            return pending.takeIf { android.os.SystemClock.elapsedRealtime() - it.createdAt in 0..INVOCATION_TTL_MS }
        }

        fun launchPicker(context: Context, summarize: Boolean = false, policy: ExecutionPolicy? = null, expectedRevision: Long? = null) {
            require((policy == null) == (expectedRevision == null))
            val nonce = if (policy != null && expectedRevision != null) mintInvocation(policy, expectedRevision) else null
            context.startActivity(
                Intent(context, TranscribeAudioActivity::class.java)
                    .putExtra(EXTRA_INSERT, true)
                    .putExtra(EXTRA_SUMMARIZE, summarize)
                    .apply { if (nonce != null) putExtra(EXTRA_INVOCATION, nonce) }
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
    }
}
