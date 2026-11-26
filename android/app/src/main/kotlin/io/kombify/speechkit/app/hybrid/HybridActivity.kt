package io.kombify.speechkit.app.hybrid

import android.Manifest
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.R
import io.kombify.speechkit.app.ui.theme.SpeechKitTheme
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.domain.*
import io.kombify.speechkit.hybrid.*
import io.kombify.speechkit.net.*
import io.kombify.speechkit.store.*
import io.kombify.speechkit.stt.streaming.*
import kotlinx.coroutines.*
import java.util.UUID
import javax.inject.Inject

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
    private var runCapturedLocalVoice = false
    private var agentConnected by mutableStateOf(false)
    private var agentRecording by mutableStateOf(false)
    private var pendingAgentMic = false
    private var selectionRevision = 0L
    private var selectedAgent: VoiceAgentSessionDriver? = null
    private var harnesses by mutableStateOf<List<VoiceHarness>>(emptyList())
    private var work: Job? = null
    private var capture: Job? = null
    private var policyListener: java.io.Closeable? = null
    private var voice: StreamingSttSession? = null
    private val activityScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val picker = registerForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri != null) launchWork {
            val result = HybridRuntime.transcribe(this, draftProfile, uri)
            updateInput(result.value)
            status = result.location.name
        }
    }
    private val permission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (pendingAgentMic) {
            pendingAgentMic = false
            if (granted) beginAgentTurn()
        } else if (granted) startRecording() else localVoiceRequested = false
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!BuildConfig.HYBRID_ENABLED) { finish(); return }
        draftProfile = profiles.currentProfile()
        selectionRevision = StoredConnectionSelection(this).revision()
        if (intent.hasExtra(EXTRA_LOCAL_VOICE_REVISION)) {
            if (intent.getLongExtra(EXTRA_LOCAL_VOICE_REVISION, -1) != selectionRevision ||
                StoredExecutionPolicy(this).read(draftProfile).mode == ExecutionMode.ENDPOINT_ONLY) { finish(); return }
            runCapturedLocalVoice = savedInstanceState == null
        }
        policyListener = NetworkUseGuard.listen {
            val jobs = listOfNotNull(work, capture)
            cancelWork()
            jobs
        }
        val settings = StoredExecutionPolicy(this)
        val draftKey = HybridRuntime.scope(draftProfile)
        val restored = savedInstanceState?.takeIf { it.getString("draft_scope") == draftKey }?.getString(EXTRA_TEXT)
        input = (restored ?: intent.getStringExtra(EXTRA_TEXT)
            ?: getSharedPreferences("speechkit_hybrid_drafts", Context.MODE_PRIVATE).getString(draftKey, "").orEmpty()).take(16000)
        updateInput(input)
        activityScope.launch {
            if (!settings.isConfigured()) settings.write(settings.read(draftProfile))
            setContent { SpeechKitTheme { Surface(Modifier.fillMaxSize()) { Screen() } } }
        }
    }

    @OptIn(ExperimentalLayoutApi::class)
    @Composable private fun Screen() {
        var policy by remember { mutableStateOf(StoredExecutionPolicy(this@HybridActivity).read(draftProfile)) }
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
        val cloudAllowed = !BuildConfig.CONNECTION_SETUP_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED && !StoredServerProfile.cloudSelected(this@HybridActivity)
        Column(Modifier.fillMaxSize().safeDrawingPadding().imePadding().verticalScroll(rememberScrollState()).padding(20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.hybrid_title), style = MaterialTheme.typography.headlineSmall)
            Text(stringResource(R.string.hybrid_destination))
            ExecutionMode.entries.forEach { mode ->
                Row(verticalAlignment = Alignment.CenterVertically) { RadioButton(enabled = !busy && !changingPolicy, selected = policy.mode == mode, onClick = { changePolicy(policy.copy(mode = mode, allowNetwork = mode != ExecutionMode.DEVICE_ONLY)) }); Text(stringResource(when (mode) { ExecutionMode.DEVICE_ONLY -> R.string.hybrid_device_only; ExecutionMode.PREFER_DEVICE -> R.string.hybrid_prefer_device; ExecutionMode.PREFER_ENDPOINT -> R.string.hybrid_prefer_endpoint; ExecutionMode.ENDPOINT_ONLY -> R.string.connection_setup_cloud_only })) }
            }
            if (!BuildConfig.CONNECTION_SETUP_ENABLED) Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(enabled = !busy && !changingPolicy, checked = policy.allowNetwork, onCheckedChange = { changePolicy(policy.copy(allowNetwork = it)) }); Text(stringResource(R.string.hybrid_allow_network)) }
            if (cloudAllowed) Row(verticalAlignment = Alignment.CenterVertically) { Checkbox(enabled = !busy, checked = firebaseCloud, onCheckedChange = { firebaseCloud = it }); Text(stringResource(R.string.hybrid_firebase_cloud)) }
            if (policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY && StoredServerProfile.cloudSelected(this@HybridActivity) && (draftProfile as? ConnectionProfile.Server)?.normalizedBaseUrl?.endsWith("/v1/speechkit") == true) {
                Button(enabled = !busy, onClick = { launchWork {
                    val server = draftProfile as ConnectionProfile.Server
                    harnesses = SpeechKitServerApi(server, context = this@HybridActivity).discoverVoiceHarnesses("mobile")
                    if (harnesses.isEmpty()) status = getString(R.string.hybrid_no_agents)
                } }) { Text(stringResource(R.string.hybrid_find_agents)) }
                harnesses.forEach { harness ->
                    Button(enabled = !busy, onClick = { launchWork {
                        val server = draftProfile as ConnectionProfile.Server
                        val target = SpeechKitServerApi(server, context = this@HybridActivity).createVoiceAgentChat(harness, "mobile")
                        StoredVoiceAgentTarget(this@HybridActivity).write(server, target)
                        status = getString(R.string.hybrid_agent_selected, harness.id)
                    } }) { Text(harness.id) }
                }
            }
            if (policy.allowNetwork && policy.mode in setOf(ExecutionMode.PREFER_ENDPOINT, ExecutionMode.ENDPOINT_ONLY) &&
                StoredVoiceAgentTarget(this@HybridActivity).read(draftProfile) != null
            ) {
                Button(enabled = !busy, onClick = ::startSelectedAgent) { Text(stringResource(R.string.hybrid_run_selected)) }
            }
            if (agentConnected) {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Button(onClick = { if (agentRecording) endAgentTurn() else beginAgentTurn() }) {
                        Text(stringResource(if (agentRecording) R.string.hybrid_stop else R.string.hybrid_dictate))
                    }
                    Button(enabled = input.isNotBlank(), onClick = {
                        val text = input
                        activityScope.launch { selectedAgent?.sendText(text) }
                    }) { Text(stringResource(R.string.hybrid_send_selected)) }
                }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Button(enabled = !busy, onClick = { launchWork {
                    val model = HybridRuntime.model(this@HybridActivity)
                    val readiness = model.readiness()
                    status = if (readiness == DeviceReadiness.AVAILABLE) "${readiness.name} - ${model.deviceModelName()}" else readiness.name
                } }) { Text(stringResource(R.string.hybrid_check)) }
                Button(enabled = !busy, onClick = { launchWork { HybridRuntime.model(this@HybridActivity).download { status = it.toString() }; status = HybridRuntime.model(this@HybridActivity).readiness().name } }) { Text(stringResource(R.string.hybrid_download)) }
                Button(enabled = !busy, onClick = { launchWork { ImportedAudio(java.util.Locale.getDefault()).use { it.download { progress -> status = progress.toString() } }; status = getString(R.string.hybrid_ready) } }) { Text(stringResource(R.string.hybrid_download_speech)) }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Button(enabled = !busy, onClick = { launchWork {
                    val store = RoomStore(this@HybridActivity)
                    try { val agent = LocalNoteAgent(this@HybridActivity, store); status = if (agent.noteToolsSupported()) agent.readiness().name else "Android 16+" }
                    finally { store.close() }
                } }) { Text(stringResource(R.string.hybrid_agent_check)) }
                Button(enabled = !busy, onClick = { launchWork {
                    val store = RoomStore(this@HybridActivity)
                    try { val agent = LocalNoteAgent(this@HybridActivity, store); check(agent.noteToolsSupported()); agent.download { status = it.toString() } }
                    finally { store.close() }
                } }) { Text(stringResource(R.string.hybrid_agent_download)) }
            }
            OutlinedTextField(value = input, onValueChange = { updateInput(it) }, label = { Text(stringResource(R.string.hybrid_input)) }, modifier = Modifier.fillMaxWidth(), minLines = 4)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Button(enabled = !busy || recording, onClick = { if (recording) stopRecording() else if (ContextCompat.checkSelfPermission(this@HybridActivity, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) startRecording() else permission.launch(Manifest.permission.RECORD_AUDIO) }) { Text(stringResource(if (recording) R.string.hybrid_stop else R.string.hybrid_dictate)) }
                Button(enabled = !busy, onClick = { picker.launch("audio/*") }) { Text(stringResource(R.string.hybrid_audio)) }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Button(enabled = !busy && input.isNotBlank(), onClick = { generate(getString(R.string.hybrid_summary_prompt), firebaseCloud) }) { Text(stringResource(R.string.hybrid_summarize)) }
                Button(enabled = !busy && input.isNotBlank(), onClick = { generate(getString(R.string.hybrid_rewrite_prompt), firebaseCloud) }) { Text(stringResource(R.string.hybrid_rewrite)) }
                Button(enabled = !busy && input.isNotBlank() && policy.mode != ExecutionMode.ENDPOINT_ONLY, onClick = { runAgent(false, NoteAccess.READ_ONLY) }) { Text(stringResource(R.string.hybrid_note_agent)) }
            }
            Button(enabled = !busy && input.isNotBlank() && policy.mode != ExecutionMode.ENDPOINT_ONLY, onClick = { runAgent(false, NoteAccess.SAVE_ONCE) }) { Text(stringResource(R.string.hybrid_save_note)) }
            Button(enabled = !busy && policy.mode != ExecutionMode.ENDPOINT_ONLY, onClick = {
                localVoiceRequested = true
                if (ContextCompat.checkSelfPermission(this@HybridActivity, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) startRecording()
                else permission.launch(Manifest.permission.RECORD_AUDIO)
            }) { Text(stringResource(R.string.hybrid_local_voice)) }
            Button(enabled = !busy && input.isNotBlank(), onClick = { generate("", firebaseCloud) }) { Text(stringResource(R.string.hybrid_ask)) }
            if (busy) Button(onClick = ::cancelWork) { Text(stringResource(R.string.hybrid_cancel)) }
            if (status.isNotBlank()) Text(status)
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
            Button(enabled = !busy && draftProfile is ConnectionProfile.Server, onClick = { launchWork {
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
            if (output.isNotBlank()) {
                Text(output, style = MaterialTheme.typography.bodyLarge)
                Button(onClick = { (getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("SpeechKit", output)) }) { Text(stringResource(R.string.hybrid_copy)) }
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
            pendingAgentMic = true
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
        recording = true
        work = activityScope.launch(start = CoroutineStart.LAZY) {
            var runAfter = false
            var finalRecognized = false
            try {
                val requestedProfile = if (localVoiceRequested) ConnectionProfile.SystemOnDevice() else draftProfile
                val controller = DictationController(requestedProfile, context = this@HybridActivity, requireOnDevice = localVoiceRequested)
                val options = DictationSegmentOptions(language = java.util.Locale.getDefault().toLanguageTag())
                val session = controller.openSession(options); voice = session
                status = controller.executionLocation?.name.orEmpty()
                val events = launch {
                    session.events.collect { event -> when (event) {
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
    private fun stopRecording() { capture?.cancel(); activityScope.launch { voice?.finishSegment() } }
    private fun cancelWork() { localVoiceRequested = false; io.kombify.speechkit.app.voice.NeuralVoiceEngine.get(this).stop(); capture?.cancel(); work?.cancel(); recording = false }
    override fun onResume() {
        super.onResume()
        if (!::draftProfile.isInitialized || isFinishing) return
        if (StoredConnectionSelection(this).revision() != selectionRevision) { input = ""; output = ""; finish(); return }
        if (runCapturedLocalVoice) { runCapturedLocalVoice = false; runAgent(true, NoteAccess.READ_ONLY) }
        val server = draftProfile as? ConnectionProfile.Server ?: return
        if (!StoredServerProfile.withActiveProfile(this, server) {}) { input = ""; output = ""; finish() }
    }
    override fun onStop() { cancelWork(); super.onStop() }
    override fun onDestroy() { policyListener?.close(); policyListener = null; cancelWork(); activityScope.cancel(); super.onDestroy() }

    companion object {
        const val EXTRA_TEXT = "io.kombify.speechkit.hybrid.TEXT"
        private const val EXTRA_LOCAL_VOICE_REVISION = "io.kombify.speechkit.hybrid.LOCAL_VOICE_REVISION"
        fun openLocalVoiceText(context: Context, text: String, expectedRevision: Long) {
            if (text.isBlank() || text.length > 12000 || StoredConnectionSelection(context).revision() != expectedRevision) return
            context.startActivity(Intent(context, HybridActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                .putExtra(EXTRA_TEXT, text).putExtra(EXTRA_LOCAL_VOICE_REVISION, expectedRevision))
        }
        fun open(context: Context, text: String = "") {
            val intent = Intent(context, HybridActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            if (text.isNotBlank()) intent.putExtra(EXTRA_TEXT, text)
            context.startActivity(intent)
        }
    }
}
