package io.kombify.speechkit.app.ui

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
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
import androidx.compose.ui.res.stringResource
import io.kombify.speechkit.R
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.net.DictationController
import io.kombify.speechkit.net.LanServer
import io.kombify.speechkit.net.NsdLanFinder
import io.kombify.speechkit.net.StoredServerProfile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import androidx.compose.foundation.horizontalScroll
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.ui.res.painterResource
import androidx.compose.foundation.layout.size
import io.kombify.speechkit.app.keyboard.KeyboardAgentPreferences
import io.kombify.speechkit.app.keyboard.KeyboardIconChoice
import io.kombify.speechkit.app.keyboard.KeyboardIconPreferences
import io.kombify.speechkit.app.keyboard.iconChoicesFor
import io.kombify.speechkit.app.build.ShippedDefaults
import io.kombify.speechkit.app.companion.CompanionProvision
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.app.companion.ConnectCloudIntent
import io.kombify.speechkit.coinstall.v1.CoinstallContract
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.fallbackModeAfterDisconnect
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    @Inject lateinit var companionProvisioner: CompanionProvisioner

    @Inject lateinit var profileSource: ConnectionProfileSource

    private val micPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted -> Timber.d("RECORD_AUDIO permission: $granted") }

    private var pendingConnectCloud by mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        requestMicPermissionIfNeeded()
        pendingConnectCloud = ConnectCloudIntent.matches(intent)
        setContent {
            SpeechKitTheme {
                SpeechKitApp(
                    companionProvisioner = companionProvisioner,
                    profileSource = profileSource,
                    connectCloudRequested = pendingConnectCloud,
                    onConnectCloudConsumed = { pendingConnectCloud = false },
                )
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        if (ConnectCloudIntent.matches(intent)) {
            pendingConnectCloud = true
        }
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
private fun SpeechKitApp(
    companionProvisioner: CompanionProvisioner,
    profileSource: ConnectionProfileSource,
    connectCloudRequested: Boolean = false,
    onConnectCloudConsumed: () -> Unit = {},
) {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_app", Context.MODE_PRIVATE) }
    var onboardingComplete by remember { mutableStateOf(prefs.getBoolean("onboarding_done", false)) }
    var selectedTab by remember { mutableIntStateOf(0) }
    LaunchedEffect(connectCloudRequested) {
        if (connectCloudRequested) selectedTab = 2
    }

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
            // The bar is never gated on onboarding. Setup can stall for reasons
            // the app cannot fix from here - a keyboard the system reports as
            // not selected, a step that needs a trip through system settings -
            // and a user stuck there still has to be able to reach Settings and
            // configure a server. Hiding the only way out behind the thing that
            // is stuck is what made setup a dead end.
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
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            // Onboarding owns the home tab only. Every other tab stays
            // reachable while it runs, so a stalled setup can still be
            // worked around from Settings.
            if (!onboardingComplete && selectedTab == 0) {
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
                    2 -> SettingsTab(
                        companionProvisioner = companionProvisioner,
                        connectCloudRequested = connectCloudRequested,
                        onConnectSucceeded = {
                            onConnectCloudConsumed()
                            if (!onboardingComplete) selectedTab = 0
                        },
                        onConnectAbandoned = onConnectCloudConsumed,
                    )
                    3 -> DictationTestScreen(profileSource)
                    4 -> VoiceAgentTestScreen(profileSource)
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
                    stringResource(R.string.onboarding_welcome),
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    stringResource(R.string.onboarding_intro),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                Spacer(Modifier.height(8.dp))

                ModeCard(
                    title = stringResource(R.string.mode_dictate),
                    description = stringResource(R.string.onboarding_dictate_body),
                    icon = "Mic",
                    requirement = stringResource(R.string.onboarding_dictate_requirement),
                    isAvailable = true,
                )
                ModeCard(
                    title = stringResource(R.string.mode_assist),
                    description = stringResource(R.string.onboarding_assist_body),
                    icon = "Sparkle",
                    requirement = stringResource(R.string.onboarding_assist_requirement),
                    isAvailable = true,
                )
                ModeCard(
                    title = stringResource(R.string.mode_voice_agent),
                    description = stringResource(R.string.onboarding_voice_agent_body),
                    icon = "Waveform",
                    requirement = stringResource(R.string.onboarding_voice_agent_requirement),
                    isAvailable = true,
                )

                Spacer(Modifier.height(16.dp))

                Text(
                    stringResource(R.string.onboarding_modes_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                Button(
                    onClick = { step = 1 },
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.onboarding_setup_keyboard))
                }
            }
            1 -> {
                // Step 2: Keyboard Setup
                // Re-read on every resume, not once. These were remembered on
                // first composition, so selecting the keyboard in system
                // settings and coming back left the wizard looking at the state
                // from before the trip - the step could never complete, and
                // with the tab bar gated on onboarding that was a dead end.
                var isSelected by remember { mutableStateOf(KeyboardSetupChecker.isKeyboardSelected(context)) }
                val stepLifecycle = androidx.compose.ui.platform.LocalLifecycleOwner.current
                DisposableEffect(stepLifecycle) {
                    val observer = androidx.lifecycle.LifecycleEventObserver { _, event ->
                        if (event == androidx.lifecycle.Lifecycle.Event.ON_RESUME) {
                            isSelected = KeyboardSetupChecker.isKeyboardSelected(context)
                        }
                    }
                    stepLifecycle.lifecycle.addObserver(observer)
                    onDispose { stepLifecycle.lifecycle.removeObserver(observer) }
                }

                KeyboardOnboardingWizard(
                    isKeyboardEnabled = isKeyboardEnabled,
                    isKeyboardSelected = isSelected,
                    onComplete = onComplete,
                    onBack = { step = 0 },
                )
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
                            stringResource(R.string.onboarding_coming_soon),
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
        QuickActionRow("Voice Agent", "Auf der Tastatur, nicht als System-Assistent") { onOpenVoiceAgent() }
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
private fun SettingsTab(
    companionProvisioner: CompanionProvisioner,
    connectCloudRequested: Boolean = false,
    onConnectSucceeded: () -> Unit = {},
    onConnectAbandoned: () -> Unit = {},
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text("Einstellungen", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)

        ServerConnectionCard(
            provisioner = companionProvisioner,
            connectCloudRequested = connectCloudRequested,
            onConnectSucceeded = onConnectSucceeded,
            onConnectAbandoned = onConnectAbandoned,
        )

        VoiceAgentProviderCard()

        KeyboardIconsCard()

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

// --- Server connection ---

/**
 * Where the SpeechKit server is, and whether one is configured at all.
 *
 * This editor existed only inside the dictation test screen, so the single
 * setting that upgrades every mode from the on-device tier to a server was
 * reachable only from a developer tab. It writes the same store
 * [StoredServerProfile] reads, so saving here is what makes the keyboard's
 * server keys and the voice agent work at all.
 */
@Composable
private fun ServerConnectionCard(
    provisioner: CompanionProvisioner,
    connectCloudRequested: Boolean = false,
    onConnectSucceeded: () -> Unit = {},
    onConnectAbandoned: () -> Unit = {},
) {
    val context = LocalContext.current
    val prefs = remember {
        context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE)
    }
    val scope = rememberCoroutineScope()
    var modeTick by remember { mutableIntStateOf(0) }

    var url by remember {
        mutableStateOf(prefs.getString(StoredServerProfile.KEY_SERVER_URL, "") ?: "")
    }
    var token by remember {
        mutableStateOf(prefs.getString(StoredServerProfile.KEY_SERVER_TOKEN, "") ?: "")
    }
    var status by remember { mutableStateOf<String?>(null) }
    var testing by remember { mutableStateOf(false) }
    var finding by remember { mutableStateOf(false) }
    var lanServers by remember { mutableStateOf<List<LanServer>>(emptyList()) }
    val finder = remember { NsdLanFinder(context) }
    DisposableEffect(finder) {
        onDispose { finder.stop() }
    }
    val nearbyPermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        if (granted || Build.VERSION.SDK_INT < 33) {
            finding = true
            lanServers = emptyList()
            finder.start { lanServers = it }
        }
    }

    fun startLanFind() {
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.NEARBY_WIFI_DEVICES)
            != PackageManager.PERMISSION_GRANTED
        ) {
            nearbyPermission.launch(Manifest.permission.NEARBY_WIFI_DEVICES)
            return
        }
        finding = true
        lanServers = emptyList()
        finder.start { lanServers = it }
    }

    val saved = remember(url, token, modeTick) { StoredServerProfile.load(context) }
    val shippedUrl = remember { ShippedDefaults.serverUrl }
    val shippedProfile = remember { ShippedDefaults.shippedProfile() }
    val cloudConnectSupported = remember { ShippedDefaults.cloudConnectSupported }
    val mode = remember(modeTick, saved, shippedProfile) {
        StoredServerProfile.resolvedMode(context, saved, shippedProfile)
    }
    val cloudSession = remember(modeTick) { provisioner.currentSession() }

    fun persistMode(next: ConnectionMode) {
        StoredServerProfile.saveMode(context, next)
        modeTick += 1
    }

    fun openCompanion() {
        val launch = context.packageManager.getLaunchIntentForPackage(CoinstallContract.COMPANION_PACKAGE)
        if (launch != null) {
            context.startActivity(launch)
            return
        }
        context.startActivity(
            Intent(
                Intent.ACTION_VIEW,
                Uri.parse("market://details?id=${CoinstallContract.COMPANION_PACKAGE}"),
            ),
        )
    }

    fun connectCloud() {
        scope.launch {
            val outcome = withContext(Dispatchers.IO) { provisioner.provisionNow() }
            when (outcome) {
                is CompanionProvision.Session -> {
                    persistMode(ConnectionMode.KOMBIFY_CLOUD)
                    status = context.getString(R.string.settings_connection_cloud_connected)
                    onConnectSucceeded()
                }
                CompanionProvision.Empty -> {
                    status = context.getString(R.string.settings_connection_cloud_signed_out)
                    openCompanion()
                }
                CompanionProvision.Unavailable -> {
                    status = if (provisioner.isCompanionInstalled()) {
                        context.getString(R.string.settings_connection_cloud_failed)
                    } else {
                        context.getString(R.string.settings_connection_cloud_missing)
                    }
                    if (!provisioner.isCompanionInstalled()) onConnectAbandoned()
                }
            }
        }
    }

    LaunchedEffect(connectCloudRequested) {
        if (!connectCloudRequested) return@LaunchedEffect
        connectCloud()
    }
    val connectLifecycle = androidx.compose.ui.platform.LocalLifecycleOwner.current
    DisposableEffect(connectCloudRequested, connectLifecycle) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME && connectCloudRequested) {
                connectCloud()
            }
        }
        connectLifecycle.lifecycle.addObserver(observer)
        onDispose { connectLifecycle.lifecycle.removeObserver(observer) }
    }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                stringResource(R.string.settings_connection_title),
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                when (mode) {
                    ConnectionMode.KOMBIFY_CLOUD -> stringResource(R.string.settings_connection_mode_cloud)
                    ConnectionMode.SELF_HOST -> stringResource(R.string.settings_connection_mode_self_host)
                    ConnectionMode.SPEECHKIT_ORIGIN -> stringResource(R.string.settings_connection_mode_origin)
                    ConnectionMode.ON_DEVICE -> stringResource(R.string.settings_connection_mode_on_device)
                },
                style = MaterialTheme.typography.bodySmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                when {
                    mode == ConnectionMode.KOMBIFY_CLOUD && cloudSession != null ->
                        stringResource(R.string.settings_connection_cloud_connected)
                    mode == ConnectionMode.KOMBIFY_CLOUD ->
                        stringResource(R.string.settings_connection_cloud_signed_out)
                    saved != null -> stringResource(R.string.settings_connection_state_server)
                    shippedUrl != null && shippedProfile != null ->
                        stringResource(R.string.settings_connection_state_shipped, shippedUrl)
                    shippedUrl != null -> stringResource(R.string.settings_connection_state_on_device)
                    else -> stringResource(R.string.settings_connection_state_on_device)
                },
                style = MaterialTheme.typography.bodySmall,
            )
            if (cloudConnectSupported) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (mode == ConnectionMode.KOMBIFY_CLOUD) {
                        TextButton(
                            onClick = {
                                provisioner.forgetSession()
                                persistMode(fallbackModeAfterDisconnect(shippedProfile))
                            },
                        ) { Text(stringResource(R.string.settings_connection_disconnect_cloud)) }
                    } else {
                        Button(onClick = { connectCloud() }) {
                            Text(stringResource(R.string.settings_connection_connect_cloud))
                        }
                    }
                    TextButton(onClick = { openCompanion() }) {
                        Text(
                            stringResource(
                                if (provisioner.isCompanionInstalled()) {
                                    R.string.settings_connection_open_companion
                                } else {
                                    R.string.settings_connection_install_companion
                                },
                            ),
                        )
                    }
                }
            }
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text(stringResource(R.string.settings_connection_url)) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = token,
                onValueChange = { token = it },
                label = { Text(stringResource(R.string.settings_connection_token)) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedButton(
                onClick = {
                    if (finding) {
                        finder.stop()
                        finding = false
                    } else {
                        startLanFind()
                    }
                },
            ) {
                Text(
                    stringResource(
                        if (finding) R.string.settings_connection_stop_lan
                        else R.string.settings_connection_find_lan,
                    ),
                )
            }
            if (finding) {
                Text(
                    stringResource(R.string.settings_connection_finding_lan),
                    style = MaterialTheme.typography.bodySmall,
                )
                Row(
                    modifier = Modifier.horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    lanServers.forEach { server ->
                        FilterChip(
                            selected = url == server.url,
                            onClick = { url = server.url },
                            label = { Text(server.instanceName) },
                        )
                    }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(
                    onClick = {
                        prefs.edit()
                            .putString(StoredServerProfile.KEY_SERVER_URL, url.trim())
                            .putString(StoredServerProfile.KEY_SERVER_TOKEN, token.trim())
                            .apply()
                        persistMode(ConnectionMode.SELF_HOST)
                        status = context.getString(R.string.settings_connection_saved)
                    },
                ) { Text(stringResource(R.string.settings_connection_save)) }
                OutlinedButton(
                    enabled = !testing && url.isNotBlank(),
                    onClick = {
                        testing = true
                        status = context.getString(R.string.settings_connection_testing)
                        scope.launch {
                            // Opening a real session is the only honest test: a
                            // reachable host that rejects the token looks exactly
                            // like a working one until a session is minted.
                            val result = runCatching {
                                val session = DictationController(
                                    profile = ConnectionProfile.Server(
                                        url.trim(),
                                        token.trim().ifEmpty { null },
                                    ),
                                    context = context,
                                ).openSession()
                                runCatching { session.close() }
                            }
                            status = result.fold(
                                onSuccess = { context.getString(R.string.settings_connection_ok) },
                                onFailure = { failure ->
                                    context.getString(
                                        R.string.settings_connection_failed,
                                        failure.message ?: failure.javaClass.simpleName,
                                    )
                                },
                            )
                            testing = false
                        }
                    },
                ) { Text(stringResource(R.string.settings_connection_test)) }
                if (saved != null) {
                    TextButton(
                        onClick = {
                            prefs.edit()
                                .remove(StoredServerProfile.KEY_SERVER_URL)
                                .remove(StoredServerProfile.KEY_SERVER_TOKEN)
                                .apply()
                            url = ""
                            token = ""
                            persistMode(fallbackModeAfterDisconnect(shippedProfile))
                            status = context.getString(R.string.settings_connection_cleared)
                        },
                    ) { Text(stringResource(R.string.settings_connection_clear)) }
                }
            }
            status?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
        }
    }
}

// --- Keyboard icons ---

/**
 * Which glyph each SpeechKit toolbar key is drawn with.
 *
 * Only SpeechKit's own symbols are offered. Each mode has Default plus a
 * couple of glyphs that match that mode. Provider logos stay out: the
 * keyboard APK is GPL-3.0 as a whole.
 */
@Composable
private fun VoiceAgentProviderCard() {
    val context = LocalContext.current
    var selected by remember { mutableStateOf(KeyboardAgentPreferences.provider(context)) }
    val options = listOf(
        KeyboardAgentPreferences.PROVIDER_DEEPGRAM to "Deepgram",
        KeyboardAgentPreferences.PROVIDER_ASSEMBLYAI to "AssemblyAI",
        KeyboardAgentPreferences.PROVIDER_OPENAI to "GPT",
    )
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.settings_agent_title), style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
            Text(
                stringResource(R.string.settings_agent_subtitle),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(
                modifier = Modifier.horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                options.forEach { (value, label) ->
                    FilterChip(
                        selected = selected == value,
                        onClick = {
                            selected = value
                            KeyboardAgentPreferences.setProvider(context, value)
                        },
                        label = { Text(label) },
                    )
                }
            }
        }
    }
}

@Composable
private fun KeyboardIconsCard() {
    val context = LocalContext.current
    val actions = remember {
        listOf(
            "SPEECHKIT_DICTATE_DEVICE" to R.string.speechkit_action_icon_device,
            "SPEECHKIT_DICTATE_SERVER" to R.string.speechkit_action_icon_server,
            "SPEECHKIT_AGENT_DEEPGRAM" to R.string.speechkit_action_icon_deepgram,
            "SPEECHKIT_AGENT_ASSEMBLYAI" to R.string.speechkit_action_icon_assemblyai,
            "SPEECHKIT_AGENT_GPT" to R.string.speechkit_action_icon_gpt,
            "SPEECHKIT_COMPANION" to R.string.speechkit_action_icon_companion,
        )
    }

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                stringResource(R.string.settings_icons_title),
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                stringResource(R.string.settings_icons_subtitle),
                style = MaterialTheme.typography.bodySmall,
            )
            actions.forEach { (action, label) ->
                var chosen by remember {
                    mutableStateOf(KeyboardIconPreferences.choice(context, action))
                }
                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(stringResource(label), style = MaterialTheme.typography.bodyMedium)
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .horizontalScroll(rememberScrollState()),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        iconChoicesFor(action, chosen).forEach { option ->
                            FilterChip(
                                selected = option == chosen,
                                onClick = {
                                    chosen = option
                                    KeyboardIconPreferences.setChoice(context, action, option)
                                },
                                label = {
                                    if (option.drawable == null) {
                                        Text(stringResource(option.label))
                                    } else {
                                        Icon(
                                            painter = painterResource(option.drawable),
                                            contentDescription = stringResource(option.label),
                                            modifier = Modifier.size(18.dp),
                                        )
                                    }
                                },
                            )
                        }
                    }
                }
            }
        }
    }
}
