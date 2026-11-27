package io.kombify.speechkit.app.hybrid

import android.Manifest
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.R
import io.kombify.speechkit.app.ui.theme.SpeechKitTheme
import io.kombify.speechkit.app.ui.MainActivity
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.domain.*
import io.kombify.speechkit.hybrid.*
import io.kombify.speechkit.net.*
import io.kombify.speechkit.store.*
import io.kombify.speechkit.stt.LANGUAGE_MULTI
import io.kombify.speechkit.stt.parakeet.ParakeetLanguages
import io.kombify.speechkit.stt.parakeet.ParakeetModels
import io.kombify.speechkit.stt.streaming.*
import kotlinx.coroutines.*
import java.util.Locale
import java.util.UUID
import javax.inject.Inject

private enum class HybridTask { TEXT, AUDIO, VOICE_AGENT, NOTES }
private enum class PendingMicrophone { TRANSCRIPTION, LOCAL_NOTES, VOICE_AGENT }
private data class AudioFileRequest(val language: String, val revision: Long)

@AndroidEntryPoint
class HybridActivity : ComponentActivity() {
    @Inject lateinit var profiles: ConnectionProfileSource
    private lateinit var draftProfile: ConnectionProfile
    private var input by mutableStateOf("")
    private var output by mutableStateOf("")
    private var status by mutableStateOf("")
    private var busy by mutableStateOf(false)
    private var recording by mutableStateOf(false)
    private var localVoiceRequested = false
    private var localVoiceEntry = false
    private var runCapturedLocalVoice = false
    private var agentConnected by mutableStateOf(false)
    private var agentRecording by mutableStateOf(false)
    // In-memory only: permission results delivered after recreation are ignored.
    private var pendingMicrophone by mutableStateOf<PendingMicrophone?>(null)
    private var pendingAudioPicker by mutableStateOf<AudioFileRequest?>(null)
    private var pendingAudioPermission by mutableStateOf<Pair<Uri, AudioFileRequest>?>(null)
    private var selectionRevision = 0L
    private var selectedAgent: VoiceAgentSessionDriver? = null
    private var harnesses by mutableStateOf<List<VoiceHarness>>(emptyList())
    private var work: Job? = null
    private var capture: Job? = null
    private var policyListener: java.io.Closeable? = null
    private var voice: StreamingSttSession? = null
    private val activityScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val picker = registerForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        val request = pendingAudioPicker
        pendingAudioPicker = null
        if (uri != null && request != null) importAudio(uri, request)
    }
    private val audioPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        val pending = pendingAudioPermission
        pendingAudioPermission = null
        if (pending != null && isCurrentAudioRequest(pending.second)) {
            if (granted) importAudio(pending.first, pending.second)
            else status = getString(R.string.hybrid_audio_permission_required)
        }
    }
    private val permission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        val request = pendingMicrophone
        pendingMicrophone = null
        if (request != null && granted) {
            when (request) {
                PendingMicrophone.VOICE_AGENT -> if (agentConnected && selectedAgent != null) beginAgentTurn()
                PendingMicrophone.LOCAL_NOTES -> { localVoiceRequested = true; startRecording() }
                PendingMicrophone.TRANSCRIPTION -> { localVoiceRequested = false; startRecording() }
            }
        } else localVoiceRequested = false
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!BuildConfig.HYBRID_ENABLED) { finish(); return }
        draftProfile = profiles.currentProfile()
        val selection = StoredConnectionSelection(this)
        selectionRevision = selection.revision()
        localVoiceEntry = intent.hasExtra(EXTRA_LOCAL_VOICE_REVISION)
        if (localVoiceEntry) {
            if (intent.getLongExtra(EXTRA_LOCAL_VOICE_REVISION, -1) != selectionRevision ||
                StoredExecutionPolicy(this).read(draftProfile).mode == ExecutionMode.ENDPOINT_ONLY) { finish(); return }
            runCapturedLocalVoice = savedInstanceState == null && !intent.getStringExtra(EXTRA_TEXT).isNullOrBlank()
        }
        policyListener = NetworkUseGuard.listen {
            val jobs = listOfNotNull(work, capture)
            cancelWork()
            jobs
        }
        val settings = StoredExecutionPolicy(this)
        val draftKey = HybridRuntime.scope(draftProfile)
        val restored = savedInstanceState?.takeIf { it.getString("draft_scope") == draftKey }?.getString(EXTRA_TEXT)
        val savedDraft = if (localVoiceEntry) null else getSharedPreferences("speechkit_hybrid_drafts", Context.MODE_PRIVATE)
            .getString(draftKey, "")
        updateInput((restored ?: intent.getStringExtra(EXTRA_TEXT) ?: savedDraft).orEmpty())
        activityScope.launch {
            if (!localVoiceEntry && !settings.isConfigured()) {
                settings.write(settings.read(draftProfile))
                draftProfile = profiles.currentProfile()
                selectionRevision = selection.revision()
            }
            setContent { SpeechKitTheme { Surface(Modifier.fillMaxSize()) { Screen() } } }
        }
    }

    @OptIn(ExperimentalLayoutApi::class)
    @Composable private fun Screen() {
        var policy by remember {
            mutableStateOf(if (localVoiceEntry) ExecutionPolicy(ExecutionMode.DEVICE_ONLY)
                else StoredExecutionPolicy(this@HybridActivity).read(draftProfile))
        }
        var changingPolicy by remember { mutableStateOf(false) }
        val policyScope = rememberCoroutineScope()
        fun changePolicy(next: ExecutionPolicy) {
            if (changingPolicy) return
            changingPolicy = true
            policyScope.launch {
                try {
                    val selection = StoredConnectionSelection(this@HybridActivity)
                    if (BuildConfig.CONNECTION_SETUP_ENABLED) selection.updatePreference(next.mode)
                    else StoredExecutionPolicy(this@HybridActivity).write(next)
                    draftProfile = profiles.currentProfile()
                    selectionRevision = selection.revision()
                    policy = StoredExecutionPolicy(this@HybridActivity).read(draftProfile)
                }
                catch (_: Exception) { status = getString(R.string.hybrid_failed) }
                finally { changingPolicy = false }
            }
        }
        var firebaseCloud by remember { mutableStateOf(false) }
        var task by rememberSaveable { mutableStateOf(if (localVoiceEntry) HybridTask.NOTES else HybridTask.TEXT) }
        var showAdvanced by remember(task) { mutableStateOf(false) }
        val deviceLocale = remember { Locale.getDefault() }
        var audioLanguage by rememberSaveable { mutableStateOf(deviceLocale.toLanguageTag()) }
        val idle = !busy && !changingPolicy && pendingMicrophone == null && pendingAudioPicker == null && pendingAudioPermission == null
        var selectedHarnessId by remember { mutableStateOf("") }
        val remoteRoute = !localVoiceEntry && (draftProfile is ConnectionProfile.Server ||
            draftProfile is ConnectionProfile.Byok || draftProfile is ConnectionProfile.Firebase)
        val unsupportedAudio = task == HybridTask.AUDIO && draftProfile is ConnectionProfile.Byok &&
            policy.mode == ExecutionMode.ENDPOINT_ONLY
        val cloudAllowed = !BuildConfig.CONNECTION_SETUP_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED && !StoredServerProfile.cloudSelected(this@HybridActivity)
        Column(Modifier.fillMaxSize().safeDrawingPadding().imePadding().verticalScroll(rememberScrollState()).padding(20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.hybrid_title), style = MaterialTheme.typography.headlineSmall)
            if (remoteRoute) {
                HybridDropdown(
                    label = stringResource(R.string.hybrid_destination), selected = policy.mode,
                    enabled = !busy && !changingPolicy,
                    options = listOf(
                        ExecutionMode.DEVICE_ONLY to stringResource(R.string.hybrid_device_only),
                        ExecutionMode.PREFER_DEVICE to stringResource(R.string.hybrid_prefer_device),
                        ExecutionMode.PREFER_ENDPOINT to stringResource(R.string.hybrid_prefer_endpoint),
                        ExecutionMode.ENDPOINT_ONLY to stringResource(R.string.connection_setup_cloud_only),
                    ),
                    onSelect = { mode -> changePolicy(policy.copy(mode = mode, allowNetwork = mode != ExecutionMode.DEVICE_ONLY)) },
                )
                if (!BuildConfig.CONNECTION_SETUP_ENABLED) Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(enabled = !busy && !changingPolicy, checked = policy.allowNetwork, onCheckedChange = { changePolicy(policy.copy(allowNetwork = it)) })
                    Text(stringResource(R.string.hybrid_allow_network))
                }
            } else {
                Text(stringResource(R.string.hybrid_local_route_detail), style = MaterialTheme.typography.bodyMedium)
                TextButton(enabled = !busy, onClick = {
                    startActivity(Intent(this@HybridActivity, MainActivity::class.java).putExtra(MainActivity.EXTRA_OPEN_CONNECTION, true))
                }) { Text(stringResource(R.string.hybrid_connection_settings)) }
            }
            if (!localVoiceEntry) HybridDropdown(
                label = stringResource(R.string.hybrid_task_label), selected = task, enabled = idle && !agentConnected,
                options = listOf(
                    HybridTask.TEXT to stringResource(R.string.hybrid_task_text),
                    HybridTask.AUDIO to stringResource(R.string.hybrid_task_audio),
                    HybridTask.VOICE_AGENT to stringResource(R.string.hybrid_task_voice_agent),
                    HybridTask.NOTES to stringResource(R.string.hybrid_task_notes),
                ),
                onSelect = { next -> if (task != next) { task = next; status = ""; output = "" } },
            )
            if (unsupportedAudio) Text(stringResource(R.string.hybrid_audio_local_required))
            if (task == HybridTask.AUDIO && !unsupportedAudio) {
                val languageOptions = remember(deviceLocale) {
                    (ParakeetLanguages.codes + MlKitLocalSpeechAdapter.supportedLanguageTags + deviceLocale.toLanguageTag())
                        .map { tag -> tag to "${Locale.forLanguageTag(tag).getDisplayName(deviceLocale)} ($tag)" }
                        .sortedBy { it.second }
                }
                HybridDropdown(
                    label = stringResource(R.string.hybrid_audio_language), selected = audioLanguage,
                    options = listOf(LANGUAGE_MULTI to stringResource(R.string.audio_language_multi)) + languageOptions,
                    enabled = idle, onSelect = { audioLanguage = it },
                )
            }
            if (task == HybridTask.TEXT && cloudAllowed) Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(enabled = !busy, checked = firebaseCloud, onCheckedChange = { firebaseCloud = it })
                Text(stringResource(R.string.hybrid_firebase_cloud))
            }
            if (task == HybridTask.VOICE_AGENT) {
                val server = (draftProfile as? ConnectionProfile.Server)?.takeIf { it.credentialAvailable }
                val target = server?.let { StoredVoiceAgentTarget(this@HybridActivity).read(it) }
                val canUseRemoteAgent = policy.allowNetwork &&
                    (policy.mode == ExecutionMode.PREFER_ENDPOINT || policy.mode == ExecutionMode.ENDPOINT_ONLY)
                val discoveryServer = server?.takeIf {
                    StoredServerProfile.cloudSelected(this@HybridActivity) && it.normalizedBaseUrl.endsWith("/v1/speechkit")
                }
                if (agentConnected) {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Button(enabled = pendingMicrophone == null, onClick = { if (agentRecording) endAgentTurn() else beginAgentTurn() }) {
                            Text(stringResource(if (agentRecording) R.string.hybrid_stop else R.string.hybrid_dictate))
                        }
                        Button(enabled = input.isNotBlank(), onClick = {
                            val text = input
                            activityScope.launch { selectedAgent?.sendText(text) }
                        }) { Text(stringResource(R.string.hybrid_send_selected)) }
                    }
                } else {
                    when {
                        !remoteRoute -> Text(stringResource(R.string.hybrid_voice_needs_connection), style = MaterialTheme.typography.bodyMedium)
                        !canUseRemoteAgent -> Text(
                            stringResource(
                                R.string.hybrid_voice_remote_processing,
                                stringResource(R.string.hybrid_prefer_endpoint),
                                stringResource(R.string.connection_setup_cloud_only),
                            ),
                            style = MaterialTheme.typography.bodyMedium,
                        )
                        discoveryServer != null -> {
                            Button(enabled = idle, onClick = { launchWork {
                                harnesses = SpeechKitServerApi(discoveryServer, context = this@HybridActivity).discoverVoiceHarnesses("mobile")
                                if (harnesses.isEmpty()) status = getString(R.string.hybrid_no_agents)
                            } }) { Text(stringResource(R.string.hybrid_find_agents)) }
                            if (harnesses.isNotEmpty()) {
                                val selected = harnesses.firstOrNull { it.id == selectedHarnessId } ?: harnesses.first()
                                HybridDropdown(
                                    label = stringResource(R.string.hybrid_agent_choice), selected = selected.id,
                                    options = harnesses.map { it.id to it.id }, enabled = idle,
                                    onSelect = { selectedHarnessId = it },
                                )
                                Button(enabled = idle, onClick = { launchWork {
                                    val session = SpeechKitServerApi(discoveryServer, context = this@HybridActivity).createVoiceAgentChat(selected, "mobile")
                                    StoredVoiceAgentTarget(this@HybridActivity).write(discoveryServer, session)
                                    status = getString(R.string.hybrid_agent_selected, selected.id)
                                } }) { Text(stringResource(R.string.hybrid_choose_agent)) }
                            }
                        }
                        target == null -> Text(stringResource(R.string.hybrid_voice_keyboard), style = MaterialTheme.typography.bodyMedium)
                    }
                    if (canUseRemoteAgent && target != null) {
                        Button(enabled = idle, onClick = ::startSelectedAgent) { Text(stringResource(R.string.hybrid_run_selected)) }
                    }
                }
            }
            TextButton(onClick = { showAdvanced = !showAdvanced }) {
                Text(stringResource(if (showAdvanced) R.string.hybrid_details_hide else R.string.hybrid_details_show))
            }
            if (showAdvanced && task != HybridTask.VOICE_AGENT) {
                ModelActions(task, audioLanguage, idle)
            }
            if (!unsupportedAudio && (task != HybridTask.VOICE_AGENT || agentConnected)) {
                OutlinedTextField(value = input, onValueChange = { updateInput(it) }, label = { Text(stringResource(R.string.hybrid_input)) }, modifier = Modifier.fillMaxWidth(), minLines = 4)
            }
            if (task == HybridTask.AUDIO && !unsupportedAudio) FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                RecordingButton(PendingMicrophone.TRANSCRIPTION, R.string.hybrid_dictate, idle)
                Button(enabled = idle, onClick = {
                    pendingAudioPicker = AudioFileRequest(audioLanguage, selectionRevision)
                    picker.launch("audio/*")
                }) { Text(stringResource(R.string.hybrid_audio)) }
            }
            if (task == HybridTask.TEXT) FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Button(enabled = !busy && input.isNotBlank(), onClick = { generate(getString(R.string.hybrid_summary_prompt), firebaseCloud) }) { Text(stringResource(R.string.hybrid_summarize)) }
                Button(enabled = !busy && input.isNotBlank(), onClick = { generate(getString(R.string.hybrid_rewrite_prompt), firebaseCloud) }) { Text(stringResource(R.string.hybrid_rewrite)) }
            }
            if (task == HybridTask.NOTES) {
                val notesEnabled = idle && policy.mode != ExecutionMode.ENDPOINT_ONLY
                Button(enabled = notesEnabled && input.isNotBlank(), onClick = { runAgent(false, NoteAccess.READ_ONLY) }) { Text(stringResource(R.string.hybrid_note_agent)) }
                Button(enabled = notesEnabled && input.isNotBlank(), onClick = { runAgent(false, NoteAccess.SAVE_ONCE) }) { Text(stringResource(R.string.hybrid_save_note)) }
                RecordingButton(PendingMicrophone.LOCAL_NOTES, R.string.hybrid_local_voice, notesEnabled)
            }
            if (task == HybridTask.TEXT) Button(enabled = !busy && input.isNotBlank(), onClick = { generate("", firebaseCloud) }) { Text(stringResource(R.string.hybrid_ask)) }
            if (busy) Button(onClick = ::cancelWork) { Text(stringResource(R.string.hybrid_cancel)) }
            if (status.isNotBlank()) Text(status)
            if (showAdvanced) {
            Button(enabled = !busy, onClick = { launchWork {
                val store = RoomStore(this@HybridActivity)
                try {
                    val records = store.listOperations("device") + store.listOperations(endpointScope()).takeIf { endpointScope() != "device" }.orEmpty()
                    output = records.filter { (it.source != "remote_agent" && it.state == "completed" && it.content.isNotBlank()) || (it.source == "remote_agent" && it.state in setOf("unknown", "dispatched", "completed")) }
                        .sortedBy { it.updatedAt }.takeLast(20).joinToString("\n\n") {
                            if (it.source == "remote_agent") getString(if (it.state != "completed") R.string.hybrid_unknown_turn else R.string.hybrid_remote_completed) else {
                                val source = when (it.source) {
                                    "firebase_on_device" -> getString(R.string.hybrid_source_device)
                                    "firebase_cloud" -> getString(R.string.hybrid_source_firebase)
                                    "speechkit_endpoint" -> getString(R.string.hybrid_source_endpoint)
                                    "gemini" -> getString(R.string.hybrid_source_gemini)
                                    "google_mlkit_basic", "google_mlkit_advanced" -> getString(R.string.audio_source_google_local)
                                    else -> io.kombify.speechkit.stt.parakeet.ParakeetModels.byId(it.source)?.let { model ->
                                        getString(R.string.audio_source_local_model, model.name)
                                    } ?: getString(R.string.hybrid_local_result)
                                }
                                "$source: ${it.content}"
                            }
                        }
                }
                finally { store.close() }
            } }) { Text(stringResource(R.string.hybrid_history)) }
            Button(enabled = !busy && remoteRoute && policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY && draftProfile is ConnectionProfile.Server, onClick = { launchWork {
                val profile = draftProfile as? ConnectionProfile.Server ?: return@launchWork
                val policyNow = StoredExecutionPolicy(this@HybridActivity).read(profile)
                check(policyNow.allowNetwork && policyNow.mode != ExecutionMode.DEVICE_ONLY)
                check(StoredServerProfile.withActiveProfile(this@HybridActivity, profile) {})
                val accountScope = profile.storageScope()
                val store = RoomStore(this@HybridActivity)
                try {
                    val api = SpeechKitServerApi(profile, context = this@HybridActivity)
                    val sessions = store.listOperations(accountScope).filter { it.source == "remote_session" }.map { it.content }.distinct()
                    for (session in sessions) {
                        for (receipt in api.listVoiceTurnReceipts(session)) {
                            store.saveRemoteTurnReceipt(accountScope, receipt.sessionId, receipt.turnId, receipt.status)
                        }
                    }
                    status = getString(R.string.hybrid_receipts_checked)
                } finally { store.close() }
            } }) { Text(stringResource(R.string.hybrid_check_receipts)) }
            Button(enabled = !busy, onClick = { launchWork {
                val store = RoomStore(this@HybridActivity)
                try { store.deleteOperations("device") } finally { store.close() }
                getSharedPreferences("speechkit_hybrid_drafts", Context.MODE_PRIVATE).edit().remove("device").apply()
                output = ""
                status = getString(R.string.hybrid_erased)
            } }) { Text(stringResource(R.string.hybrid_erase_history)) }
            }
            if (output.isNotBlank()) {
                Text(output, style = MaterialTheme.typography.bodyLarge)
                Button(onClick = { (getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("SpeechKit", output)) }) { Text(stringResource(R.string.hybrid_copy)) }
            }
        }
    }

    @OptIn(ExperimentalLayoutApi::class)
    @Composable
    private fun ModelActions(task: HybridTask, audioLanguage: String, enabled: Boolean) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            when (task) {
                HybridTask.TEXT -> {
                    Button(enabled = enabled, onClick = { launchWork {
                        val model = HybridRuntime.model(this@HybridActivity)
                        val readiness = model.readiness()
                        status = if (readiness == DeviceReadiness.AVAILABLE) "${readiness.name} - ${model.deviceModelName()}" else readiness.name
                    } }) { Text(stringResource(R.string.hybrid_check)) }
                    Button(enabled = enabled, onClick = { launchWork {
                        val model = HybridRuntime.model(this@HybridActivity)
                        model.download { status = it.toString() }
                        status = model.readiness().name
                    } }) { Text(stringResource(R.string.hybrid_download)) }
                }
                HybridTask.AUDIO -> {
                    if (audioLanguage in MlKitLocalSpeechAdapter.supportedLanguageTags) {
                        Button(enabled = enabled, onClick = { launchWork {
                            prepareSpeechModel(audioLanguage)
                        } }) { Text(stringResource(R.string.hybrid_download_speech)) }
                    } else Text(stringResource(R.string.audio_language_hint), style = MaterialTheme.typography.bodySmall)
                }
                HybridTask.NOTES -> {
                    Button(enabled = enabled, onClick = { launchWork {
                        val store = RoomStore(this@HybridActivity)
                        try {
                            val agent = LocalNoteAgent(this@HybridActivity, store)
                            status = if (agent.noteToolsSupported()) agent.readiness().name else "Android 16+"
                        } finally { store.close() }
                    } }) { Text(stringResource(R.string.hybrid_agent_check)) }
                    Button(enabled = enabled, onClick = { launchWork {
                        val store = RoomStore(this@HybridActivity)
                        try {
                            val agent = LocalNoteAgent(this@HybridActivity, store)
                            check(agent.noteToolsSupported())
                            agent.download { status = it.toString() }
                        } finally { store.close() }
                    } }) { Text(stringResource(R.string.hybrid_agent_download)) }
                }
                HybridTask.VOICE_AGENT -> Unit
            }
        }
    }

    private suspend fun prepareSpeechModel(language: String) {
        val locale = Locale.forLanguageTag(language)
        for (advanced in listOf(true, false)) {
            val ready = try {
                ImportedAudio(locale, advanced).use { model ->
                    when (model.readiness()) {
                        DeviceReadiness.AVAILABLE -> true
                        DeviceReadiness.DOWNLOADABLE, DeviceReadiness.DOWNLOADING -> {
                            model.download { status = it.toString() }
                            model.readiness() == DeviceReadiness.AVAILABLE
                        }
                        else -> false
                    }
                }
            } catch (failure: ExecutionFailure) {
                if (failure.kind != ExecutionFailureKind.UNAVAILABLE) throw failure
                false
            }
            if (ready) {
                status = getString(R.string.hybrid_ready)
                return
            }
        }
        status = getString(R.string.hybrid_audio_model_unavailable)
    }

    @Composable
    private fun RecordingButton(request: PendingMicrophone, startLabel: Int, enabled: Boolean) {
        Button(enabled = enabled || recording, onClick = {
            if (recording) {
                stopRecording()
            } else if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
                localVoiceRequested = request == PendingMicrophone.LOCAL_NOTES
                startRecording()
            } else {
                pendingMicrophone = request
                permission.launch(Manifest.permission.RECORD_AUDIO)
            }
        }) { Text(stringResource(if (recording) R.string.hybrid_stop else startLabel)) }
    }

    private fun isCurrentAudioRequest(request: AudioFileRequest): Boolean = !isFinishing &&
        request.revision == selectionRevision && request.revision == StoredConnectionSelection(this).revision()

    private fun importAudio(uri: Uri, request: AudioFileRequest) {
        if (!isCurrentAudioRequest(request)) return
        launchWork {
            val result = try {
                HybridRuntime.transcribe(this, draftProfile, uri, options = DictationSegmentOptions(language = request.language))
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Exception) {
                val needsAudioPermission = checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED &&
                    generateSequence(failure as Throwable?) { it.cause }.take(8).any { it is LocalAudioPermissionRequired }
                if (!needsAudioPermission) throw failure
                if (isCurrentAudioRequest(request)) {
                    pendingAudioPermission = uri to request
                    audioPermission.launch(Manifest.permission.RECORD_AUDIO)
                }
                return@launchWork
            }
            if (!isCurrentAudioRequest(request)) return@launchWork
            require(result.value.isNotBlank())
            updateInput(result.value)
            val model = result.adapterId?.let(ParakeetModels::byId)
            status = when (result.adapterId) {
                "firebase_cloud" -> getString(R.string.hybrid_source_firebase)
                "speechkit_endpoint" -> getString(R.string.hybrid_source_endpoint)
                "google_mlkit_basic", "google_mlkit_advanced" -> getString(R.string.audio_source_google_local)
                else -> if (model != null) getString(R.string.audio_source_local_model, model.name)
                    else getString(R.string.hybrid_source_device)
            }
        }
    }

    private fun launchWork(block: suspend () -> Unit) {
        if (busy) return
        busy = true
        work = activityScope.launch(start = CoroutineStart.LAZY) {
            try { block() } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { status = getString(R.string.hybrid_failed) }
            finally { busy = false }
        }
        work?.let { job ->
            HybridRuntime.track(this, draftProfile, job)
            job.invokeOnCompletion { if (work === job) activityScope.launch { busy = false } }
            job.start()
        }
    }

    private fun generate(instruction: String, cloud: Boolean) {
        val text = input
        val profile = draftProfile
        launchWork {
            val result = HybridRuntime.generate(this, profile, "$instruction\n\n$text", cloud)
            output = result.value.text
            val source = when (result.value.source) {
                HybridTextSource.FIREBASE_DEVICE -> "firebase_on_device"
                HybridTextSource.FIREBASE_CLOUD -> "firebase_cloud"
                HybridTextSource.SPEECHKIT_ENDPOINT -> "speechkit_endpoint"
                HybridTextSource.GEMINI -> "gemini"
            }
            status = getString(when (source) { "firebase_on_device" -> R.string.hybrid_source_device; "firebase_cloud" -> R.string.hybrid_source_firebase; "gemini" -> R.string.hybrid_source_gemini; else -> R.string.hybrid_source_endpoint }) +
                if (result.fallbackReason != null) " - " + getString(R.string.hybrid_fallback) else ""
            persistResult(source, output, HybridRuntime.scope(profile))
        }
    }

    private fun runAgent(speak: Boolean, access: NoteAccess) {
        if (StoredExecutionPolicy(this).read(draftProfile).mode == ExecutionMode.ENDPOINT_ONLY) { status = getString(R.string.hybrid_failed); return }
        val text = input
        val operationId = UUID.randomUUID().toString()
        launchWork {
            val store = RoomStore(this)
            try {
                val turnScope = HybridRuntime.scope(draftProfile)
                suspend fun saveTurn(state: String, content: String) {
                    val receipt = OperationReceipt(turnScope, operationId, "local_agent", state, content, System.currentTimeMillis())
                    if (turnScope == "device") store.saveOperation(receipt) else store.saveScopedOperation(receipt)
                }
                saveTurn("started", "")
                // The note tool owns a device-local receipt; conversation context stays with its captured profile.
                val completedContext = store.listOperations(turnScope).filter { it.source == "local_agent" && it.state == "completed" }
                    .takeLast(4).joinToString("\n") { it.content }.takeLast(4000)
                val answer = LocalNoteAgent(this, store).run("device", "$operationId:note", text, completedContext, access)
                saveTurn("completed", answer)
                output = answer
                val receipt = store.getOperation("device", "$operationId:note")
                status = if (receipt?.source == "local_note" && receipt.state == "completed")
                    getString(R.string.hybrid_saved_note_id, receipt.content) else getString(R.string.hybrid_local_result)
                if (speak) io.kombify.speechkit.app.voice.NeuralVoiceEngine.get(this).speak(answer, java.util.Locale.getDefault())
            } finally { store.close() }
        }
    }

    private suspend fun persistResult(source: String, value: String, scope: String) {
        val store = RoomStore(this)
        try {
            val receipt = OperationReceipt(scope, UUID.randomUUID().toString(), source, "completed", value, System.currentTimeMillis())
            if (scope == "device") store.saveOperation(receipt) else store.saveScopedOperation(receipt)
        }
        finally { store.close() }
    }

    private fun endpointScope(): String = HybridRuntime.scope(draftProfile)
    private fun updateInput(value: String) {
        val bounded = value.take(16000)
        if (localVoiceEntry) {
            input = bounded
            return
        }
        val save = {
            input = bounded
            getSharedPreferences("speechkit_hybrid_drafts", Context.MODE_PRIVATE).edit()
                .putString(HybridRuntime.scope(draftProfile), bounded).apply()
            Unit
        }
        val server = draftProfile as? ConnectionProfile.Server
        if (server == null) save() else if (!StoredServerProfile.withActiveProfile(this, server, save)) {
            input = ""; output = ""; finish()
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putString(EXTRA_TEXT, input)
        outState.putString("draft_scope", HybridRuntime.scope(draftProfile))
        super.onSaveInstanceState(outState)
    }
    private fun startSelectedAgent() {
        if (busy) return
        val profile = draftProfile as? ConnectionProfile.Server ?: return
        val target = StoredVoiceAgentTarget(this).read(profile) ?: return
        busy = true
        work = activityScope.launch(start = CoroutineStart.LAZY) {
            val controller = voiceAgentDriver(profile, renewalClaimNamespace = "", context = this@HybridActivity, target = target)
            val player = io.kombify.speechkit.audio.PcmStreamPlayer(VoiceAgentAudio.SERVER_SAMPLE_RATE)
            val playback = io.kombify.speechkit.audio.PcmPlaybackQueue(VoiceAgentAudio.SERVER_SAMPLE_RATE)
            selectedAgent = controller
            try {
                val events = controller.start()
                agentConnected = true
                coroutineScope {
                    val playing = launch { playback.consume { player.play(it) } }
                    try {
                        events.collect { event ->
                            controller.accept(event)
                            when (event) {
                                is VoiceAgentEvent.Audio -> check(playback.offer(event.pcm))
                                is VoiceAgentEvent.Transcript -> if (!event.input) output = event.text
                                is VoiceAgentEvent.Failure -> status = event.code
                                VoiceAgentEvent.Interrupted -> { playback.clear(); player.flush() }
                                else -> Unit
                            }
                        }
                    } finally { playback.close(); playing.cancelAndJoin() }
                }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { status = getString(R.string.hybrid_failed) }
            finally {
                withContext(NonCancellable) { capture?.cancelAndJoin(); controller.stop() }
                player.release(); selectedAgent = null; agentConnected = false; agentRecording = false; busy = false
            }
        }
        work?.let { job ->
            HybridRuntime.track(this, draftProfile, job)
            job.invokeOnCompletion { if (work === job) activityScope.launch { busy = false; agentConnected = false; agentRecording = false } }
            job.start()
        }
    }

    private fun beginAgentTurn() {
        val controller = selectedAgent ?: return
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            pendingMicrophone = PendingMicrophone.VOICE_AGENT
            permission.launch(Manifest.permission.RECORD_AUDIO)
            return
        }
        agentRecording = true
        capture = activityScope.launch { MicAudioCapture().frames().collect { controller.sendAudio(it) } }
    }

    private fun endAgentTurn() {
        if (!agentRecording) return
        agentRecording = false
        capture?.cancel()
        activityScope.launch { selectedAgent?.endTurn() }
    }

    private fun startRecording() {
        if (busy) return
        if (localVoiceRequested && StoredExecutionPolicy(this).read(draftProfile).mode == ExecutionMode.ENDPOINT_ONLY) {
            localVoiceRequested = false; status = getString(R.string.hybrid_failed); return
        }
        busy = true
        work = activityScope.launch(start = CoroutineStart.LAZY) {
            var runAfter = false
            var finalRecognized = false
            try {
                val localVoice = localVoiceRequested
                if (localVoice && !HybridRuntime.localVoiceReady(this@HybridActivity)) {
                    status = getString(R.string.keyboard_local_voice_unavailable)
                    return@launch
                }
                if (StoredConnectionSelection(this@HybridActivity).revision() != selectionRevision) {
                    throw CancellationException("Connection changed")
                }
                val requestedProfile = if (localVoice) ConnectionProfile.SystemOnDevice() else draftProfile
                val controller = DictationController(requestedProfile, context = this@HybridActivity,
                    requireOnDevice = localVoice,
                    policyOverride = if (localVoice) ExecutionPolicy(ExecutionMode.DEVICE_ONLY) else null)
                val options = DictationSegmentOptions(language = java.util.Locale.getDefault().toLanguageTag())
                val session = controller.openSession(options); voice = session
                status = controller.executionLocation?.name.orEmpty()
                val events = launch {
                    session.events.collect { event -> when (event) {
                        is TranscriptEvent.SegmentReady -> recording = true
                        is TranscriptEvent.Draft -> updateInput(event.text)
                        is TranscriptEvent.Final -> { updateInput(event.text); finalRecognized = event.text.isNotBlank() }
                        is TranscriptEvent.Failure -> status = event.code
                        is TranscriptEvent.SegmentDone -> { recording = false; capture?.cancel(); session.close() }
                        else -> Unit
                    } }
                }
                session.startSegment(options)
                if (!session.capturesOwnAudio) capture = launch { MicAudioCapture().frames().collect { session.sendAudio(it) } }
                events.join()
                runAfter = localVoiceRequested && finalRecognized
                localVoiceRequested = false
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { status = getString(R.string.hybrid_failed) }
            finally {
                withContext(NonCancellable) { capture?.cancelAndJoin(); voice?.close() }
                localVoiceRequested = false
                voice = null; recording = false; busy = false
            }
            if (runAfter) runAgent(true, NoteAccess.READ_ONLY)
        }
        work?.let { job ->
            HybridRuntime.track(this, draftProfile, job)
            job.invokeOnCompletion { if (work === job) activityScope.launch { busy = false; recording = false } }
            job.start()
        }
    }
    private fun stopRecording() {
        val session = voice ?: return
        val recordingJob = work ?: return
        recording = false
        activityScope.launch(recordingJob) {
            try {
                capture?.cancelAndJoin()
                session.finishSegment()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                status = getString(R.string.hybrid_failed)
                session.close()
            }
        }
    }

    private fun cancelWork() {
        pendingMicrophone = null
        pendingAudioPicker = null
        pendingAudioPermission = null
        localVoiceRequested = false
        io.kombify.speechkit.app.voice.NeuralVoiceEngine.get(this).stop()
        capture?.cancel()
        work?.cancel()
        recording = false
    }
    override fun onResume() {
        super.onResume()
        if (!::draftProfile.isInitialized || isFinishing) return
        if (StoredConnectionSelection(this).revision() != selectionRevision) { input = ""; output = ""; finish(); return }
        if (runCapturedLocalVoice) { runCapturedLocalVoice = false; runAgent(true, NoteAccess.READ_ONLY) }
        val server = draftProfile as? ConnectionProfile.Server ?: return
        if (!StoredServerProfile.withActiveProfile(this, server) {}) { input = ""; output = ""; finish() }
    }
    override fun onStop() {
        // Picker and permission results are valid only for this activity instance.
        val microphoneRequest = pendingMicrophone
        val pickerRequest = pendingAudioPicker
        val audioRequest = pendingAudioPermission
        cancelWork()
        pendingMicrophone = microphoneRequest
        pendingAudioPicker = pickerRequest
        pendingAudioPermission = audioRequest
        super.onStop()
    }
    override fun onDestroy() { policyListener?.close(); policyListener = null; cancelWork(); activityScope.cancel(); super.onDestroy() }

    companion object {
        const val EXTRA_TEXT = "io.kombify.speechkit.hybrid.TEXT"
        private const val EXTRA_LOCAL_VOICE_REVISION = "io.kombify.speechkit.hybrid.LOCAL_VOICE_REVISION"

        fun openLocalVoice(context: Context, expectedRevision: Long) {
            if (!BuildConfig.HYBRID_ENABLED || StoredConnectionSelection(context).revision() != expectedRevision) return
            context.startActivity(localVoiceIntent(context, expectedRevision))
        }

        fun openLocalVoiceText(context: Context, text: String, expectedRevision: Long) {
            if (!BuildConfig.HYBRID_ENABLED || text.isBlank() || text.length > 12000 ||
                StoredConnectionSelection(context).revision() != expectedRevision) return
            context.startActivity(localVoiceIntent(context, expectedRevision).putExtra(EXTRA_TEXT, text))
        }

        private fun localVoiceIntent(context: Context, expectedRevision: Long): Intent =
            Intent(context, HybridActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                .putExtra(EXTRA_LOCAL_VOICE_REVISION, expectedRevision)

        fun open(context: Context, text: String = "") {
            val intent = Intent(context, HybridActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            if (text.isNotBlank()) intent.putExtra(EXTRA_TEXT, text)
            context.startActivity(intent)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun <T> HybridDropdown(label: String, selected: T, options: List<Pair<T, String>>, enabled: Boolean, onSelect: (T) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    Column(modifier = Modifier.fillMaxWidth()) {
        Text(label, style = MaterialTheme.typography.bodySmall)
        Box {
            OutlinedButton(enabled = enabled, onClick = { expanded = true }, modifier = Modifier.fillMaxWidth()) {
                Text(options.firstOrNull { it.first == selected }?.second.orEmpty(), modifier = Modifier.weight(1f))
                ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded)
            }
            DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }, modifier = Modifier.heightIn(max = 320.dp)) {
                options.forEach { (value, title) ->
                    DropdownMenuItem(text = { Text(title) }, onClick = { expanded = false; onSelect(value) })
                }
            }
        }
    }
}
