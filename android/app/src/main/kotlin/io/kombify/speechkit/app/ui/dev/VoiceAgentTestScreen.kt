package io.kombify.speechkit.app.ui.dev

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.Alignment
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import io.kombify.speechkit.net.ConnectionProfile
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.voiceui.VoiceAuraOrb
import io.kombify.speechkit.voiceui.VoiceAuraState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import timber.log.Timber

private const val SAMPLE_RATE_HZ = 16_000
private const val CHUNK_BYTES = 3_200 // 100 ms of 16 kHz S16 mono

/**
 * Developer surface for the realtime Voice Agent: hold to talk, release to
 * let the agent answer.
 *
 * This is a test harness, not the product surface. The shipping surfaces are
 * the system assistant overlay and the keyboard panel; both bind to the same
 * [VoiceAgentController], so what works here is what works there. It exists
 * because a conversation cannot be verified from unit tests alone — someone
 * has to hear the answer come back.
 */
@Composable
fun VoiceAgentTestScreen(modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    // Same store the dictation screen and ConnectionProfileSource use.
    val prefs = remember {
        context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE)
    }

    var serverUrl by remember {
        mutableStateOf(prefs.getString(StoredServerProfile.KEY_SERVER_URL, "").orEmpty())
    }
    var token by remember {
        mutableStateOf(prefs.getString(StoredServerProfile.KEY_SERVER_TOKEN, "").orEmpty())
    }
    var controller by remember { mutableStateOf<VoiceAgentController?>(null) }
    var status by remember { mutableStateOf("Nicht verbunden") }
    var holding by remember { mutableStateOf(false) }
    var recordJob by remember { mutableStateOf<Job?>(null) }
    val player = remember { AgentAudioPlayer() }

    val state = controller?.state?.collectAsState()?.value ?: VoiceAgentUiState()

    DisposableEffect(Unit) {
        onDispose {
            recordJob?.cancel()
            player.release()
            controller?.let { live -> scope.launch { runCatching { live.stop() } } }
        }
    }

    @SuppressLint("MissingPermission")
    fun startCapture(live: VoiceAgentController) {
        recordJob = scope.launch(Dispatchers.IO) {
            runCatching {
                val minBuffer = AudioRecord.getMinBufferSize(
                    SAMPLE_RATE_HZ,
                    AudioFormat.CHANNEL_IN_MONO,
                    AudioFormat.ENCODING_PCM_16BIT,
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
                            read > 0 -> live.sendAudio(chunk.copyOf(read))
                            read < 0 -> error("AudioRecord.read failed: $read")
                        }
                    }
                } finally {
                    runCatching { recorder.stop() }
                    recorder.release()
                }
            }.onFailure { Timber.w(it, "voice agent capture failed") }
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
                val live = VoiceAgentController(
                    ConnectionProfile.Server(serverUrl.trim(), token.trim().ifEmpty { null }),
                )
                val events = live.start(VoiceAgentStartFrame())
                controller = live
                status = "Verbunden"
                launch {
                    events.collect { event ->
                        live.accept(event)
                        // Audio is the one event the controller passes through:
                        // playback is the host's job, not the session's.
                        if (event is VoiceAgentEvent.Audio) player.play(event.pcm)
                    }
                }
            }.onFailure {
                status = "Fehler: ${it.message}"
                Timber.w(it, "voice agent connect failed")
            }
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Voice Agent (Test)", style = MaterialTheme.typography.titleMedium)
        Text(
            "Halten zum Sprechen, loslassen für die Antwort. Braucht einen " +
                "gekoppelten SpeechKit-Server — der Sprachdialog hat keine " +
                "Gerätevariante.",
            style = MaterialTheme.typography.bodySmall,
        )

        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text("Server-URL") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = token,
            onValueChange = { token = it },
            label = { Text("Token (optional)") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = ::connect, enabled = controller == null) { Text("Verbinden") }
            Button(
                onClick = {
                    recordJob?.cancel()
                    val live = controller
                    controller = null
                    holding = false
                    scope.launch { runCatching { live?.stop() } }
                    status = "Beendet"
                },
                enabled = controller != null,
            ) { Text("Beenden") }
        }

        val live = controller
        if (live != null) {
            Button(
                onClick = {
                    if (!hasMicPermission(context)) {
                        status = "Mikrofon-Berechtigung fehlt"
                        return@Button
                    }
                    if (holding) {
                        holding = false
                        recordJob?.cancel()
                        scope.launch { runCatching { live.endTurn() } }
                    } else {
                        holding = true
                        startCapture(live)
                    }
                },
                modifier = Modifier.fillMaxWidth(),
            ) { Text(if (holding) "Loslassen (Antwort abrufen)" else "Halten zum Sprechen") }
        }

        Card(modifier = Modifier.fillMaxWidth()) {
            Column(
                Modifier.padding(12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                VoiceAuraOrb(state = state.phase.orbState(), sizeDp = 96)
                Text("Status: $status", style = MaterialTheme.typography.bodySmall)
                Text("Phase: ${state.phase}", style = MaterialTheme.typography.bodySmall)
                state.error?.let {
                    Text("Fehler: $it", style = MaterialTheme.typography.bodySmall)
                }
                if (state.userText.isNotBlank()) {
                    Text("Du: ${state.userText}", style = MaterialTheme.typography.bodyMedium)
                }
                if (state.agentText.isNotBlank()) {
                    Text("Agent: ${state.agentText}", style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
    }
}

private fun VoiceAgentUiState.Phase.orbState(): VoiceAuraState = when (this) {
    VoiceAgentUiState.Phase.Inactive -> VoiceAuraState.INACTIVE
    VoiceAgentUiState.Phase.Connecting -> VoiceAuraState.CONNECTING
    VoiceAgentUiState.Phase.Listening -> VoiceAuraState.LISTENING
    VoiceAgentUiState.Phase.Processing -> VoiceAuraState.PROCESSING
    VoiceAgentUiState.Phase.Speaking -> VoiceAuraState.SPEAKING
    VoiceAgentUiState.Phase.Ended -> VoiceAuraState.SETTLING
}

private fun hasMicPermission(context: Context): Boolean =
    ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) ==
        PackageManager.PERMISSION_GRANTED

/**
 * Streams the agent's PCM answer to the speaker.
 *
 * Deliberately minimal: the shipping surfaces route playback through the
 * platform audio adapters, and duplicating that here would mean two playback
 * paths to keep honest. This one only has to prove the audio arrives.
 */
private class AgentAudioPlayer {
    private var track: AudioTrack? = null

    fun play(pcm: ByteArray) {
        val active = track ?: create().also { track = it }
        runCatching { active.write(pcm, 0, pcm.size) }
            .onFailure { Timber.w(it, "agent audio playback failed") }
    }

    fun release() {
        runCatching {
            track?.stop()
            track?.release()
        }
        track = null
    }

    private fun create(): AudioTrack {
        val minBuffer = AudioTrack.getMinBufferSize(
            SAMPLE_RATE_HZ,
            AudioFormat.CHANNEL_OUT_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
        )
        return AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANT)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setSampleRate(SAMPLE_RATE_HZ)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(minBuffer, CHUNK_BYTES * 4))
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
            .also { it.play() }
    }
}
