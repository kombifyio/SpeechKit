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
import androidx.compose.runtime.produceState
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
import io.kombify.speechkit.stt.parakeet.ParakeetModel
import io.kombify.speechkit.stt.parakeet.ParakeetModels
import io.kombify.speechkit.stt.parakeet.ParakeetOnDevice
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Chooses the on-device recognizer and the offline model, and manages the model download.
 *
 * The system recognizer stays the default; the offline model becomes selectable once the device can run one, and
 * is used for on-device dictation (keyboard, assistant, Voice IME) only after it is downloaded. The models offered
 * are the ones this device can run ([ParakeetOnDevice.supportedModels]): Phonon-2.5 (recommended), Parakeet Ultra
 * on phones with enough memory, and Phonon-2.5 English. The download runs in the background ([ParakeetDownloads])
 * and resumes where it stopped.
 */
@Composable
internal fun OnDeviceModelCard() {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    // The support checks load native libraries, so they run off the main thread; null until they are done.
    val probe by produceState<Pair<List<ParakeetModel>, ParakeetModel?>?>(initialValue = null) {
        value = withContext(Dispatchers.Default) {
            val supported = ParakeetOnDevice.supportedModels(context).filter { it != ParakeetModels.V3 }
            supported to (ParakeetOnDevice.selectedModel(context).takeIf { it in supported } ?: supported.firstOrNull())
        }
    }
    val offered = probe?.first.orEmpty()
    var chosen by remember { mutableStateOf<ParakeetModel?>(null) }
    val model = chosen ?: probe?.second
    val store = remember { ParakeetOnDevice.modelStore(context) }
    val download by ParakeetDownloads.state.collectAsState()
    var choice by remember { mutableStateOf(OnDeviceEngine.choice(context)) }
    // Bumped after a download finishes or the model is deleted, so the
    // installed state is read again.
    var storeTick by remember { mutableIntStateOf(0) }
    val installed = remember(storeTick, download, model) { model?.let(store::isInstalled) ?: false }

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

            if (probe == null) return@Column
            val current = model
            if (current == null) {
                val reason = (ParakeetOnDevice.support(context, ParakeetModels.PHONON) as? ParakeetOnDevice.Support.Unsupported)
                    ?.reason.orEmpty()
                Text(
                    stringResource(R.string.settings_on_device_unsupported, reason),
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

            if (offered.size > 1) {
                Text(
                    stringResource(R.string.settings_on_device_model_title),
                    style = MaterialTheme.typography.labelMedium,
                )
                Row(
                    modifier = Modifier.horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    for (option in offered) {
                        FilterChip(
                            selected = option == current,
                            onClick = {
                                chosen = option
                                ParakeetOnDevice.setSelectedModel(context, option)
                                // The next segment loads the newly selected model.
                                ParakeetOnDevice.release()
                            },
                            label = {
                                Text(stringResource(R.string.settings_on_device_model_chip, option.name, option.sizeBytes / 1_000_000))
                            },
                        )
                    }
                }
            }

            val megabytes = current.sizeBytes / 1_000_000
            // A download or failure of another model does not belong under this model's chip.
            val state = download.takeIf { it.modelId == null || it.modelId == current.id } ?: ParakeetDownloads.State.Idle
            when (state) {
                is ParakeetDownloads.State.Running -> {
                    val fraction = if (state.totalBytes > 0) state.doneBytes.toFloat() / state.totalBytes else 0f
                    LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth())
                    Text(
                        stringResource(
                            R.string.settings_on_device_downloading,
                            ParakeetModels.byId(state.modelId)?.name ?: current.name,
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
                                current.name,
                            ),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.primary,
                        )
                        TextButton(onClick = {
                            scope.launch {
                                ParakeetOnDevice.deleteModel(context, current)
                                storeTick += 1
                            }
                        }) { Text(stringResource(R.string.settings_on_device_delete, current.name, megabytes)) }
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
                                stringResource(R.string.settings_on_device_needs_download, current.name),
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
                            onClick = { ParakeetDownloads.start(context, current) },
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text(stringResource(R.string.settings_on_device_download, current.name, megabytes)) }
                        val partial = remember(storeTick, download, current) { store.partialBytes(current) }
                        if (partial > 0) {
                            TextButton(onClick = {
                                scope.launch {
                                    ParakeetOnDevice.deleteModel(context, current)
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
