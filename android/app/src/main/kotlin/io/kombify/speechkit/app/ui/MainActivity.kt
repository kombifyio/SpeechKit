package io.kombify.speechkit.app.ui

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.app.ui.theme.SpeechKitTheme
import io.kombify.speechkit.app.ui.onboarding.KeyboardOnboardingWizard
import io.kombify.speechkit.app.ui.onboarding.KeyboardSetupChecker
import io.kombify.speechkit.store.QuickNote
import io.kombify.speechkit.store.Stats
import io.kombify.speechkit.store.Transcription
import timber.log.Timber
import java.time.ZoneId
import java.time.format.DateTimeFormatter

@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    private val micPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted -> Timber.d("RECORD_AUDIO permission: $granted") }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        requestMicPermissionIfNeeded()
        setContent { SpeechKitTheme { SpeechKitApp() } }
    }

    private fun requestMicPermissionIfNeeded() {
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO)
            != PackageManager.PERMISSION_GRANTED
        ) {
            micPermissionLauncher.launch(Manifest.permission.RECORD_AUDIO)
        }
    }
}

// --- App Root ---

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SpeechKitApp(viewModel: MainViewModel = hiltViewModel()) {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_app", Context.MODE_PRIVATE) }
    var onboardingComplete by remember { mutableStateOf(prefs.getBoolean("onboarding_done", false)) }
    var selectedTab by remember { mutableIntStateOf(0) }

    val stats by viewModel.stats.collectAsStateWithLifecycle()
    val transcriptions by viewModel.transcriptions.collectAsStateWithLifecycle()
    val quickNotes by viewModel.quickNotes.collectAsStateWithLifecycle()

    // Refresh store data when the app resumes (user may have dictated via keyboard).
    // Live-refresh keyboard status on resume from system settings.
    var isKeyboardEnabled by remember { mutableStateOf(KeyboardSetupChecker.isKeyboardEnabled(context)) }
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                isKeyboardEnabled = KeyboardSetupChecker.isKeyboardEnabled(context)
                viewModel.refresh()
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("kombify SpeechKit", fontWeight = FontWeight.Bold) },
            )
        },
        bottomBar = {
            if (onboardingComplete) {
                NavigationBar {
                    NavigationBarItem(
                        selected = selectedTab == 0,
                        onClick = { selectedTab = 0 },
                        label = { Text("Home") },
                        icon = {},
                    )
                    NavigationBarItem(
                        selected = selectedTab == 1,
                        onClick = { selectedTab = 1 },
                        label = { Text("Library") },
                        icon = {},
                    )
                    NavigationBarItem(
                        selected = selectedTab == 2,
                        onClick = { selectedTab = 2 },
                        label = { Text("Settings") },
                        icon = {},
                    )
                }
            }
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            if (!onboardingComplete) {
                OnboardingFlow(
                    isKeyboardEnabled = isKeyboardEnabled,
                    viewModel = viewModel,
                    onComplete = {
                        prefs.edit().putBoolean("onboarding_done", true).apply()
                        onboardingComplete = true
                    },
                )
            } else {
                when (selectedTab) {
                    0 -> HomeTab(stats = stats)
                    1 -> LibraryTab(
                        transcriptions = transcriptions,
                        quickNotes = quickNotes,
                        onDeleteQuickNote = viewModel::deleteQuickNote,
                        onTogglePinQuickNote = viewModel::togglePinQuickNote,
                    )
                    2 -> SettingsTab(viewModel)
                }
            }
        }
    }
}

// --- Onboarding Flow with Mode Explanation ---

@Composable
private fun OnboardingFlow(
    isKeyboardEnabled: Boolean,
    viewModel: MainViewModel,
    onComplete: () -> Unit,
) {
    val context = LocalContext.current
    var step by remember { mutableIntStateOf(0) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        when (step) {
            0 -> {
                // Step 1: Welcome + Mode Explanation
                Text(
                    "Willkommen bei SpeechKit",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    "SpeechKit bietet drei Modi fuer sprachgesteuerte Produktivitaet:",
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                Spacer(Modifier.height(8.dp))

                ModeCard(
                    title = "Dictate",
                    description = "Sprache zu Text. Diktiere in jeder App direkt per Tastatur. Kein KI-Processing -- pure Transkription.",
                    icon = "Mic",
                    requirement = "Tastatur aktivieren",
                    isAvailable = true,
                )
                ModeCard(
                    title = "Assist",
                    description = "Stelle eine Frage per Sprache, erhalte eine KI-Antwort direkt in der Tastatur. Umschreiben, Zusammenfassen, Uebersetzen -- alles per Sprachbefehl.",
                    icon = "Sparkle",
                    requirement = "Tastatur aktivieren + HuggingFace Token",
                    isAvailable = true,
                )
                ModeCard(
                    title = "Voice Agent",
                    description = "Persistenter Sprachassistent fuer laengere Konversationen. Audio-zu-Audio mit Gemini Live -- natuerliche Gespraeche in Echtzeit.",
                    icon = "Waveform",
                    requirement = "Separat in der App einrichten (kommt bald)",
                    isAvailable = false,
                )

                Spacer(Modifier.height(16.dp))

                Text(
                    "Fuer Dictate und Assist wird die SpeechKit-Tastatur benoetigt. Der Voice Agent laeuft als eigenstaendiger Assistent in der App.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                Button(
                    onClick = { step = 1 },
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text("Tastatur einrichten")
                }
            }
            1 -> {
                // Step 2: Keyboard Setup
                val isSelected = KeyboardSetupChecker.isKeyboardSelected(context)
                val isAssistant = KeyboardSetupChecker.isAssistantSet(context)

                KeyboardOnboardingWizard(
                    isKeyboardEnabled = isKeyboardEnabled,
                    isKeyboardSelected = isSelected,
                    isAssistantSet = isAssistant,
                    onComplete = { step = 2 },
                    onBack = { step = 0 },
                )
            }
            2 -> {
                // Step 3: HF Token Setup
                TextButton(onClick = { step = 1 }) {
                    Text("\u2190 Zurueck")
                }
                Text(
                    "HuggingFace Token einrichten",
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    "Fuer Dictate (Spracherkennung) und Assist (KI-Antworten) wird ein kostenloser HuggingFace Token benoetigt.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                Spacer(Modifier.height(8.dp))

                HfTokenInput(viewModel)

                Spacer(Modifier.height(16.dp))

                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = onComplete) {
                        Text("Ueberspringen")
                    }
                    Button(onClick = onComplete) {
                        Text("Fertig")
                    }
                }
            }
        }
    }
}

@Composable
private fun ModeCard(
    title: String,
    description: String,
    icon: String,
    requirement: String,
    isAvailable: Boolean,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = if (isAvailable) MaterialTheme.colorScheme.surfaceContainerHigh
            else MaterialTheme.colorScheme.surfaceContainerLow,
        ),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                // Icon placeholder
                Box(
                    modifier = Modifier
                        .size(40.dp)
                        .clip(RoundedCornerShape(10.dp))
                        .background(
                            if (isAvailable) MaterialTheme.colorScheme.primaryContainer
                            else MaterialTheme.colorScheme.surfaceVariant
                        ),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = when (icon) {
                            "Mic" -> "\uD83C\uDFA4"
                            "Sparkle" -> "\u2728"
                            "Waveform" -> "\uD83C\uDF99"
                            else -> ""
                        },
                        style = MaterialTheme.typography.titleLarge,
                    )
                }
                Column {
                    Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                    if (!isAvailable) {
                        Text(
                            "Kommt bald",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            Spacer(Modifier.height(8.dp))
            Text(description, style = MaterialTheme.typography.bodySmall)
            Spacer(Modifier.height(4.dp))
            Text(
                requirement,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

// --- HF Token Input ---

@Composable
private fun HfTokenInput(viewModel: MainViewModel) {
    val token by viewModel.hfToken.collectAsStateWithLifecycle()
    val saved by viewModel.hfTokenSaved.collectAsStateWithLifecycle()
    val error by viewModel.hfTokenError.collectAsStateWithLifecycle()

    OutlinedTextField(
        value = token,
        onValueChange = viewModel::updateHuggingFaceToken,
        label = { Text("HuggingFace Token") },
        placeholder = { Text("hf_...") },
        modifier = Modifier.fillMaxWidth(),
        singleLine = true,
    )
    Spacer(Modifier.height(8.dp))
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Button(
            onClick = viewModel::saveHuggingFaceToken,
            enabled = token.startsWith("hf_") && token.length > 10,
        ) {
            Text(if (saved) "Gespeichert" else "Token speichern")
        }
        if (token.isNotBlank()) {
            OutlinedButton(onClick = viewModel::clearHuggingFaceToken) {
                Text("Entfernen")
            }
        }
    }
    if (error != null) {
        Spacer(Modifier.height(8.dp))
        Text(
            text = error ?: "",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.error,
        )
    }
    Text(
        "Erstelle einen Token auf huggingface.co/settings/tokens (kostenlos, Read-Zugriff reicht).",
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

// --- Home Tab (Dashboard) ---

@Composable
private fun HomeTab(stats: Stats) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text("Dashboard", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)

        // Status Cards
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            StatCard("Tastatur", "Aktiv", Modifier.weight(1f))
            StatCard("Modus", "Dictate", Modifier.weight(1f))
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            StatCard("Transkriptionen", stats.transcriptions.toString(), Modifier.weight(1f))
            StatCard("Quick Notes", stats.quickNotes.toString(), Modifier.weight(1f))
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            StatCard(
                "Audio-Dauer",
                formatDurationMs(stats.totalAudioDurationMs),
                Modifier.weight(1f),
            )
            StatCard(
                "Latenz (avg)",
                if (stats.averageLatencyMs > 0) "${stats.averageLatencyMs} ms" else "--",
                Modifier.weight(1f),
            )
        }

        Spacer(Modifier.height(8.dp))

        // Quick Actions
        Text("Schnellzugriff", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Medium)

        QuickActionRow("Dictate", "Sprache zu Text in jeder App") {}
        QuickActionRow("Assist", "KI-Antwort auf eine Frage") {}
        QuickActionRow("Voice Agent", "Kommt bald -- persistenter Sprachassistent") {}
    }
}

@Composable
private fun StatCard(label: String, value: String, modifier: Modifier = Modifier) {
    Card(modifier = modifier) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(value, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
            Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun QuickActionRow(title: String, description: String, onClick: () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick),
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
                Text(description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

// --- Library Tab ---

@Composable
private fun LibraryTab(
    transcriptions: List<Transcription>,
    quickNotes: List<QuickNote>,
    onDeleteQuickNote: (Long) -> Unit,
    onTogglePinQuickNote: (QuickNote) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 16.dp, top = 16.dp),
    ) {
        Text("Library", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(4.dp))
        Text(
            "Hier erscheinen deine Transkriptionen, Quick Notes und Assist-Antworten.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(12.dp))

        var libraryTab by remember { mutableIntStateOf(0) }
        TabRow(selectedTabIndex = libraryTab) {
            Tab(selected = libraryTab == 0, onClick = { libraryTab = 0 }) {
                Text("Transkriptionen", Modifier.padding(12.dp))
            }
            Tab(selected = libraryTab == 1, onClick = { libraryTab = 1 }) {
                Text("Quick Notes", Modifier.padding(12.dp))
            }
        }

        when (libraryTab) {
            0 -> TranscriptionList(transcriptions)
            1 -> QuickNoteList(quickNotes, onDeleteQuickNote, onTogglePinQuickNote)
        }
    }
}

@Composable
private fun TranscriptionList(transcriptions: List<Transcription>) {
    if (transcriptions.isEmpty()) {
        EmptyLibraryHint("Noch keine Transkriptionen. Nutze die SpeechKit-Tastatur um loszulegen.")
        return
    }

    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(transcriptions, key = { it.id }) { transcription ->
            TranscriptionCard(transcription)
        }
    }
}

@Composable
private fun TranscriptionCard(transcription: Transcription) {
    val formatter = remember {
        DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm")
            .withZone(ZoneId.systemDefault())
    }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(12.dp)) {
            Text(
                text = transcription.text,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 4,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(8.dp))
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = "${transcription.provider} / ${transcription.language}",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = formatter.format(transcription.createdAt),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Row(
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(
                    text = formatDurationMs(transcription.durationMs),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.primary,
                )
                Text(
                    text = "${transcription.latencyMs} ms",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}

@Composable
private fun QuickNoteList(
    quickNotes: List<QuickNote>,
    onDelete: (Long) -> Unit,
    onTogglePin: (QuickNote) -> Unit,
) {
    if (quickNotes.isEmpty()) {
        EmptyLibraryHint("Noch keine Quick Notes. Nutze die SpeechKit-Tastatur um loszulegen.")
        return
    }

    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(quickNotes, key = { it.id }) { note ->
            QuickNoteCard(note, onDelete = { onDelete(note.id) }, onTogglePin = { onTogglePin(note) })
        }
    }
}

@Composable
private fun QuickNoteCard(
    note: QuickNote,
    onDelete: () -> Unit,
    onTogglePin: () -> Unit,
) {
    val formatter = remember {
        DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm")
            .withZone(ZoneId.systemDefault())
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = if (note.pinned) MaterialTheme.colorScheme.secondaryContainer
            else MaterialTheme.colorScheme.surfaceContainerHigh,
        ),
    ) {
        Column(modifier = Modifier.padding(12.dp)) {
            if (note.pinned) {
                Text(
                    text = "Angepinnt",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.secondary,
                    fontWeight = FontWeight.Bold,
                )
                Spacer(Modifier.height(4.dp))
            }
            Text(
                text = note.text,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 6,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(8.dp))
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = formatter.format(note.updatedAt),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    TextButton(onClick = onTogglePin) {
                        Text(if (note.pinned) "Loslassen" else "Anpinnen", style = MaterialTheme.typography.labelSmall)
                    }
                    TextButton(onClick = onDelete) {
                        Text("Loeschen", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
    }
}

@Composable
private fun EmptyLibraryHint(message: String) {
    Text(
        text = message,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f),
        textAlign = TextAlign.Center,
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 32.dp),
    )
}

private fun formatDurationMs(ms: Long): String {
    if (ms <= 0) return "--"
    val totalSeconds = ms / 1000
    val minutes = totalSeconds / 60
    val seconds = totalSeconds % 60
    return if (minutes > 0) "${minutes}m ${seconds}s" else "${seconds}s"
}

// --- Settings Tab ---

@Composable
private fun SettingsTab(viewModel: MainViewModel) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text("Einstellungen", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)

        // HF Token
        Card(modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.padding(16.dp)) {
                Text("HuggingFace Token", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(8.dp))
                HfTokenInput(viewModel)
            }
        }

        // Keyboard Setup
        Card(modifier = Modifier.fillMaxWidth()) {
            val settingsContext = LocalContext.current
            var enabled by remember { mutableStateOf(KeyboardSetupChecker.isKeyboardEnabled(settingsContext)) }
            val settingsLifecycle = LocalLifecycleOwner.current
            DisposableEffect(settingsLifecycle) {
                val obs = LifecycleEventObserver { _, event ->
                    if (event == Lifecycle.Event.ON_RESUME) {
                        enabled = KeyboardSetupChecker.isKeyboardEnabled(settingsContext)
                    }
                }
                settingsLifecycle.lifecycle.addObserver(obs)
                onDispose { settingsLifecycle.lifecycle.removeObserver(obs) }
            }
            Column(modifier = Modifier.padding(16.dp)) {
                Text("Tastatur", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(4.dp))
                Text(
                    if (enabled) "SpeechKit Tastatur ist aktiviert"
                    else "SpeechKit Tastatur ist nicht aktiviert",
                    style = MaterialTheme.typography.bodySmall,
                    color = if (enabled) MaterialTheme.colorScheme.primary
                    else MaterialTheme.colorScheme.error,
                )
            }
        }

        // About
        Card(modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.padding(16.dp)) {
                Text("Ueber", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(4.dp))
                Text("kombify SpeechKit v${BuildConfig.VERSION_NAME}", style = MaterialTheme.typography.bodySmall)
                Text("AI-powered Voice Keyboard", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("github.com/kombifyio/SpeechKit", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
            }
        }
    }
}
