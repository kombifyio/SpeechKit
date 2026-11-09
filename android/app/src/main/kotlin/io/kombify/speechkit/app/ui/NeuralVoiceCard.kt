package io.kombify.speechkit.app.ui

import android.content.Context
import android.net.ConnectivityManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.R
import io.kombify.speechkit.app.voice.NeuralVoice
import io.kombify.speechkit.app.voice.NeuralVoiceDownloads
import io.kombify.speechkit.app.voice.NeuralVoiceEngine
import io.kombify.speechkit.app.voice.NeuralVoiceStore
import io.kombify.speechkit.app.voice.NeuralVoices
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Optional natural voices for the assistant's replies, spoken on the device without a network. The platform voice
 * stays the default and speaks whenever no voice is downloaded for the reply's language. Each voice is a download
 * of about 21 MB that resumes where it stopped ([NeuralVoiceDownloads]); an installed voice can be played as a
 * sample or deleted.
 */
@Composable
internal fun NeuralVoiceCard() {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val store = remember { NeuralVoiceStore.get(context) }
    val engine = remember { NeuralVoiceEngine.get(context) }
    val download by NeuralVoiceDownloads.state.collectAsState()
    // Bumped after a download finishes or a voice is deleted, so the installed state is read again.
    var storeTick by remember { mutableIntStateOf(0) }
    // The voice id whose sample is playing, and the one whose sample could not play.
    var playing by remember { mutableStateOf<String?>(null) }
    var sampleFailed by remember { mutableStateOf<String?>(null) }
    var playback by remember { mutableStateOf<Job?>(null) }

    LaunchedEffect(download) {
        if (download is NeuralVoiceDownloads.State.Done) storeTick += 1
    }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                stringResource(R.string.settings_voice_title),
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                stringResource(R.string.settings_voice_subtitle),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (!NeuralVoiceEngine.supported()) {
                Text(
                    stringResource(R.string.settings_voice_unsupported),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
                return@Column
            }

            for (voice in NeuralVoices.all) {
                VoiceRow(
                    voice = voice,
                    context = context,
                    installed = remember(storeTick, download) { store.isInstalled(voice.model) },
                    partialBytes = remember(storeTick, download) { store.partialBytes(voice.model) },
                    state = download.takeIf { it.modelId == null || it.modelId == voice.model.id }
                        ?: NeuralVoiceDownloads.State.Idle,
                    downloadBusy = download is NeuralVoiceDownloads.State.Running,
                    isPlaying = playing == voice.id,
                    sampleFailed = sampleFailed == voice.id,
                    onPlay = {
                        sampleFailed = null
                        playing = voice.id
                        playback = scope.launch {
                            val spoken = try {
                                engine.speak(voice, voice.sample)
                            } finally {
                                if (playing == voice.id) playing = null
                            }
                            if (!spoken) sampleFailed = voice.id
                        }
                    },
                    onStop = {
                        engine.stop()
                        playback?.cancel()
                        playing = null
                    },
                    onDelete = {
                        scope.launch {
                            NeuralVoiceDownloads.delete(context, voice)
                            storeTick += 1
                        }
                    },
                )
            }
        }
    }
}

@Composable
private fun VoiceRow(
    voice: NeuralVoice,
    context: Context,
    installed: Boolean,
    partialBytes: Long,
    state: NeuralVoiceDownloads.State,
    downloadBusy: Boolean,
    isPlaying: Boolean,
    sampleFailed: Boolean,
    onPlay: () -> Unit,
    onStop: () -> Unit,
    onDelete: () -> Unit,
) {
    val megabytes = voice.downloadBytes / 1_000_000
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        when {
            state is NeuralVoiceDownloads.State.Running -> {
                val fraction = if (state.totalBytes > 0) state.doneBytes.toFloat() / state.totalBytes else 0f
                LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth())
                Text(
                    stringResource(
                        R.string.settings_on_device_downloading,
                        voice.name,
                        state.doneBytes / 1_000_000,
                        state.totalBytes / 1_000_000,
                    ),
                    style = MaterialTheme.typography.bodySmall,
                )
                TextButton(onClick = { NeuralVoiceDownloads.cancel() }) {
                    Text(stringResource(R.string.settings_on_device_pause))
                }
            }
            installed -> {
                Text(
                    stringResource(R.string.settings_voice_ready, voice.name),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.primary,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = if (isPlaying) onStop else onPlay) {
                        Text(stringResource(if (isPlaying) R.string.settings_voice_stop_sample else R.string.settings_voice_play_sample))
                    }
                    TextButton(onClick = onDelete) {
                        Text(stringResource(R.string.settings_on_device_delete, voice.name, megabytes))
                    }
                }
                if (sampleFailed) {
                    Text(
                        stringResource(R.string.settings_voice_sample_failed),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
            else -> {
                if (state is NeuralVoiceDownloads.State.Failed) {
                    Text(
                        stringResource(R.string.settings_on_device_failed, state.message),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
                if (isMetered(context)) {
                    Text(
                        stringResource(R.string.settings_on_device_metered, megabytes),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Button(
                    onClick = { NeuralVoiceDownloads.start(context, voice) },
                    enabled = !downloadBusy,
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.settings_on_device_download, voice.name, megabytes)) }
                if (partialBytes > 0) {
                    TextButton(onClick = onDelete) {
                        Text(stringResource(R.string.settings_on_device_discard_partial, partialBytes / 1_000_000))
                    }
                }
            }
        }
    }
}

private fun isMetered(context: Context): Boolean =
    (context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager)
        ?.isActiveNetworkMetered ?: false
