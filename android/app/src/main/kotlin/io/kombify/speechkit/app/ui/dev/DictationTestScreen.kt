package io.kombify.speechkit.app.ui.dev

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
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
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import io.kombify.speechkit.net.ConnectionProfile
import io.kombify.speechkit.net.DictationController
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import timber.log.Timber

private const val SAMPLE_RATE_HZ = 16_000
private const val CHUNK_BYTES = 3_200 // 100 ms @ 16 kHz S16 mono

/**
 * B-M1 dev screen: exercises the full server dictation path — session mint →
 * streaming WS (or batch fallback) → live drafts → committed finals — against
 * a configurable speechkit-server. The Voice IME (B-M2) consumes the same
 * [DictationController]; this screen is the manual verification surface.
 */
@Composable
fun DictationTestScreen() {
    val context = LocalContext.current
    // Same store StoredServerProfile reads: on the kombify flavor a URL saved
    // here upgrades the IME from the system tier to this server (B-M2b).
    val prefs = remember {
        context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE)
    }
    val scope = rememberCoroutineScope()

    var serverUrl by remember {
        mutableStateOf(prefs.getString(StoredServerProfile.KEY_SERVER_URL, "") ?: "")
    }
    var token by remember {
        mutableStateOf(prefs.getString(StoredServerProfile.KEY_SERVER_TOKEN, "") ?: "")
    }
    var status by remember { mutableStateOf("Nicht verbunden") }
    var draft by remember { mutableStateOf("") }
    var recording by remember { mutableStateOf(false) }
    var session by remember { mutableStateOf<StreamingSttSession?>(null) }
    var recordJob by remember { mutableStateOf<Job?>(null) }
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
                prefs.edit()
                    .putString(StoredServerProfile.KEY_SERVER_URL, serverUrl.trim())
                    .putString(StoredServerProfile.KEY_SERVER_TOKEN, token.trim())
                    .apply()
                status = "Verbinde…"
                val controller = DictationController(
                    ConnectionProfile.Server(serverUrl.trim(), token.trim().ifEmpty { null }),
                )
                val open = controller.openSession()
                session = open
                status = "Verbunden"
                appendLog("Session offen (${open.javaClass.simpleName})")
                launch {
                    open.events.collect { event ->
                        when (event) {
                            is TranscriptEvent.SegmentReady ->
                                appendLog("Segment ${event.segmentId} bereit")
                            is TranscriptEvent.Draft -> draft = event.text
                            is TranscriptEvent.Final -> {
                                draft = ""
                                finals.add(event.text)
                                appendLog("Final: ${event.text}")
                            }
                            is TranscriptEvent.SegmentDone ->
                                appendLog("Segment ${event.segmentId} abgeschlossen")
                            is TranscriptEvent.Failure ->
                                appendLog("Fehler ${event.code}: ${event.message}")
                            is TranscriptEvent.Closed -> {
                                status = "Getrennt (${event.reason ?: "unbekannt"})"
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
                status = "Fehler: ${error.message}"
                appendLog("Verbindung fehlgeschlagen: ${error.message}")
                Timber.w(error, "dictation test connect failed")
            }
        }
    }

    fun micPermissionGranted(): Boolean = ContextCompat.checkSelfPermission(
        context, Manifest.permission.RECORD_AUDIO,
    ) == PackageManager.PERMISSION_GRANTED

    // Permission is verified by the caller (micPermissionGranted) before the
    // segment starts; lint cannot see through the boolean.
    @SuppressLint("MissingPermission")
    fun startRecording(open: StreamingSttSession) {
        recordJob = scope.launch(Dispatchers.IO) {
            val outcome = runCatching {
                val minBuffer = AudioRecord.getMinBufferSize(
                    SAMPLE_RATE_HZ, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
                )
                val recorder = AudioRecord(
                    MediaRecorder.AudioSource.VOICE_RECOGNITION,
                    SAMPLE_RATE_HZ,
                    AudioFormat.CHANNEL_IN_MONO,
                    AudioFormat.ENCODING_PCM_16BIT,
                    maxOf(minBuffer, CHUNK_BYTES),
                )
                try {
                    recorder.startRecording()
                    val chunk = ByteArray(CHUNK_BYTES)
                    while (isActive) {
                        val read = recorder.read(chunk, 0, chunk.size)
                        when {
                            read > 0 -> open.sendAudio(chunk.copyOf(read))
                            read < 0 -> error("AudioRecord.read failed: $read")
                        }
                    }
                } finally {
                    runCatching { recorder.stop() }
                    recorder.release()
                }
            }
            outcome.onFailure { error ->
                appendLog("Aufnahmefehler: ${error.message}")
                recording = false
                Timber.w(error, "audio capture failed")
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
            "Streaming-Diktat (Dev)",
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )
        Text(
            "Testet Session-Mint, Streaming-WebSocket und Batch-Fallback gegen einen speechkit-server.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text("Server-URL") },
            placeholder = { Text("http://192.168.1.10:8080") },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
        )
        OutlinedTextField(
            value = token,
            onValueChange = { token = it },
            label = { Text("Bearer-Token (optional)") },
            modifier = Modifier.fillMaxWidth(),
            singleLine = true,
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(
                onClick = { if (status != "Verbinde…") connect() },
                enabled = session == null && serverUrl.isNotBlank() && status != "Verbinde…",
            ) { Text("Verbinden") }
            OutlinedButton(
                onClick = {
                    val open = session ?: return@OutlinedButton
                    recordJob?.cancel()
                    recording = false
                    scope.launch { runCatching { open.close() } }
                },
                enabled = session != null,
            ) { Text("Trennen") }
        }

        Text(status, style = MaterialTheme.typography.labelLarge)

        Button(
            onClick = {
                val open = session ?: return@Button
                if (!recording) {
                    if (!micPermissionGranted()) {
                        appendLog("Mikrofon-Berechtigung fehlt")
                        return@Button
                    }
                    recording = true
                    // Sequence start → record in ONE coroutine: two racing
                    // launches could enqueue PCM before the start frame.
                    scope.launch {
                        open.startSegment(
                            DictationSegmentOptions(
                                language = "de",
                                promptHint = "Kurzes Test-Diktat aus der SpeechKit Android-App.",
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
