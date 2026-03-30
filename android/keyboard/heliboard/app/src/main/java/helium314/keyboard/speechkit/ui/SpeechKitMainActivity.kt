package helium314.keyboard.speechkit.ui

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Bundle
import android.provider.Settings
import android.view.inputmethod.InputMethodManager
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
import kotlinx.coroutines.launch
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import helium314.keyboard.latin.R

class SpeechKitMainActivity : ComponentActivity() {

    private val micPermission = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) {}

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO)
            != PackageManager.PERMISSION_GRANTED
        ) micPermission.launch(Manifest.permission.RECORD_AUDIO)

        setContent {
            MaterialTheme(colorScheme = dynamicColorScheme()) {
                SpeechKitApp()
            }
        }
    }

    @Composable
    private fun dynamicColorScheme(): ColorScheme {
        return if (android.os.Build.VERSION.SDK_INT >= 31) {
            dynamicLightColorScheme(this)
        } else lightColorScheme()
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SpeechKitApp() {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_app", Context.MODE_PRIVATE) }
    var onboardingDone by remember { mutableStateOf(prefs.getBoolean("onboarding_done", false)) }
    var tab by remember { mutableIntStateOf(0) }

    Scaffold(
        topBar = { TopAppBar(title = { Text("kombify SpeechKit", fontWeight = FontWeight.Bold) }) },
        bottomBar = {
            if (onboardingDone) {
                NavigationBar {
                    listOf("Home", "Library", "Settings").forEachIndexed { i, label ->
                        NavigationBarItem(
                            selected = tab == i,
                            onClick = { tab = i },
                            label = { Text(label) },
                            icon = {},
                        )
                    }
                }
            }
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            if (!onboardingDone) {
                OnboardingFlow {
                    prefs.edit().putBoolean("onboarding_done", true).apply()
                    onboardingDone = true
                }
            } else {
                when (tab) {
                    0 -> HomeTab()
                    1 -> LibraryTab()
                    2 -> SettingsTab()
                }
            }
        }
    }
}

// --- Onboarding ---

@Composable
private fun OnboardingFlow(onComplete: () -> Unit) {
    val context = LocalContext.current
    var step by remember { mutableIntStateOf(0) }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        when (step) {
            0 -> {
                Text("Willkommen bei SpeechKit", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                Text("SpeechKit bietet drei Modi:", style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)

                ModeCard("Dictate", "Sprache zu Text. Diktiere in jeder App direkt per Tastatur.", "Tastatur aktivieren", true)
                ModeCard("Assist", "Frage per Sprache stellen, KI-Antwort direkt in der Tastatur. Umschreiben, Zusammenfassen, Uebersetzen.", "Tastatur + HuggingFace Token", true)
                ModeCard("Voice Agent", "Persistenter Sprachassistent fuer laengere Konversationen mit Gemini Live.", "In der App einrichten (kommt bald)", false)

                Spacer(Modifier.height(8.dp))
                Text("Fuer Dictate und Assist brauchst du die SpeechKit-Tastatur.\nFuer Voice Agent richte den Assistenten in der App ein.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)

                Button(onClick = { step = 1 }, Modifier.fillMaxWidth()) { Text("Tastatur einrichten") }
            }
            1 -> {
                Text("Tastatur aktivieren", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                Text("Aktiviere SpeechKit in den System-Einstellungen.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)

                Button(onClick = {
                    context.startActivity(android.content.Intent(Settings.ACTION_INPUT_METHOD_SETTINGS).apply {
                        flags = android.content.Intent.FLAG_ACTIVITY_NEW_TASK
                    })
                }) { Text("Eingabemethoden oeffnen") }

                OutlinedButton(onClick = {
                    val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
                    imm.showInputMethodPicker()
                }) { Text("Tastatur waehlen") }

                Spacer(Modifier.height(16.dp))
                Button(onClick = { step = 2 }, Modifier.fillMaxWidth()) { Text("Weiter") }
            }
            2 -> {
                Text("HuggingFace Token", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                Text("Fuer Spracherkennung und KI-Antworten wird ein kostenloser HuggingFace Token benoetigt.", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.height(8.dp))
                HfTokenInput()
                Spacer(Modifier.height(16.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = onComplete) { Text("Ueberspringen") }
                    Button(onClick = onComplete) { Text("Fertig") }
                }
            }
        }
    }
}

@Composable
private fun ModeCard(title: String, description: String, requirement: String, available: Boolean) {
    Card(
        Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = if (available) MaterialTheme.colorScheme.surfaceContainerHigh
            else MaterialTheme.colorScheme.surfaceContainerLow,
        ),
    ) {
        Column(Modifier.padding(16.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            if (!available) Text("Kommt bald", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.height(4.dp))
            Text(description, style = MaterialTheme.typography.bodySmall)
            Spacer(Modifier.height(4.dp))
            Text(requirement, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        }
    }
}

// --- HF Token ---

@Composable
private fun HfTokenInput() {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_config", Context.MODE_PRIVATE) }
    var token by remember { mutableStateOf(prefs.getString("hf_token", "") ?: "") }
    var saved by remember { mutableStateOf(false) }

    OutlinedTextField(
        value = token, onValueChange = { token = it; saved = false },
        label = { Text("HuggingFace Token") }, placeholder = { Text("hf_...") },
        modifier = Modifier.fillMaxWidth(), singleLine = true,
    )
    Spacer(Modifier.height(8.dp))
    Button(
        onClick = { prefs.edit().putString("hf_token", token.trim()).apply(); saved = true },
        enabled = token.startsWith("hf_") && token.length > 10,
    ) { Text(if (saved) "Gespeichert" else "Speichern") }
    Text("Token erstellen: huggingface.co/settings/tokens (kostenlos)", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

// --- Tabs ---

@Composable
private fun HomeTab() {
    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Text("Dashboard", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            StatCard("Tastatur", "Aktiv", Modifier.weight(1f))
            StatCard("Modus", "Dictate", Modifier.weight(1f))
        }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            StatCard("Transkriptionen", "0", Modifier.weight(1f))
            StatCard("Assist", "0", Modifier.weight(1f))
        }
    }
}

@Composable
private fun StatCard(label: String, value: String, modifier: Modifier = Modifier) {
    Card(modifier) {
        Column(Modifier.padding(16.dp)) {
            Text(value, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
            Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun LibraryTab() {
    Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text("Library", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
        var libraryTab by remember { mutableIntStateOf(0) }
        TabRow(selectedTabIndex = libraryTab) {
            Tab(selected = libraryTab == 0, onClick = { libraryTab = 0 }) { Text("Dictate", Modifier.padding(12.dp)) }
            Tab(selected = libraryTab == 1, onClick = { libraryTab = 1 }) { Text("Notes", Modifier.padding(12.dp)) }
            Tab(selected = libraryTab == 2, onClick = { libraryTab = 2 }) { Text("Assist", Modifier.padding(12.dp)) }
        }
        Text("Noch keine Eintraege.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f), textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth().padding(top = 32.dp))
    }
}

@Composable
private fun SettingsTab() {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_config", Context.MODE_PRIVATE) }

    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Text("Einstellungen", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)

        // --- Tastatur & Assistant ---
        SettingsSection("Tastatur & Assistant") {
            val keyboardEnabled = remember { isImeEnabled(context) }

            SettingsRow(
                title = "SpeechKit Tastatur",
                subtitle = if (keyboardEnabled) "Aktiviert" else "Nicht aktiviert",
                isOk = keyboardEnabled,
            ) {
                Button(onClick = {
                    context.startActivity(android.content.Intent(Settings.ACTION_INPUT_METHOD_SETTINGS).apply {
                        flags = android.content.Intent.FLAG_ACTIVITY_NEW_TASK
                    })
                }, Modifier.height(36.dp)) { Text("Einrichten", style = MaterialTheme.typography.labelMedium) }
            }

            SettingsRow(
                title = "Tastatur auswaehlen",
                subtitle = "SpeechKit als aktive Eingabemethode setzen",
            ) {
                OutlinedButton(onClick = {
                    val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
                    imm.showInputMethodPicker()
                }, Modifier.height(36.dp)) { Text("Waehlen", style = MaterialTheme.typography.labelMedium) }
            }

            SettingsRow(
                title = "Voice Assistant",
                subtitle = "SpeechKit als Standard-Assistenten setzen (optional)",
            ) {
                OutlinedButton(onClick = {
                    context.startActivity(android.content.Intent(Settings.ACTION_VOICE_INPUT_SETTINGS).apply {
                        flags = android.content.Intent.FLAG_ACTIVITY_NEW_TASK
                    })
                }, Modifier.height(36.dp)) { Text("Einrichten", style = MaterialTheme.typography.labelMedium) }
            }
        }

        // --- Lokales Modell ---
        SettingsSection("Lokales Modell") {
            LocalModelSettings(prefs)
        }

        // --- Provider ---
        SettingsSection("STT Provider") {
            ProviderSettings(prefs)
        }

        // --- HF Token ---
        SettingsSection("HuggingFace Token") {
            HfTokenInput()
        }

        // --- Ueber ---
        SettingsSection("Ueber") {
            Text("kombify SpeechKit v0.8.0", style = MaterialTheme.typography.bodySmall)
            Text("AI-powered Voice Keyboard", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text("github.com/kombifyio/SpeechKit", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
        }
    }
}

@Composable
private fun SettingsSection(title: String, content: @Composable ColumnScope.() -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(title, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
            content()
        }
    }
}

@Composable
private fun SettingsRow(
    title: String,
    subtitle: String,
    isOk: Boolean? = null,
    action: @Composable () -> Unit = {},
) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
                if (isOk != null) {
                    Text(
                        if (isOk) "\u2713" else "\u2717",
                        color = if (isOk) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
            Text(subtitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        action()
    }
}

// --- Local Model Download ---

@Composable
private fun LocalModelSettings(prefs: android.content.SharedPreferences) {
    val context = LocalContext.current
    val modelDir = remember { java.io.File(context.filesDir, "models/whisper-tiny") }
    var isInstalled by remember { mutableStateOf(java.io.File(modelDir, "encoder.onnx").exists()) }
    var isDownloading by remember { mutableStateOf(false) }
    var progress by remember { mutableStateOf("") }
    var showConfirmDialog by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    if (isInstalled) {
        SettingsRow(
            title = "Whisper Tiny (lokal)",
            subtitle = "Installiert -- Offline-Transkription verfuegbar",
            isOk = true,
        )
    } else {
        SettingsRow(
            title = "Lokales Modell",
            subtitle = if (isDownloading) progress else "Kein lokales Modell installiert. Cloud wird verwendet.",
        ) {
            if (!isDownloading) {
                Button(onClick = { showConfirmDialog = true }, Modifier.height(36.dp)) {
                    Text("Installieren", style = MaterialTheme.typography.labelMedium)
                }
            } else {
                CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
            }
        }
    }

    if (showConfirmDialog) {
        AlertDialog(
            onDismissRequest = { showConfirmDialog = false },
            title = { Text("Lokales Modell installieren") },
            text = {
                Text("Das Whisper Tiny Modell (~75 MB) wird heruntergeladen und lokal gespeichert. " +
                    "Danach kannst du Sprache auch ohne Internet transkribieren.\n\n" +
                    "Das Modell wird automatisch als bevorzugter Provider eingestellt.")
            },
            confirmButton = {
                Button(onClick = {
                    showConfirmDialog = false
                    isDownloading = true
                    scope.launch {
                        try {
                            progress = "Lade Encoder herunter..."
                            downloadModel(context, "encoder.onnx", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin") { p ->
                                progress = "Lade herunter: ${p}%"
                            }
                            // Mark as installed and set as default
                            prefs.edit().putString("stt_provider", "local").apply()
                            isInstalled = true
                            isDownloading = false
                            progress = ""
                        } catch (e: Exception) {
                            progress = "Fehler: ${e.message?.take(60)}"
                            isDownloading = false
                        }
                    }
                }) { Text("Installieren") }
            },
            dismissButton = {
                OutlinedButton(onClick = { showConfirmDialog = false }) { Text("Abbrechen") }
            },
        )
    }
}

private suspend fun downloadModel(
    context: Context,
    filename: String,
    url: String,
    onProgress: (Int) -> Unit,
) = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
    val dir = java.io.File(context.filesDir, "models/whisper-tiny")
    dir.mkdirs()
    val target = java.io.File(dir, filename)

    val connection = java.net.URL(url).openConnection() as java.net.HttpURLConnection
    connection.connectTimeout = 30000
    connection.readTimeout = 60000
    connection.connect()

    val totalSize = connection.contentLength.toLong()
    var downloaded = 0L

    connection.inputStream.use { input ->
        target.outputStream().use { output ->
            val buffer = ByteArray(8192)
            var read: Int
            while (input.read(buffer).also { read = it } != -1) {
                output.write(buffer, 0, read)
                downloaded += read
                if (totalSize > 0) {
                    val pct = (downloaded * 100 / totalSize).toInt()
                    kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.Main) { onProgress(pct) }
                }
            }
        }
    }
}

// --- Provider Selection ---

@Composable
private fun ProviderSettings(prefs: android.content.SharedPreferences) {
    var selected by remember { mutableStateOf(prefs.getString("stt_provider", "cloud") ?: "cloud") }

    val options = listOf(
        Triple("auto", "Automatisch", "Lokal wenn verfuegbar, sonst Cloud"),
        Triple("local", "Lokal (Whisper)", "Offline, on-device -- lokales Modell erforderlich"),
        Triple("cloud", "Cloud (HuggingFace)", "Online, beste Qualitaet -- HF Token erforderlich"),
    )

    options.forEach { (key, title, desc) ->
        Row(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(8.dp))
                .clickable {
                    selected = key
                    prefs.edit().putString("stt_provider", key).apply()
                }
                .padding(vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            RadioButton(selected = selected == key, onClick = {
                selected = key
                prefs.edit().putString("stt_provider", key).apply()
            })
            Column(Modifier.padding(start = 8.dp)) {
                Text(title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
                Text(desc, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

private fun isImeEnabled(context: Context): Boolean {
    return try {
        val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
        imm.enabledInputMethodList.any { it.packageName == context.packageName }
    } catch (_: Exception) { false }
}
