package io.kombify.speechkit.app.ui

import android.content.Context
import android.net.ConnectivityManager
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
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
import io.kombify.speechkit.net.OnDeviceEngine
import io.kombify.speechkit.stt.parakeet.ParakeetDownloads
import io.kombify.speechkit.stt.parakeet.ParakeetModels
import io.kombify.speechkit.stt.parakeet.ParakeetOnDevice
import kotlinx.coroutines.launch

/**
 * Chooses the on-device recognizer and manages the Parakeet download.
 *
 * The system recognizer stays the default; Parakeet becomes selectable once
 * the device can run it, and is used for on-device dictation (keyboard,
 * assistant, Voice IME) only after its model is downloaded. The download runs
 * in the background ([ParakeetDownloads]) and resumes where it stopped.
 */
@Composable
internal fun OnDeviceModelCard() {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val model = ParakeetModels.ULTRA
    val store = remember { ParakeetOnDevice.modelStore(context) }
    val support = remember { ParakeetOnDevice.support(context) }
    val download by ParakeetDownloads.state.collectAsState()
    var choice by remember { mutableStateOf(OnDeviceEngine.choice(context)) }
    // Bumped after a download finishes or the model is deleted, so the
    // installed state is read again.
    var storeTick by remember { mutableIntStateOf(0) }
    val installed = remember(storeTick, download) { store.isInstalled(model) }

    LaunchedEffect(download) {
        if (download is ParakeetDownloads.State.Done) storeTick += 1
    }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                stringResource(R.string.settings_on_device_title),
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                stringResource(R.string.settings_on_device_subtitle),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )

            if (support is ParakeetOnDevice.Support.Unsupported) {
                Text(
                    stringResource(R.string.settings_on_device_unsupported, support.reason),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
                return@Column
            }

            Row(
                modifier = Modifier.horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                FilterChip(
                    selected = choice == OnDeviceEngine.Choice.SYSTEM,
                    onClick = {
                        choice = OnDeviceEngine.Choice.SYSTEM
                        OnDeviceEngine.setChoice(context, choice)
                    },
                    label = { Text(stringResource(R.string.settings_on_device_engine_system)) },
                )
                FilterChip(
                    selected = choice == OnDeviceEngine.Choice.PARAKEET,
                    onClick = {
                        choice = OnDeviceEngine.Choice.PARAKEET
                        OnDeviceEngine.setChoice(context, choice)
                    },
                    label = { Text(stringResource(R.string.settings_on_device_engine_parakeet)) },
                )
            }

            val megabytes = model.sizeBytes / 1_000_000
            when (val state = download) {
                is ParakeetDownloads.State.Running -> {
                    val fraction = if (state.totalBytes > 0) state.doneBytes.toFloat() / state.totalBytes else 0f
                    LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth())
                    Text(
                        stringResource(
                            R.string.settings_on_device_downloading,
                            state.doneBytes / 1_000_000,
                            state.totalBytes / 1_000_000,
                        ),
                        style = MaterialTheme.typography.bodySmall,
                    )
                    TextButton(onClick = { ParakeetDownloads.cancel() }) {
                        Text(stringResource(R.string.settings_on_device_pause))
                    }
                }
                else -> {
                    if (installed) {
                        Text(
                            stringResource(
                                if (choice == OnDeviceEngine.Choice.PARAKEET) {
                                    R.string.settings_on_device_ready_active
                                } else {
                                    R.string.settings_on_device_ready_inactive
                                },
                            ),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.primary,
                        )
                        TextButton(onClick = {
                            scope.launch {
                                ParakeetOnDevice.deleteModel(context, model)
                                storeTick += 1
                            }
                        }) { Text(stringResource(R.string.settings_on_device_delete, megabytes)) }
                    } else {
                        if (state is ParakeetDownloads.State.Failed) {
                            Text(
                                stringResource(R.string.settings_on_device_failed, state.message),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.error,
                            )
                        }
                        if (choice == OnDeviceEngine.Choice.PARAKEET) {
                            Text(
                                stringResource(R.string.settings_on_device_needs_download),
                                style = MaterialTheme.typography.bodySmall,
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
                            onClick = { ParakeetDownloads.start(context, model) },
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text(stringResource(R.string.settings_on_device_download, megabytes)) }
                        val partial = remember(storeTick, download) { store.partialBytes(model) }
                        if (partial > 0) {
                            TextButton(onClick = {
                                scope.launch {
                                    ParakeetOnDevice.deleteModel(context, model)
                                    storeTick += 1
                                }
                            }) { Text(stringResource(R.string.settings_on_device_discard_partial, partial / 1_000_000)) }
                        }
                    }
                }
            }
        }
    }
}

private fun isMetered(context: Context): Boolean =
    (context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager)
        ?.isActiveNetworkMetered ?: false
