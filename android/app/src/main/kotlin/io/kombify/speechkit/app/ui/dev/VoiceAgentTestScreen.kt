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
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.R
import androidx.core.content.ContextCompat
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.audio.PcmStreamPlayer
import io.kombify.speechkit.audio.PcmPlaybackException
import io.kombify.speechkit.audio.PcmPlaybackQueue
import io.kombify.speechkit.ime.ui.toAuraState
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.VoiceAgentAudio
import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.net.VoiceAgentSetupException
import io.kombify.speechkit.net.safeVoiceAgentCode
import io.kombify.speechkit.domain.serverDisplayToken
import io.kombify.speechkit.domain.serverDisplayUrl
import io.kombify.speechkit.domain.testSurfaceConnectProfile
import io.kombify.speechkit.voiceui.VoiceAuraOrb
import kotlinx.coroutines.Job
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.withContext
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
    val resources = LocalResources.current
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
    var status by remember { mutableStateOf(resources.getString(R.string.dev_status_disconnected)) }
    var holding by remember { mutableStateOf(false) }
    var recordJob by remember { mutableStateOf<Job?>(null) }
    var connectJob by remember { mutableStateOf<Job?>(null) }
    val capture = remember { MicAudioCapture() }
    val player = remember { PcmStreamPlayer(VoiceAgentAudio.SERVER_SAMPLE_RATE) }

    val state = controller?.state?.collectAsState()?.value ?: VoiceAgentUiState()

    DisposableEffect(Unit) {
        onDispose {
            recordJob?.cancel()
            connectJob?.cancel()
            player.release()
        }
    }

    fun startCapture(live: VoiceAgentController) {
        recordJob = scope.launch {
            runCatching {
                capture.frames().collect { live.sendAudio(it) }
            }.onFailure {
                if (it is CancellationException) throw it
                VoiceLog.w(VoiceLog.AUDIO, "test voice agent capture failed")
                status = resources.getString(R.string.dev_status_error, "capture_failed")
                connectJob?.cancel()
            }
        }
    }

    fun connect() {
        if (connectJob?.isActive == true || controller != null) return
        val connecting = scope.launch(start = CoroutineStart.LAZY) {
            val owner = coroutineContext[Job]
            var live: VoiceAgentController? = null
            val playback = PcmPlaybackQueue(VoiceAgentAudio.SERVER_SAMPLE_RATE)
            try {
                val profile = testSurfaceConnectProfile(
                    profileSource.currentProfile(),
                    serverUrl,
                    token,
                )
                if (profile !is ConnectionProfile.Server) {
                    status = "Voice Agent needs a SpeechKit server (tester origin, Cloud, or self-host)."
                    return@launch
                }
                status = resources.getString(R.string.dev_status_connecting)
                val session = VoiceAgentController(profile)
                live = session
                controller = session
                val events = session.start(VoiceAgentStartFrame())
                status = resources.getString(R.string.dev_status_connected)
                coroutineScope {
                    val playing = launch { playback.consume { player.play(it) } }
                    events.collect { event ->
                        session.accept(event)
                        when (event) {
                            is VoiceAgentEvent.Audio -> if (!playback.offer(event.pcm))
                                throw PcmPlaybackException("voice_buffer_overflow")
                            VoiceAgentEvent.Interrupted -> { playback.clear(); player.flush() }
                            is VoiceAgentEvent.Closed -> { playback.clear(); player.release() }
                            is VoiceAgentEvent.Failure -> if (event.fatal) { playback.clear(); player.release() }
                            else -> Unit
                        }
                    }
                    playback.close()
                    playing.join()
                }
                status = resources.getString(R.string.dev_status_ended)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: PcmPlaybackException) {
                status = resources.getString(R.string.dev_status_error, safeVoiceAgentCode(failure.code, "playback_failed"))
            } catch (failure: VoiceAgentSetupException) {
                status = resources.getString(R.string.dev_status_error, safeVoiceAgentCode(failure.code, "ws_setup_failed"))
            } catch (_: Throwable) {
                status = resources.getString(R.string.dev_status_error, "voice_session_failed")
                VoiceLog.w(VoiceLog.AGENT, "test connect failed")
            } finally {
                playback.close()
                recordJob?.cancel()
                holding = false
                player.release()
                withContext(NonCancellable) { runCatching { live?.stop() } }
                if (controller === live) controller = null
                if (connectJob === owner) connectJob = null
            }
        }
        connectJob = connecting
        connecting.start()
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.dev_va_title), style = MaterialTheme.typography.titleMedium)
        Text(
            "Hold to talk, release for the answer. Uses the same connection as the keyboard. Empty fields keep tester origin or Cloud; a typed URL is a one-shot override and is not saved.",
            style = MaterialTheme.typography.bodySmall,
        )

        OutlinedTextField(
            value = serverUrl,
            onValueChange = { serverUrl = it },
            label = { Text(stringResource(R.string.dev_server_url)) },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = token,
            onValueChange = { token = it },
            label = { Text(stringResource(R.string.dev_token_optional)) },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = ::connect, enabled = controller == null && connectJob?.isActive != true) { Text(stringResource(R.string.dev_connect)) }
            Button(
                onClick = {
                    recordJob?.cancel()
                    connectJob?.cancel()
                    player.release()
                    holding = false
                    status = resources.getString(R.string.dev_status_ended)
                },
                enabled = controller != null,
            ) { Text(stringResource(R.string.dev_end)) }
        }

        val live = controller
        if (live != null) {
            Button(
                onClick = {
                    if (!hasMicPermission(context)) {
                        status = resources.getString(R.string.dev_status_mic_missing)
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
                enabled = state.phase != VoiceAgentUiState.Phase.Connecting && state.phase != VoiceAgentUiState.Phase.Ended,
            ) { Text(stringResource(if (holding) R.string.dev_release_to_answer else R.string.dev_hold_to_talk)) }
        }

        Card(modifier = Modifier.fillMaxWidth()) {
            Column(
                Modifier.padding(12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                VoiceAuraOrb(state = state.phase.toAuraState(), sizeDp = 96)
                Text(stringResource(R.string.dev_status_label, status), style = MaterialTheme.typography.bodySmall)
                Text(stringResource(R.string.dev_phase, state.phase.name), style = MaterialTheme.typography.bodySmall)
                state.error?.let {
                    Text(stringResource(R.string.dev_status_error, it), style = MaterialTheme.typography.bodySmall)
                }
                if (state.userText.isNotBlank()) {
                    Text("${stringResource(R.string.dev_you)}: ${state.userText}", style = MaterialTheme.typography.bodyMedium)
                }
                if (state.agentText.isNotBlank()) {
                    Text("${stringResource(R.string.dev_agent)}: ${state.agentText}", style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
    }
}

private fun hasMicPermission(context: Context): Boolean =
    ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) ==
        PackageManager.PERMISSION_GRANTED
