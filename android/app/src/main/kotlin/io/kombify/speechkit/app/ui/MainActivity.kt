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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.app.ui.dev.DictationTestScreen
import io.kombify.speechkit.app.ui.dev.VoiceAgentTestScreen
import io.kombify.speechkit.app.ui.theme.SpeechKitTheme
import io.kombify.speechkit.app.ui.onboarding.KeyboardOnboardingWizard
import io.kombify.speechkit.app.ui.onboarding.KeyboardSetupChecker
import timber.log.Timber

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
private fun SpeechKitApp() {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_app", Context.MODE_PRIVATE) }
    var onboardingComplete by remember { mutableStateOf(prefs.getBoolean("onboarding_done", false)) }
    var selectedTab by remember { mutableIntStateOf(0) }

    // Live-refresh keyboard status on resume from system settings.
    var isKeyboardEnabled by remember { mutableStateOf(KeyboardSetupChecker.isKeyboardEnabled(context)) }
    val lifecycleOwner = androidx.compose.ui.platform.LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = androidx.lifecycle.LifecycleEventObserver { _, event ->
            if (event == androidx.lifecycle.Lifecycle.Event.ON_RESUME) {
                isKeyboardEnabled = KeyboardSetupChecker.isKeyboardEnabled(context)
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
                    NavigationBarItem(
                        selected = selectedTab == 3,
                        onClick = { selectedTab = 3 },
                        label = { Text("Dev") },
                        icon = {},
                    )
                    NavigationBarItem(
                        selected = selectedTab == 4,
                        onClick = { selectedTab = 4 },
                        label = { Text("Voice") },
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
                    onComplete = {
                        prefs.edit().putBoolean("onboarding_done", true).apply()
                        onboardingComplete = true
                    },
                )
            } else {
                when (selectedTab) {
                    0 -> HomeTab(onOpenVoiceAgent = { selectedTab = 4 })
                    1 -> LibraryTab()
                    2 -> SettingsTab()
                    3 -> DictationTestScreen()
                    4 -> VoiceAgentTestScreen()
                }
            }
        }
    }
}

// --- Onboarding Flow with Mode Explanation ---

@Composable
private fun OnboardingFlow(
    isKeyboardEnabled: Boolean,
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
                    "SpeechKit bietet drei Modi für sprachgesteuerte Produktivität:",
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
                    description = "Stelle eine Frage per Sprache, erhalte eine KI-Antwort direkt in der Tastatur. Umschreiben, Zusammenfassen, Übersetzen — alles per Sprachbefehl.",
                    icon = "Sparkle",
                    requirement = "Tastatur aktivieren + HuggingFace Token",
                    isAvailable = true,
                )
                ModeCard(
                    title = "Voice Agent",
                    description = "Persistenter Sprachassistent für längere Konversationen. Audio-zu-Audio in Echtzeit, mit dem Anbieter aus deinem Profil.",
                    icon = "Waveform",
                    // The mode shipped; the card advertised "kommt bald" long
                    // after the Voice tab could hold a real conversation. The
                    // provider is no longer named here either — it follows the
                    // selected profile and is not always Gemini.
                    requirement = "Im Voice-Tab dieser App",
                    isAvailable = true,
                )

                Spacer(Modifier.height(16.dp))

                Text(
                    "Für Dictate und Assist wird die SpeechKit-Tastatur benötigt. Der Voice Agent läuft als eigenständiger Assistent in der App.",
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
                val isSelected = remember { KeyboardSetupChecker.isKeyboardSelected(context) }
                val isAssistant = remember { KeyboardSetupChecker.isAssistantSet(context) }

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
                    Text("\u2190 Zur\u00fcck")
                }
                Text(
                    "HuggingFace Token einrichten",
                    style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    "Für Dictate (Spracherkennung) und Assist (KI-Antworten) wird ein kostenloser HuggingFace Token benötigt.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                Spacer(Modifier.height(8.dp))

                HfTokenInput()

                Spacer(Modifier.height(16.dp))

                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = onComplete) {
                        Text("Überspringen")
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
private fun HfTokenInput() {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_config", Context.MODE_PRIVATE) }
    var token by remember { mutableStateOf(prefs.getString("hf_token", "") ?: "") }
    var saved by remember { mutableStateOf(false) }

    OutlinedTextField(
        value = token,
        onValueChange = { token = it; saved = false },
        label = { Text("HuggingFace Token") },
        placeholder = { Text("hf_...") },
        modifier = Modifier.fillMaxWidth(),
        singleLine = true,
    )
    Spacer(Modifier.height(8.dp))
    Button(
        onClick = {
            prefs.edit().putString("hf_token", token.trim()).apply()
            saved = true
        },
        enabled = token.startsWith("hf_") && token.length > 10,
    ) {
        Text(if (saved) "Gespeichert" else "Token speichern")
    }
    Text(
        "Erstelle einen Token auf huggingface.co/settings/tokens (kostenlos, Read-Zugriff reicht).",
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

// --- Home Tab (Dashboard) ---

@Composable
private fun HomeTab(onOpenVoiceAgent: () -> Unit = {}) {
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
            StatCard("Transkriptionen", "0", Modifier.weight(1f))
            StatCard("Assist-Anfragen", "0", Modifier.weight(1f))
        }

        Spacer(Modifier.height(8.dp))

        // Quick Actions
        Text("Schnellzugriff", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Medium)

        QuickActionRow("Dictate", "Sprache zu Text in jeder App") {}
        QuickActionRow("Assist", "KI-Antwort auf eine Frage") {}
        QuickActionRow("Voice Agent", "Sprachdialog testen (Voice-Tab)") { onOpenVoiceAgent() }
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
private fun LibraryTab() {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Library", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
        Text(
            "Hier erscheinen deine Transkriptionen, Quick Notes und Assist-Antworten.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        // Tabs for different content types
        var libraryTab by remember { mutableIntStateOf(0) }
        TabRow(selectedTabIndex = libraryTab) {
            Tab(selected = libraryTab == 0, onClick = { libraryTab = 0 }) { Text("Transkriptionen", Modifier.padding(12.dp)) }
            Tab(selected = libraryTab == 1, onClick = { libraryTab = 1 }) { Text("Quick Notes", Modifier.padding(12.dp)) }
            Tab(selected = libraryTab == 2, onClick = { libraryTab = 2 }) { Text("Assist", Modifier.padding(12.dp)) }
        }

        Text(
            "Noch keine Eintraege. Nutze die SpeechKit-Tastatur um loszulegen.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f),
            textAlign = TextAlign.Center,
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 32.dp),
        )
    }
}

// --- Settings Tab ---

@Composable
private fun SettingsTab() {
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
                HfTokenInput()
            }
        }

        // Keyboard Setup
        Card(modifier = Modifier.fillMaxWidth()) {
            val settingsContext = LocalContext.current
            var enabled by remember { mutableStateOf(KeyboardSetupChecker.isKeyboardEnabled(settingsContext)) }
            val settingsLifecycle = androidx.compose.ui.platform.LocalLifecycleOwner.current
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
                Text("Über", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(4.dp))
                Text("kombify SpeechKit v0.7.0", style = MaterialTheme.typography.bodySmall)
                Text("AI-powered Voice Keyboard", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text("github.com/kombifyio/SpeechKit", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
            }
        }
    }
}
