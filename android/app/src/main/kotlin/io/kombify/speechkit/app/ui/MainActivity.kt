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
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
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
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.R
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.net.SpeechKitServerApi
import io.kombify.speechkit.net.VoiceAgentCatalog
import io.kombify.speechkit.net.VoiceAgentProfile
import io.kombify.speechkit.net.testFirebaseLiveConnection
import io.kombify.speechkit.net.LanServer
import io.kombify.speechkit.net.NsdLanFinder
import io.kombify.speechkit.net.PairingRecords
import io.kombify.speechkit.net.StoredServerProfile
import io.kombify.speechkit.net.StoredConnectionSelection
import io.kombify.speechkit.net.testGeminiLiveConnection
import io.kombify.speechkit.domain.ExecutionMode
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.Job
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
import io.kombify.speechkit.app.companion.CompanionPackage
import io.kombify.speechkit.app.companion.CompanionProvision
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.app.companion.ConnectCloudIntent
import io.kombify.speechkit.app.companion.companionCloudProfile
import io.kombify.speechkit.app.companion.shouldStartConnectOnResume
import io.kombify.speechkit.net.KombifyCloudAuth
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import io.kombify.speechkit.coinstall.v1.CoinstallContract
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.log.VoiceLog
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
    private var connectionSettingsRequest by mutableIntStateOf(0)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (!intent.getBooleanExtra(EXTRA_OPEN_CONNECTION, false)) requestMicPermissionIfNeeded()
        pendingConnectCloud = ConnectCloudIntent.matches(intent)
        if (intent.getBooleanExtra(EXTRA_OPEN_CONNECTION, false)) connectionSettingsRequest++
        setContent {
            SpeechKitTheme {
                SpeechKitApp(
                    companionProvisioner = companionProvisioner,
                    profileSource = profileSource,
                    connectCloudRequested = pendingConnectCloud,
                    onConnectCloudConsumed = { pendingConnectCloud = false },
                    connectionSettingsRequest = connectionSettingsRequest,
                )
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        if (intent.getBooleanExtra(EXTRA_OPEN_CONNECTION, false)) connectionSettingsRequest++
        if (ConnectCloudIntent.matches(intent)) {
            pendingConnectCloud = true
        }
    }

    companion object {
        const val EXTRA_OPEN_CONNECTION = "io.kombify.speechkit.OPEN_CONNECTION"
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
    connectionSettingsRequest: Int = 0,
) {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("speechkit_app", Context.MODE_PRIVATE) }
    var onboardingComplete by remember { mutableStateOf(prefs.getBoolean("onboarding_done", false)) }
    var selectedTab by remember { mutableIntStateOf(0) }
    LaunchedEffect(connectCloudRequested) {
        if (connectCloudRequested) selectedTab = 2
    }
    LaunchedEffect(connectionSettingsRequest) {
        if (connectionSettingsRequest > 0) selectedTab = 2
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
                title = { Text(stringResource(R.string.app_name), fontWeight = FontWeight.Bold) },
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
                    label = { Text(stringResource(R.string.nav_home)) },
                    icon = {},
                )
                NavigationBarItem(
                    selected = selectedTab == 1,
                    onClick = { selectedTab = 1 },
                    label = { Text(stringResource(R.string.nav_library)) },
                    icon = {},
                )
                NavigationBarItem(
                    selected = selectedTab == 2,
                    onClick = { selectedTab = 2 },
                    label = { Text(stringResource(R.string.nav_settings)) },
                    icon = {},
                )
                if (ShippedDefaults.showLabTabs) {
                    NavigationBarItem(
                        selected = selectedTab == 3,
                        onClick = { selectedTab = 3 },
                        label = { Text(stringResource(R.string.nav_dev)) },
                        icon = {},
                    )
                    NavigationBarItem(
                        selected = selectedTab == 4,
                        onClick = { selectedTab = 4 },
                        label = { Text(stringResource(R.string.nav_voice)) },
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
        Text(stringResource(R.string.home_title), style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)

        // Status Cards
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            StatCard(stringResource(R.string.home_stat_keyboard), stringResource(R.string.home_stat_keyboard_active), Modifier.weight(1f))
            StatCard(stringResource(R.string.home_stat_mode), stringResource(R.string.mode_dictate), Modifier.weight(1f))
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            StatCard(stringResource(R.string.home_stat_transcripts), "0", Modifier.weight(1f))
            StatCard(stringResource(R.string.home_stat_assist), "0", Modifier.weight(1f))
        }

        Spacer(Modifier.height(8.dp))

        // Quick Actions
        Text(stringResource(R.string.home_quick_actions), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Medium)

        QuickActionRow(stringResource(R.string.mode_dictate), stringResource(R.string.home_dictate_desc)) {}
        QuickActionRow(stringResource(R.string.mode_assist), stringResource(R.string.home_assist_desc)) {}
        QuickActionRow(stringResource(R.string.mode_voice_agent), stringResource(R.string.home_voice_agent_desc)) { onOpenVoiceAgent() }
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
        Text(stringResource(R.string.nav_library), style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
        Text(
            stringResource(R.string.library_intro),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        // Tabs for different content types
        var libraryTab by remember { mutableIntStateOf(0) }
        TabRow(selectedTabIndex = libraryTab) {
            Tab(selected = libraryTab == 0, onClick = { libraryTab = 0 }) { Text(stringResource(R.string.library_tab_transcripts), Modifier.padding(12.dp)) }
            Tab(selected = libraryTab == 1, onClick = { libraryTab = 1 }) { Text(stringResource(R.string.library_tab_quick_notes), Modifier.padding(12.dp)) }
            Tab(selected = libraryTab == 2, onClick = { libraryTab = 2 }) { Text(stringResource(R.string.mode_assist), Modifier.padding(12.dp)) }
        }

        Text(
            stringResource(R.string.library_empty),
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

/** Whether `io.kombify.companion` has a public Play listing; false routes the install button to the tester page. */
private const val COMPANION_PLAY_LISTED = false
private const val COMPANION_TESTER_URL = "https://appdistribution.firebase.google.com/testerapps"

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
        Text(stringResource(R.string.nav_settings), style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)

        var modeVersion by remember { mutableIntStateOf(0) }
        ServerConnectionCard(
            provisioner = companionProvisioner,
            modeVersion = modeVersion,
            connectCloudRequested = connectCloudRequested,
            onConnectSucceeded = onConnectSucceeded,
            onConnectAbandoned = onConnectAbandoned,
        )

        if (io.kombify.speechkit.BuildConfig.HYBRID_ENABLED) {
            val hybridContext = LocalContext.current
            Button(onClick = { io.kombify.speechkit.app.hybrid.HybridActivity.open(hybridContext) }) {
                Text(stringResource(R.string.hybrid_title))
            }
        }
        OnDeviceModelCard(onModeChanged = { modeVersion += 1 })

        NeuralVoiceCard()

        VocabularyCard()


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
                Text(stringResource(R.string.settings_keyboard_title), style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(4.dp))
                Text(
                    stringResource(
                        if (enabled) R.string.settings_keyboard_enabled
                        else R.string.settings_keyboard_disabled,
                    ),
                    style = MaterialTheme.typography.bodySmall,
                    color = if (enabled) MaterialTheme.colorScheme.primary
                    else MaterialTheme.colorScheme.error,
                )
            }
        }

        // About
        Card(modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.padding(16.dp)) {
                Text(stringResource(R.string.settings_about), style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(4.dp))
                Text(
                    stringResource(R.string.app_name) + " v" + BuildConfig.VERSION_NAME,
                    style = MaterialTheme.typography.bodySmall,
                )
                Text(stringResource(R.string.settings_about_tagline), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
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
    modeVersion: Int = 0,
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
    // Resolved off the main thread and again on every resume (modeTick), so an install or update of the
    // Companion shows up without leaving Settings; null until the first answer.
    var companionInstalled by remember { mutableStateOf<Boolean?>(null) }
    LaunchedEffect(modeVersion) { if (modeVersion > 0) modeTick += 1 }
    LaunchedEffect(modeTick) {
        companionInstalled = withContext(Dispatchers.IO) { provisioner.isCompanionInstalled() }
    }

    var url by remember {
        mutableStateOf(prefs.getString(StoredServerProfile.KEY_SERVER_URL, "") ?: "")
    }
    var token by remember { mutableStateOf("") }
    var apiKey by remember { mutableStateOf("") }
    var model by remember { mutableStateOf(KeyboardAgentPreferences.model(context).ifBlank { "gemini-3.8-live" }) }
    var voice by remember { mutableStateOf("Kore") }
    var remoteKind by remember { mutableIntStateOf(-1) }
    val bundledProfiles = remember { VoiceAgentCatalog.bundled().filter { it.isNativeRealtimeEndpoint } }
    var selectedProfileId by remember {
        mutableStateOf(KeyboardAgentPreferences.profileId(context).takeIf { id -> bundledProfiles.any { it.id == id } }
            ?: bundledProfiles.firstOrNull { it.adapterKind == "gemini_live" }?.id.orEmpty())
    }
    var serverProfiles by remember { mutableStateOf<List<VoiceAgentProfile>>(emptyList()) }
    var readyProfileIds by remember { mutableStateOf<Set<String>>(emptySet()) }
    val selection = remember { StoredConnectionSelection(context) }
    var choice by remember { mutableIntStateOf(-1) }
    var generation by remember { mutableIntStateOf(0) }
    var pairingPayload by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<String?>(null) }
    var connecting by remember { mutableStateOf(false) }
    var connectJob by remember { mutableStateOf<Job?>(null) }
    var showSelfHost by remember { mutableStateOf(!ShippedDefaults.cloudConnectSupported) }
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
    val snapshot = remember(modeTick, saved, shippedProfile) { selection.snapshot(shippedProfile) }
    val mode = snapshot.mode
    val byokProfile = snapshot.profile as? ConnectionProfile.Byok
    var restoredByokDetails by remember { mutableStateOf(false) }
    if (!restoredByokDetails && byokProfile != null) {
        model = byokProfile.model
        voice = byokProfile.voice
        restoredByokDetails = true
    }
    val firebaseConfigured = BuildConfig.HYBRID_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED &&
        BuildConfig.FIREBASE_APPLICATION_ID.isNotBlank() && BuildConfig.FIREBASE_PROJECT_ID.isNotBlank() && BuildConfig.FIREBASE_API_KEY.isNotBlank()
    if (remoteKind < 0) remoteKind = when (mode) {
        ConnectionMode.SELF_HOST, ConnectionMode.SPEECHKIT_ORIGIN -> 1
        ConnectionMode.FIREBASE -> 2
        ConnectionMode.BYOK -> 0
        else -> if (firebaseConfigured) 2 else 0
    }
    val selectedProfile = (serverProfiles + bundledProfiles).firstOrNull { it.id == selectedProfileId }
    LaunchedEffect(modeTick, mode, snapshot.revision) {
        serverProfiles = emptyList()
        readyProfileIds = emptySet()
        val active = snapshot.profile as? ConnectionProfile.Server
        if (active != null && (mode == ConnectionMode.SELF_HOST || mode == ConnectionMode.KOMBIFY_CLOUD || mode == ConnectionMode.SPEECHKIT_ORIGIN) && active.credentialAvailable && snapshot.policy.allowNetwork) {
            try {
                val api = SpeechKitServerApi(active, context = context)
                val profiles = api.voiceAgentProfiles()
                val readiness = api.voiceAgentReadiness()
                serverProfiles = VoiceAgentCatalog.eligibleEndpointProfiles(profiles, readiness)
                readyProfileIds = serverProfiles.map { it.id }.toSet()
            } catch (e: CancellationException) { throw e }
              catch (_: Exception) { /* An unavailable endpoint is not ready. */ }
        }
    }
    if (choice < 0) choice = when (mode) {
        ConnectionMode.ON_DEVICE -> 0
        ConnectionMode.KOMBIFY_CLOUD -> 2
        else -> 1
    }
    val cloudSession = remember(modeTick) {
        StoredServerProfile.loadCloud(context)
    }

    suspend fun saveSelection(action: suspend () -> Unit): Boolean = try {
        action()
        modeTick += 1
        status = context.getString(R.string.settings_connection_saved)
        true
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        status = context.getString(R.string.connection_setup_save_failed)
        false
    }

    fun persistKnownReadyProfile(profile: VoiceAgentProfile, chosenModel: String) {
        if (snapshot.mode != ConnectionMode.KOMBIFY_CLOUD) return
        val activeServer = snapshot.profile as? ConnectionProfile.Server
        val admitted = serverProfiles.firstOrNull { it.id == profile.id &&
            (it.modelId == chosenModel || it.variants.any { variant -> variant.modelId == chosenModel }) }
        if (admitted != null && activeServer != null && selection.isCurrent(activeServer)) KeyboardAgentPreferences.setProfile(context, admitted, chosenModel, activeServer)
        else status = context.getString(R.string.connection_setup_not_ready)
    }

    suspend fun persistReadyEndpointProfile(profile: VoiceAgentProfile?, chosenModel: String, endpoint: String) {
        if (profile == null) return
        try {
            val active = selection.snapshot(shippedProfile)
            val server = active.profile as? ConnectionProfile.Server ?: return
            if (active.mode != ConnectionMode.SELF_HOST || server.normalizedBaseUrl != endpoint.trim().trimEnd('/')) return
            val api = SpeechKitServerApi(server, context = context)
            val available = VoiceAgentCatalog.eligibleEndpointProfiles(api.voiceAgentProfiles(), api.voiceAgentReadiness())
            val admitted = available.firstOrNull { it.id == profile.id &&
                (it.modelId == chosenModel || it.variants.any { variant -> variant.modelId == chosenModel }) }
            if (admitted != null && selection.isCurrent(server)) KeyboardAgentPreferences.setProfile(context, admitted, chosenModel, server)
            else status = context.getString(R.string.connection_setup_not_ready)
        } catch (e: CancellationException) { throw e }
          catch (_: Exception) { status = context.getString(R.string.connection_setup_not_ready) }
    }

    suspend fun persistMode(next: ConnectionMode) {
        selection.selectMode(next)
        modeTick += 1
    }

    fun openCompanion() {
        val installed = CompanionPackage.resolve(context.packageManager, ownPackage = context.packageName)
        val launch = installed?.let(context.packageManager::getLaunchIntentForPackage)
        if (launch != null) {
            context.startActivity(launch)
            return
        }
        // Until the Play listing exists the store only says "not found": the invited-tester page is the one
        // working route (same pattern as the Companion's own SpeechKit install). Flip COMPANION_PLAY_LISTED
        // once `io.kombify.companion` is public on Play.
        val targets = if (COMPANION_PLAY_LISTED) {
            listOf("market://details?id=${CoinstallContract.COMPANION_RELEASE_PACKAGE}", COMPANION_TESTER_URL)
        } else {
            listOf(COMPANION_TESTER_URL)
        }
        val opened = targets.any { target ->
            runCatching {
                context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(target)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            }.isSuccess
        }
        if (!opened) VoiceLog.w(VoiceLog.NET, "no handler for the Companion install link")
    }

    fun connectCloud() {
        if (connecting) return
        val requestGeneration = generation
        val revision = selection.revision()
        connecting = true
        status = context.getString(R.string.settings_connection_testing)
        connectJob = scope.launch {
            if (StoredServerProfile.hasOwnCloudSession(context)) {
                if (requestGeneration == generation && selection.selectCloud(revision)) {
                    modeTick += 1
                    onConnectSucceeded()
                    status = null
                }
                connecting = false
                return@launch
            }
            val outcome = try {
                withContext(Dispatchers.IO) { provisioner.provisionNow() }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                CompanionProvision.Unavailable
            }
            val fromCompanion = companionCloudProfile(outcome)
            if (fromCompanion != null) {
                val connected = requestGeneration == generation && selection.connectCompanionCloud(
                    fromCompanion.baseUrl, fromCompanion.bearerToken.orEmpty(), expectedRevision = revision,
                )
                if (connected) {
                    modeTick += 1
                    onConnectSucceeded()
                }
                // The state line above already says Connected; a stale success
                // message here would outlive a later sign-out.
                status = null
                connecting = false
                return@launch
            }
            val clientId = ShippedDefaults.cloudAuthClientId
            if (clientId.isEmpty()) {
                status = context.getString(R.string.settings_connection_cloud_no_client)
                connecting = false
                return@launch
            }
            val auth = KombifyCloudAuth(KombifyCloudAuth.Config(clientId = clientId))
            val started = try {
                auth.start()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                null
            }
            if (started == null || started.verificationUriComplete.isEmpty()) {
                status = context.getString(R.string.settings_connection_cloud_network)
                connecting = false
                return@launch
            }
            // The code stays on screen while the browser tab is open: it is what
            // the user compares against the page kombify shows.
            status = if (started.userCode.isNotEmpty()) {
                context.getString(R.string.settings_connection_cloud_code, started.userCode)
            } else {
                context.getString(R.string.settings_connection_cloud_waiting)
            }
            runCatching {
                context.startActivity(
                    Intent(Intent.ACTION_VIEW, Uri.parse(started.verificationUriComplete))
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
            }
            val signIn = try {
                auth.awaitSignIn(started) { delay(it) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                KombifyCloudAuth.SignIn.Unreachable
            }
            status = when (signIn) {
                is KombifyCloudAuth.SignIn.Done -> {
                    val tokens = signIn.tokens
                    if (requestGeneration == generation && selection.connectOwnedCloud(
                        KombifyCloudAuth.DEFAULT_SERVER_URL,
                        tokens.accessToken,
                        tokens.refreshToken,
                        System.currentTimeMillis() + tokens.expiresInSec * 1000L,
                        expectedRevision = revision,
                    )) {
                        modeTick += 1
                        onConnectSucceeded()
                    }
                    null
                }
                KombifyCloudAuth.SignIn.Declined ->
                    context.getString(R.string.settings_connection_cloud_declined)
                KombifyCloudAuth.SignIn.Expired ->
                    context.getString(R.string.settings_connection_cloud_expired)
                KombifyCloudAuth.SignIn.Unreachable ->
                    context.getString(R.string.settings_connection_cloud_network)
            }
            connecting = false
        }
    }

    LaunchedEffect(connectCloudRequested) {
        if (!connectCloudRequested) return@LaunchedEffect
        if (BuildConfig.CONNECTION_SETUP_ENABLED) choice = 2
        connectCloud()
    }
    DisposableEffect(Unit) { onDispose { connectJob?.cancel() } }
    val connectLifecycle = androidx.compose.ui.platform.LocalLifecycleOwner.current
    DisposableEffect(connectCloudRequested, connectLifecycle) {
        val observer = LifecycleEventObserver { _, event ->
            // The sign-in can end while the app is in the background (refresh
            // token refused): re-read it so Settings never claims Connected.
            if (event == Lifecycle.Event.ON_RESUME) modeTick += 1
            if (event == Lifecycle.Event.ON_RESUME &&
                shouldStartConnectOnResume(
                    requested = connectCloudRequested,
                    alreadyConnected = mode == ConnectionMode.KOMBIFY_CLOUD && cloudSession != null,
                    connecting = connecting,
                )
            ) {
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
            if (BuildConfig.CONNECTION_SETUP_ENABLED) {
                listOf(R.string.connection_setup_local, R.string.connection_setup_own, R.string.connection_setup_account).forEachIndexed { index, label ->
                    FilterChip(selected = choice == index, onClick = {
                        if (choice != index) {
                            generation += 1
                            connectJob?.cancel()
                            connecting = false
                            choice = index
                            status = null
                            if (index != 2) scope.launch { selection.selectLocal(); modeTick += 1 }
                        } else if (index == 0) {
                            scope.launch { selection.selectLocal(); modeTick += 1 }
                        }
                    }, label = { Text(stringResource(label)) })
                }
                if (choice == 0) Text(stringResource(R.string.connection_setup_local_detail), style = MaterialTheme.typography.bodySmall)
            }
            Text(
                when (mode) {
                    ConnectionMode.KOMBIFY_CLOUD -> stringResource(R.string.settings_connection_mode_cloud)
                    ConnectionMode.SELF_HOST -> stringResource(R.string.settings_connection_mode_self_host)
                    ConnectionMode.SPEECHKIT_ORIGIN -> stringResource(R.string.settings_connection_mode_origin)
                    ConnectionMode.ON_DEVICE -> stringResource(R.string.settings_connection_mode_on_device)
                    ConnectionMode.BYOK -> stringResource(R.string.connection_setup_gemini)
                    ConnectionMode.FIREBASE -> stringResource(R.string.connection_setup_firebase)
                },
                style = MaterialTheme.typography.bodySmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                when {
                    mode == ConnectionMode.KOMBIFY_CLOUD && cloudSession != null ->
                        stringResource(R.string.settings_connection_cloud_connected)
                    mode == ConnectionMode.KOMBIFY_CLOUD ->
                        stringResource(R.string.settings_connection_cloud_session_ended)
                    mode == ConnectionMode.BYOK && byokProfile?.hasUsableKey == true -> stringResource(R.string.connection_setup_key_saved)
                    mode == ConnectionMode.BYOK -> stringResource(R.string.connection_setup_key_required)
                    mode == ConnectionMode.FIREBASE -> stringResource(R.string.connection_setup_firebase)
                    mode == ConnectionMode.SELF_HOST && saved != null -> stringResource(R.string.settings_connection_state_server)
                    mode == ConnectionMode.SPEECHKIT_ORIGIN && shippedUrl != null && shippedProfile != null ->
                        stringResource(R.string.settings_connection_state_shipped, shippedUrl)
                    shippedUrl != null -> stringResource(R.string.settings_connection_state_on_device)
                    else -> stringResource(R.string.settings_connection_state_on_device)
                },
                style = MaterialTheme.typography.bodySmall,
            )
            if ((!BuildConfig.CONNECTION_SETUP_ENABLED || choice == 2) && mode == ConnectionMode.KOMBIFY_CLOUD && cloudSession != null) {
                CloudAccountSection(
                    provisioner = provisioner,
                    onSessionChanged = { modeTick += 1 },
                )
            }
            if ((!BuildConfig.CONNECTION_SETUP_ENABLED && cloudConnectSupported) || (BuildConfig.CONNECTION_SETUP_ENABLED && choice == 2)) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                    if (mode == ConnectionMode.KOMBIFY_CLOUD && cloudSession != null) {
                        TextButton(
                            onClick = {
                                connecting = true
                                scope.launch {
                                    try {
                                        io.kombify.speechkit.app.hybrid.HybridRuntime.cancelActive()
                                        provisioner.forgetSession()
                                        StoredServerProfile.clearCloud(context)
                                        persistMode(fallbackModeAfterDisconnect(shippedProfile))
                                    } finally { connecting = false }
                                }
                            },
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text(stringResource(R.string.settings_connection_disconnect_cloud)) }
                    } else {
                        Button(
                            onClick = { connectCloud() },
                            enabled = !connecting,
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Text(
                                stringResource(
                                    if (connecting) R.string.settings_connection_testing
                                    else R.string.settings_connection_connect_cloud,
                                ),
                            )
                        }
                    }
                    TextButton(
                        onClick = { openCompanion() },
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text(
                            stringResource(
                                if (companionInstalled != false) {
                                    R.string.settings_connection_open_companion
                                } else {
                                    R.string.settings_connection_install_companion
                                },
                            ),
                        )
                    }
                    status?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                }
            }
            if (BuildConfig.CONNECTION_SETUP_ENABLED && choice == 2) {
                val profiles = (bundledProfiles + serverProfiles).distinctBy { it.id }
                SetupDropdown(label = stringResource(R.string.connection_setup_profile), selected = selectedProfileId,
                    options = profiles.map { it.id to it.name }, onSelect = { id ->
                        selectedProfileId = id
                        val profile = profiles.first { it.id == id }
                        model = profile.variants.firstOrNull { it.recommended }?.modelId
                            ?: profile.variants.firstOrNull()?.modelId ?: profile.modelId.orEmpty()
                        persistKnownReadyProfile(profile, model)
                    })
                selectedProfile?.let { profile ->
                    val variants = profile.variants.map { it.modelId to it.name }.ifEmpty { listOf(profile.modelId.orEmpty() to profile.modelId.orEmpty()) }
                    if (model !in variants.map { it.first }) model = variants.first().first
                    SetupDropdown(label = stringResource(R.string.connection_setup_variant), selected = model,
                        options = variants, onSelect = { model = it; persistKnownReadyProfile(profile, it) })
                    if (mode == ConnectionMode.KOMBIFY_CLOUD && cloudSession != null) {
                        Text(stringResource(if (profile.id in readyProfileIds) R.string.connection_setup_available else R.string.connection_setup_not_ready), style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
            if (BuildConfig.CONNECTION_SETUP_ENABLED && choice == 1) {
                val profiles = (bundledProfiles + serverProfiles).distinctBy { it.id }
                SetupDropdown(
                    label = stringResource(R.string.connection_setup_profile),
                    selected = selectedProfileId,
                    options = profiles.map { it.id to it.name },
                    onSelect = { id ->
                        selectedProfileId = id
                        val profile = profiles.first { it.id == id }
                        model = profile.variants.firstOrNull { it.recommended }?.modelId
                            ?: profile.variants.firstOrNull()?.modelId ?: profile.modelId.orEmpty()
                        remoteKind = if (profile.adapterKind == "gemini_live") { if (firebaseConfigured) 2 else 0 } else 1
                        status = null
                    },
                )
                selectedProfile?.let { profile ->
                    val variants = profile.variants.map { it.modelId to it.name }.ifEmpty {
                        listOf(profile.modelId.orEmpty() to profile.modelId.orEmpty())
                    }
                    if (model !in variants.map { it.first }) model = variants.first().first
                    SetupDropdown(
                        label = stringResource(R.string.connection_setup_variant),
                        selected = model,
                        options = variants,
                        onSelect = { variant -> model = variant },
                    )
                    val methods = buildList {
                        if (profile.adapterKind == "gemini_live") {
                            add(2 to context.getString(R.string.connection_setup_firebase))
                            add(0 to context.getString(R.string.connection_setup_own_key))
                        }
                        add(1 to context.getString(R.string.connection_setup_server_method))
                    }
                    if (methods.none { it.first == remoteKind }) remoteKind = 1
                    SetupDropdown(label = stringResource(R.string.connection_setup_method), selected = remoteKind,
                        options = methods, onSelect = { remoteKind = it; status = null })
                    if (remoteKind == 1 && mode == ConnectionMode.SELF_HOST) {
                        Text(stringResource(if (profile.id in readyProfileIds) R.string.connection_setup_available else R.string.connection_setup_not_ready), style = MaterialTheme.typography.bodySmall)
                    }
                }
                if (remoteKind == 0) {
                    Text(stringResource(R.string.connection_setup_gemini_detail), style = MaterialTheme.typography.bodySmall)
                    OutlinedTextField(value = apiKey, onValueChange = { apiKey = it }, label = { Text(stringResource(R.string.connection_setup_key)) }, visualTransformation = PasswordVisualTransformation(), singleLine = true, modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(value = voice, onValueChange = { voice = it }, label = { Text(stringResource(R.string.connection_setup_voice)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    if (mode != ConnectionMode.BYOK && selection.hasSavedByok()) {
                        OutlinedButton(onClick = { scope.launch {
                            if (saveSelection { check(selection.restoreByok()) }) {
                                val restored = selection.snapshot(shippedProfile).profile as? ConnectionProfile.Byok
                                val catalogProfile = bundledProfiles.firstOrNull { it.adapterKind == "gemini_live" &&
                                    restored != null && (it.modelId == restored.model || it.variants.any { variant -> variant.modelId == restored.model }) }
                                if (restored != null && catalogProfile != null) {
                                    selectedProfileId = catalogProfile.id
                                    model = restored.model
                                    voice = restored.voice
                                    KeyboardAgentPreferences.setProfile(context, catalogProfile, restored.model, restored)
                                } else status = context.getString(R.string.connection_setup_not_ready)
                            }
                        } }) {
                            Text(stringResource(R.string.connection_setup_use_saved))
                        }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(enabled = apiKey.isNotBlank() && model.isNotBlank() && voice.isNotBlank(), onClick = {
                            val replacement = apiKey
                            val selectedModel = model.trim()
                            val selectedVoice = voice.trim()
                            val chosenProfile = selectedProfile
                            scope.launch {
                                if (saveSelection { selection.selectByok(replacement, selectedModel, selectedVoice) }) {
                                    val active = selection.snapshot(shippedProfile).profile as? ConnectionProfile.Byok
                                    if (active != null && active.model == selectedModel && active.voice == selectedVoice && active.apiKey == replacement.trim()) {
                                        chosenProfile?.let { KeyboardAgentPreferences.setProfile(context, it, selectedModel, active) }
                                    }
                                    apiKey = ""
                                }
                            }
                        }) { Text(stringResource(R.string.settings_connection_save)) }
                        OutlinedButton(enabled = !testing && mode == ConnectionMode.BYOK && byokProfile?.hasUsableKey == true, onClick = {
                            val profile = byokProfile ?: return@OutlinedButton
                            testing = true
                            scope.launch {
                                status = try { testGeminiLiveConnection(profile, context); context.getString(R.string.settings_connection_ok) }
                                    catch (e: CancellationException) { throw e }
                                    catch (e: Exception) { context.getString(R.string.connection_setup_test_failed) }
                                testing = false
                            }
                        }) { Text(stringResource(R.string.settings_connection_test)) }
                    }
                    status?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                } else if (remoteKind == 2) {
                    val configured = firebaseConfigured
                    if (!configured) Text(stringResource(R.string.connection_setup_firebase_unavailable), style = MaterialTheme.typography.bodySmall)
                    OutlinedTextField(value = voice, onValueChange = { voice = it }, label = { Text(stringResource(R.string.connection_setup_voice)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    Button(enabled = configured && model.isNotBlank() && voice.isNotBlank(), onClick = {
                        val firebase = ConnectionProfile.Firebase(
                            applicationId = BuildConfig.FIREBASE_APPLICATION_ID,
                            projectId = BuildConfig.FIREBASE_PROJECT_ID,
                            apiKey = BuildConfig.FIREBASE_API_KEY,
                            cloudModel = BuildConfig.FIREBASE_CLOUD_MODEL.ifBlank { "gemini-3.5-flash-lite" },
                            liveModel = model, voice = voice.trim(), debugAppCheck = BuildConfig.DEBUG,
                        )
                        val chosenProfile = selectedProfile
                        scope.launch {
                            if (saveSelection { selection.selectFirebase(firebase) }) {
                                val active = selection.snapshot(shippedProfile).profile as? ConnectionProfile.Firebase
                                if (active != null && active.copy(selectionRevision = null) == firebase) {
                                    chosenProfile?.let { KeyboardAgentPreferences.setProfile(context, it, firebase.liveModel, active) }
                                }
                            }
                        }
                    }) { Text(stringResource(R.string.connection_setup_firebase_activate)) }
                    val firebaseProfile = snapshot.profile as? ConnectionProfile.Firebase
                    OutlinedButton(enabled = !testing && mode == ConnectionMode.FIREBASE && firebaseProfile != null, onClick = {
                        val active = firebaseProfile ?: return@OutlinedButton
                        testing = true
                        scope.launch {
                            status = try { testFirebaseLiveConnection(active, context); context.getString(R.string.settings_connection_ok) }
                                catch (e: CancellationException) { throw e }
                                catch (_: Exception) { context.getString(R.string.connection_setup_test_failed) }
                            testing = false
                        }
                    }) { Text(stringResource(R.string.settings_connection_test)) }
                    status?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                } else Text(stringResource(R.string.connection_setup_server_detail), style = MaterialTheme.typography.bodySmall)
            }
            if (!BuildConfig.CONNECTION_SETUP_ENABLED) TextButton(onClick = { showSelfHost = !showSelfHost }) {
                Text(
                    stringResource(
                        if (showSelfHost) R.string.settings_connection_self_host_hide
                        else R.string.settings_connection_self_host_show,
                    ),
                )
            }
            if ((!BuildConfig.CONNECTION_SETUP_ENABLED && showSelfHost) || (BuildConfig.CONNECTION_SETUP_ENABLED && choice == 1 && remoteKind == 1)) {
            if (!BuildConfig.CONNECTION_SETUP_ENABLED) {
                val profiles = (bundledProfiles + serverProfiles).distinctBy { it.id }
                SetupDropdown(label = stringResource(R.string.connection_setup_profile), selected = selectedProfileId,
                    options = profiles.map { it.id to it.name }, onSelect = { id ->
                        selectedProfileId = id
                        val profile = profiles.first { it.id == id }
                        model = profile.variants.firstOrNull { it.recommended }?.modelId
                            ?: profile.variants.firstOrNull()?.modelId ?: profile.modelId.orEmpty()
                    })
                selectedProfile?.let { profile ->
                    val variants = profile.variants.map { it.modelId to it.name }.ifEmpty { listOf(profile.modelId.orEmpty() to profile.modelId.orEmpty()) }
                    if (model !in variants.map { it.first }) model = variants.first().first
                    SetupDropdown(label = stringResource(R.string.connection_setup_variant), selected = model,
                        options = variants, onSelect = { model = it })
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
                visualTransformation = PasswordVisualTransformation(),
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            if (saved?.credentialAvailable == false) Text(stringResource(R.string.connection_setup_server_key_required), style = MaterialTheme.typography.bodySmall)
            else if (!saved?.bearerToken.isNullOrBlank()) Text(stringResource(R.string.connection_setup_key_saved), style = MaterialTheme.typography.bodySmall)
            OutlinedTextField(
                value = pairingPayload,
                onValueChange = { pairingPayload = it },
                label = { Text(stringResource(R.string.settings_connection_pairing)) },
                visualTransformation = PasswordVisualTransformation(),
                modifier = Modifier.fillMaxWidth(),
                minLines = 2,
            )
            OutlinedButton(
                onClick = {
                    val selected = PairingRecords.consume(pairingPayload)
                    if (selected == null) {
                        status = context.getString(R.string.settings_connection_pairing_invalid)
                        return@OutlinedButton
                    }
                    val chosenProfile = selectedProfile
                    val chosenModel = model
                    scope.launch {
                        if (saveSelection { selection.selectServer(selected.profile.baseUrl, selected.profile.bearerToken) }) {
                            persistReadyEndpointProfile(chosenProfile, chosenModel, selected.profile.baseUrl)
                            url = selected.profile.baseUrl
                            token = ""
                            pairingPayload = ""
                        }
                    }
                },
                enabled = pairingPayload.isNotBlank(),
            ) {
                Text(stringResource(R.string.settings_connection_pairing_apply))
            }
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
                            onClick = {
                                val sameEndpoint = saved?.normalizedBaseUrl == server.url.trim().trimEnd('/')
                                val replacement = token.trim().ifEmpty { saved?.bearerToken?.takeIf { sameEndpoint } }
                                if (sameEndpoint && saved?.credentialAvailable == false && replacement.isNullOrEmpty()) {
                                    status = context.getString(R.string.connection_setup_server_key_required)
                                } else {
                                    val chosenProfile = selectedProfile
                                    val chosenModel = model
                                    scope.launch {
                                    if (saveSelection { selection.selectServer(server.url, replacement) }) {
                                        persistReadyEndpointProfile(chosenProfile, chosenModel, server.url)
                                        url = server.url
                                        token = ""
                                    }
                                    }
                                }
                            },
                            label = { Text(server.instanceName) },
                        )
                    }
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(
                    onClick = {
                        val endpoint = url.trim()
                        val sameEndpoint = saved?.normalizedBaseUrl == endpoint.trimEnd('/')
                        val replacement = token.trim().ifEmpty { saved?.bearerToken?.takeIf { sameEndpoint } }
                        if (sameEndpoint && saved?.credentialAvailable == false && replacement.isNullOrEmpty()) {
                            status = context.getString(R.string.connection_setup_server_key_required)
                        } else {
                            val chosenProfile = selectedProfile
                            val chosenModel = model
                            scope.launch {
                                if (saveSelection { selection.selectServer(endpoint, replacement) }) {
                                    persistReadyEndpointProfile(chosenProfile, chosenModel, endpoint)
                                    token = ""
                                }
                            }
                        }
                    },
                ) { Text(stringResource(R.string.settings_connection_save)) }
                OutlinedButton(
                    enabled = !testing && url.isNotBlank(),
                    onClick = {
                        testing = true
                        status = context.getString(R.string.settings_connection_testing)
                        scope.launch {
                            val active = selection.snapshot(shippedProfile)
                            val profile = active.profile as? ConnectionProfile.Server
                            status = try {
                                if (active.mode != ConnectionMode.SELF_HOST || profile == null ||
                                    profile.normalizedBaseUrl != url.trim().trimEnd('/') || token.isNotBlank()) {
                                    context.getString(R.string.connection_setup_save_before_test)
                                } else if (!profile.credentialAvailable) {
                                    context.getString(R.string.connection_setup_server_key_required)
                                } else if (SpeechKitServerApi(profile, context = context).healthy()) {
                                    context.getString(R.string.connection_setup_server_reachable)
                                } else context.getString(R.string.connection_setup_test_failed)
                            } catch (e: CancellationException) {
                                throw e
                            } catch (e: Exception) {
                                context.getString(R.string.connection_setup_test_failed)
                            }
                            testing = false
                        }
                    },
                ) { Text(stringResource(R.string.settings_connection_test)) }
                if (saved != null) {
                    TextButton(onClick = {
                        scope.launch {
                            if (saveSelection { selection.clearServer() }) {
                                url = ""
                                token = ""
                                status = context.getString(R.string.settings_connection_cleared)
                            }
                        }
                    }) { Text(stringResource(R.string.settings_connection_clear)) }
                }
            }
            status?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
            }
            if (BuildConfig.CONNECTION_SETUP_ENABLED && choice != 0 && mode != ConnectionMode.ON_DEVICE &&
                ((choice == 2 && mode == ConnectionMode.KOMBIFY_CLOUD && cloudSession != null) ||
                 (choice == 1 && ((remoteKind == 0 && mode == ConnectionMode.BYOK) ||
                  (remoteKind == 1 && (mode == ConnectionMode.SELF_HOST || mode == ConnectionMode.SPEECHKIT_ORIGIN)) ||
                  (remoteKind == 2 && mode == ConnectionMode.FIREBASE))))) {
                val preferences = listOf(
                    ExecutionMode.PREFER_ENDPOINT to stringResource(R.string.connection_setup_remote_first),
                    ExecutionMode.PREFER_DEVICE to stringResource(R.string.connection_setup_local_first),
                    ExecutionMode.DEVICE_ONLY to stringResource(R.string.connection_setup_local_only),
                    ExecutionMode.ENDPOINT_ONLY to stringResource(R.string.connection_setup_cloud_only),
                )
                SetupDropdown(
                    label = stringResource(R.string.connection_setup_preference),
                    selected = snapshot.policy.mode,
                    options = preferences,
                    onSelect = { preference -> scope.launch { saveSelection { selection.updatePreference(preference) } } },
                )
            }
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
@OptIn(ExperimentalMaterial3Api::class)
private fun <T> SetupDropdown(label: String, selected: T, options: List<Pair<T, String>>, onSelect: (T) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    Column(modifier = Modifier.fillMaxWidth()) {
        Text(label, style = MaterialTheme.typography.bodySmall)
        Box {
            OutlinedButton(onClick = { expanded = true }, modifier = Modifier.fillMaxWidth().semantics { contentDescription = label }) {
                Text(options.firstOrNull { it.first == selected }?.second.orEmpty(), modifier = Modifier.weight(1f), textAlign = TextAlign.Start)
                ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded)
            }
            DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
                options.forEach { (value, title) ->
                    DropdownMenuItem(text = { Text(title) }, onClick = { expanded = false; onSelect(value) })
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
