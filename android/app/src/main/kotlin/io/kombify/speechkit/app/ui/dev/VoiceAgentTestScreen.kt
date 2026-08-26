package io.kombify.speechkit.app.ui.dev

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
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
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.audio.PcmStreamPlayer
import io.kombify.speechkit.ime.ui.toAuraState
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.VoiceAgentAudio
import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.domain.serverDisplayToken
import io.kombify.speechkit.domain.serverDisplayUrl
import io.kombify.speechkit.domain.testSurfaceConnectProfile
import io.kombify.speechkit.voiceui.VoiceAuraOrb
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Developer surface for the realtime Voice Agent: hold to talk, release to
 * let the agent answer.
 *
 * This is a test harness, not the product surface. The shipping surfaces are
 * the system assistant overlay and the keyboard panel; both bind to the same
 * [VoiceAgentController] through [ConnectionProfileSource], so what works here
 * is what works there. Settings owns persistence — Connect here must not write
 * `speechkit_config`. Capture, playback, and the orb adapter are the same
 * `:core` / `:ime` pieces those surfaces use.
 */
@Composable
fun VoiceAgentTestScreen(
    profileSource: ConnectionProfileSource,
    modifier: Modifier = Modifier,
) {
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
    var controller by remember { mutableStateOf<VoiceAgentController?>(null) }
    var status by remember { mutableStateOf("Nicht verbunden") }
    var holding by remember { mutableStateOf(false) }
    var recordJob by remember { mutableStateOf<Job?>(null) }
    val capture = remember { MicAudioCapture() }
    val player = remember { PcmStreamPlayer(VoiceAgentAudio.SERVER_SAMPLE_RATE) }

    val state = controller?.state?.collectAsState()?.value ?: VoiceAgentUiState()

    DisposableEffect(Unit) {
        onDispose {
            recordJob?.cancel()
            player.release()
            controller?.let { live -> scope.launch { runCatching { live.stop() } } }
        }
    }

    fun startCapture(live: VoiceAgentController) {
        recordJob = scope.launch {
            runCatching {
                capture.frames().collect { live.sendAudio(it) }
            }.onFailure { VoiceLog.w(VoiceLog.AUDIO, "test voice agent capture failed", it) }
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
                if (profile !is ConnectionProfile.Server) {
                    status = "Voice Agent needs a SpeechKit server (tester origin, Cloud, or self-host)."
                    return@launch
                }
                status = "Verbinde…"
                val live = VoiceAgentController(profile)
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
                VoiceLog.w(VoiceLog.AGENT, "test connect failed", it)
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
            "Hold to talk, release for the answer. Uses the same connection as the keyboard. Empty fields keep tester origin or Cloud; a typed URL is a one-shot override and is not saved.",
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
                VoiceAuraOrb(state = state.phase.toAuraState(), sizeDp = 96)
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

private fun hasMicPermission(context: Context): Boolean =
    ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) ==
        PackageManager.PERMISSION_GRANTED
