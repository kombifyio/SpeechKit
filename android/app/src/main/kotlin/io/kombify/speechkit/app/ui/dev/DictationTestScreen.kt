package io.kombify.speechkit.app.ui.dev

import android.Manifest
import android.content.pm.PackageManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import io.kombify.speechkit.R
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.DictationController
import io.kombify.speechkit.domain.serverDisplayToken
import io.kombify.speechkit.domain.serverDisplayUrl
import io.kombify.speechkit.domain.testSurfaceConnectProfile
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * B-M1 dev screen: exercises the dictation path the keyboard already uses —
 * [ConnectionProfileSource] → [DictationController] → live drafts → finals.
 * Settings owns persistence; this screen must not write `speechkit_config` or
 * a Connect with empty fields turns tester origin / Cloud into a self-host.
 */
@Composable
fun DictationTestScreen(profileSource: ConnectionProfileSource) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val lifecycleOwner = androidx.compose.ui.platform.LocalLifecycleOwner.current

    var resolved by remember { mutableStateOf(profileSource.currentProfile()) }
    var serverUrl by remember { mutableStateOf(resolved.serverDisplayUrl()) }
    var token by remember { mutableStateOf(resolved.serverDisplayToken()) }
    DisposableEffect(lifecycleOwner) {
        val observer = androidx.lifecycle.LifecycleEventObserver { _, event ->
            if (event == androidx.lifecycle.Lifecycle.Event.ON_RESUME) {
                resolved = profileSource.currentProfile()
                if (serverUrl.isBlank()) serverUrl = resolved.serverDisplayUrl()
                if (token.isBlank()) token = resolved.serverDisplayToken()
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }
    var status by remember { mutableStateOf(context.getString(R.string.dev_status_disconnected)) }
    var draft by remember { mutableStateOf("") }
    var recording by remember { mutableStateOf(false) }
    var session by remember { mutableStateOf<StreamingSttSession?>(null) }
    var recordJob by remember { mutableStateOf<Job?>(null) }
    val capture = remember { MicAudioCapture() }
    val finals = remember { mutableStateListOf<String>() }
    val log = remember { mutableStateListOf<String>() }

    fun appendLog(line: String) {
        log.add(0, line)
        while (log.size > 40) log.removeAt(log.size - 1)
    }

    DisposableEffect(Unit) {
        onDispose {
            recordJob?.cancel()
            session?.let { open ->
                CoroutineScope(Dispatchers.IO).launch { runCatching { open.close() } }
            }
        }
    }

    fun connect() {
        scope.launch {
            runCatching {
                val profile = testSurfaceConnectProfile(
                    profileSource.currentProfile(),
                    serverUrl,
                    token,
                )
                status = context.getString(R.string.dev_status_connecting)
                val controller = DictationController(profile, context = context)
                val open = controller.openSession()
                session = open
                status = when (profile) {
                    is ConnectionProfile.Server ->
                        context.getString(R.string.dev_connected_url, profile.normalizedBaseUrl)
                    is ConnectionProfile.SystemOnDevice ->
                        context.getString(R.string.dev_connected_on_device)
                    else -> context.getString(R.string.dev_status_connected)
                }
                appendLog(context.getString(R.string.dev_session_open, open.javaClass.simpleName))
                launch {
                    open.events.collect { event ->
                        when (event) {
                            is TranscriptEvent.SegmentReady ->
                                appendLog(context.getString(R.string.dev_segment_ready, event.segmentId))
                            is TranscriptEvent.Draft -> draft = event.text
                            is TranscriptEvent.Final -> {
                                draft = ""
                                finals.add(event.text)
                                appendLog("Final: ${event.text}")
                            }
                            is TranscriptEvent.SegmentDone ->
                                appendLog(context.getString(R.string.dev_segment_done, event.segmentId))
                            is TranscriptEvent.Failure ->
                                appendLog(context.getString(R.string.dev_status_error, "${event.code}: ${event.message}"))
                            is TranscriptEvent.Closed -> {
                                status = context.getString(
                                    R.string.dev_disconnected_reason,
                                    event.reason ?: context.getString(R.string.dev_unknown),
                                )
                                session = null
                                recording = false
                                // The mic loop must die with the session, or
                                // it records forever with no UI to stop it.
                                recordJob?.cancel()
                                recordJob = null
                            }
                        }
                    }
                }
            }.onFailure { error ->
                status = context.getString(R.string.dev_status_error, error.message ?: "")
                appendLog(context.getString(R.string.dev_connect_failed, error.message ?: ""))
                VoiceLog.w(VoiceLog.DICTATION, "test connect failed", error)
            }
        }
    }

    fun micPermissionGranted(): Boolean = ContextCompat.checkSelfPermission(
        context, Manifest.permission.RECORD_AUDIO,
    ) == PackageManager.PERMISSION_GRANTED

    fun startRecording(open: StreamingSttSession) {
        if (open.capturesOwnAudio) {
            appendLog("System recognizer owns the microphone")
            return
        }
        recordJob = scope.launch {
            val outcome = runCatching {
                capture.frames().collect { frame -> open.sendAudio(frame) }
            }
            outcome.onFailure { error ->
                appendLog(context.getString(R.string.dev_capture_failed, error.message ?: ""))
                recording = false
                VoiceLog.w(VoiceLog.AUDIO, "test capture failed", error)
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            stringResource(R.string.dev_dictation_title),
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )
        Text(
            "Uses the same connection as the keyboard. Leave the fields empty for tester origin, Cloud, or on-device. A typed URL is a one-shot override and is not saved.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text(stringResource(R.string.dev_server_url)) },
            placeholder = { Text("http://192.168.1.10:8080") },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
        )
        OutlinedTextField(
            value = token,
            onValueChange = { token = it },
            label = { Text(stringResource(R.string.dev_bearer_optional)) },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(
                onClick = { if (status != context.getString(R.string.dev_status_connecting)) connect() },
                enabled = session == null && status != context.getString(R.string.dev_status_connecting),
            ) { Text(stringResource(R.string.dev_connect)) }
            OutlinedButton(
                onClick = {
                    val open = session ?: return@OutlinedButton
                    recordJob?.cancel()
                    recording = false
                    scope.launch { runCatching { open.close() } }
                },
                enabled = session != null,
            ) { Text(stringResource(R.string.dev_disconnect)) }
        }

        Text(status, style = MaterialTheme.typography.labelLarge)

        Button(
            onClick = {
                val open = session ?: return@Button
                if (!recording) {
                    if (!micPermissionGranted()) {
                        appendLog(context.getString(R.string.dev_status_mic_missing))
                        return@Button
                    }
                    recording = true
                    // Sequence start → record in ONE coroutine: two racing
                    // launches could enqueue PCM before the start frame.
                    scope.launch {
                        open.startSegment(
                            DictationSegmentOptions(
                                promptHint = "Short dictation test from the SpeechKit Android app.",
                            ),
                        )
                        startRecording(open)
                    }
                } else {
                    recording = false
                    recordJob?.cancel()
                    recordJob = null
                    scope.launch { open.finishSegment() }
                }
            },
            enabled = session != null,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (recording) "⏹ Aufnahme beenden" else "🎤 Aufnahme starten")
        }

        if (draft.isNotEmpty()) {
            Card(modifier = Modifier.fillMaxWidth()) {
                Text(
                    draft,
                    style = MaterialTheme.typography.bodyLarge,
                    fontStyle = FontStyle.Italic,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(12.dp),
                )
            }
        }

        if (finals.isNotEmpty()) {
            Text("Transkripte", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Medium)
            finals.asReversed().forEach { text ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Text(text, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(12.dp))
                }
            }
        }

        Spacer(Modifier.height(4.dp))
        Text("Protokoll", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
        log.forEach { line ->
            Text(
                line,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
