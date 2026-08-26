package io.kombify.speechkit.ime

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import android.view.View
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import android.view.inputmethod.InputMethodSubtype
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.ComposeView
import android.inputmethodservice.InputMethodService
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.audio.PcmStreamPlayer
import io.kombify.speechkit.ime.ui.VoiceAgentPanelUi
import io.kombify.speechkit.ime.ui.VoicePanelUi
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.net.DictationController
import io.kombify.speechkit.net.VoiceAgentAudio
import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.ui.ServiceWindowOwner
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import javax.inject.Inject
import timber.log.Timber

/**
 * Voice-only IME (B-M2 Stage 1): a Compose panel in the IME window driving
 * [VoicePanelController] against the shared `:net` dictation pipeline. Works
 * "behind" any keyboard — the user hops over via the system IME switcher for
 * a dictation segment and hops back with the panel's keyboard key.
 */
@AndroidEntryPoint
class SpeechKitVoiceImeService : InputMethodService() {

    /**
     * Flavor-bound profile resolution (B-M2b): system on-device STT as the
     * zero-config default; the kombify flavor upgrades to a configured
     * speechkit-server automatically.
     */
    @Inject
    lateinit var profileSource: ConnectionProfileSource

    private lateinit var serviceScope: CoroutineScope
    private lateinit var controller: VoicePanelController
    private lateinit var agentController: ImeVoiceAgentController
    private val agentPlayer = PcmStreamPlayer(VoiceAgentAudio.SERVER_SAMPLE_RATE)
    private var windowOwner: ServiceWindowOwner? = null

    override fun onCreate() {
        super.onCreate()
        serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
        controller = VoicePanelController(
            scope = serviceScope,
            sessionFactory = ::openConfiguredSession,
            audioCapture = MicAudioCapture(),
            micPermission = object : MicPermissionGate {
                override fun isGranted(): Boolean = ContextCompat.checkSelfPermission(
                    this@SpeechKitVoiceImeService, Manifest.permission.RECORD_AUDIO,
                ) == PackageManager.PERMISSION_GRANTED

                override fun request() =
                    MicPermissionTrampolineActivity.launch(this@SpeechKitVoiceImeService)
            },
            initialLanguage = subtypeLanguage(currentSubtype()),
        )
        agentController = ImeVoiceAgentController(
            scope = serviceScope,
            controllerFactory = { VoiceAgentController(profileSource.currentProfile()) },
            audioCapture = MicAudioCapture(),
            micPermission = object : MicPermissionGate {
                override fun isGranted(): Boolean = ContextCompat.checkSelfPermission(
                    this@SpeechKitVoiceImeService, Manifest.permission.RECORD_AUDIO,
                ) == PackageManager.PERMISSION_GRANTED

                override fun request() =
                    MicPermissionTrampolineActivity.launch(this@SpeechKitVoiceImeService)
            },
        )
        // Both controllers hear every answer; each ignores the ones it did not
        // ask for, so a grant resumes whichever mode the user was in.
        serviceScope.launch {
            MicPermissionEvents.results.collect { granted ->
                controller.onMicPermissionResult(granted)
                agentController.onMicPermissionResult(granted)
            }
        }
    }

    override fun onCreateInputView(): View {
        // Fresh owners per input view: the framework may recreate the view
        // (config changes), and a destroyed lifecycle cannot be restarted.
        windowOwner?.onDestroy()
        val owner = ServiceWindowOwner()
        windowOwner = owner
        // The owners must also sit on the IME window's decor view, not only on
        // the ComposeView. Compose resolves the recomposer from the window's
        // root, so with owners attached to the child alone it looked upward,
        // found nothing, and killed the process the first time the keyboard
        // was asked to appear:
        //   IllegalStateException: ViewTreeLifecycleOwner not found from
        //   android.widget.LinearLayout{... android:id/parentPanel}
        //   at InputMethodService.setInputView
        // Observed on an emulator; the build, lint and unit gates were all
        // green while the keyboard could not be shown at all.
        window?.window?.decorView?.let(owner::attachTo)
        return ComposeView(this).also { view ->
            owner.attachTo(view)
            view.setContent {
                MaterialTheme {
                    // Two surfaces, not one panel with a flag: dictation
                    // commits into the editor, the agent never touches it.
                    var agentMode by remember { mutableStateOf(false) }
                    var holding by remember { mutableStateOf(false) }

                    if (agentMode) {
                        val agentState by agentController.state.collectAsState()
                        // One collector for the whole agent session rather than
                        // an effect keyed on the newest frame: the queue is
                        // ordered and every frame has to be played, while an
                        // effect could only ever see the frames that survived
                        // to the next composition. play() moves itself off this
                        // dispatcher, which is the IME window's main thread.
                        LaunchedEffect(Unit) {
                            agentController.audio.collect { pcm -> agentPlayer.play(pcm) }
                        }
                        VoiceAgentPanelUi(
                            state = agentState,
                            holding = holding,
                            onHoldChange = { hold ->
                                holding = hold
                                if (hold) {
                                    // Taking the turn is the barge-in: drop the
                                    // answer still queued and cut what the
                                    // speaker is already reading out, instead of
                                    // talking over it.
                                    agentController.discardPendingAudio()
                                    agentPlayer.flush()
                                    agentController.beginTurn()
                                } else {
                                    agentController.endTurn()
                                }
                            },
                            onEnd = {
                                holding = false
                                agentController.stop()
                                agentPlayer.release()
                                agentMode = false
                            },
                            onSwitchToDictation = {
                                holding = false
                                agentController.stop()
                                agentPlayer.release()
                                agentMode = false
                            },
                        )
                    } else {
                        val state by controller.state.collectAsState()
                        val language by controller.language.collectAsState()
                        VoicePanelUi(
                            state = state,
                            language = language,
                            onMicToggle = controller::toggleMic,
                            onRetry = controller::retry,
                            onLanguageToggle = { controller.setLanguage(nextLanguage(language)) },
                            onSwitchKeyboard = ::switchBackToKeyboard,
                            onStartAgent = {
                                agentMode = true
                                agentController.start()
                            },
                        )
                    }
                }
            }
        }
    }

    override fun onStartInputView(editorInfo: EditorInfo, restarting: Boolean) {
        super.onStartInputView(editorInfo, restarting)
        windowOwner?.onResume()
        val ic = currentInputConnection ?: return
        controller.showPanel(ic, editorInfo)
    }

    override fun onFinishInputView(finishingInput: Boolean) {
        controller.hidePanel()
        windowOwner?.onPause()
        super.onFinishInputView(finishingInput)
    }

    override fun onCurrentInputMethodSubtypeChanged(newSubtype: InputMethodSubtype) {
        super.onCurrentInputMethodSubtypeChanged(newSubtype)
        controller.setLanguage(subtypeLanguage(newSubtype))
    }

    /** A voice panel has no use for the fullscreen extract editor. */
    override fun onEvaluateFullscreenMode(): Boolean = false

    override fun onDestroy() {
        controller.shutdown()
        windowOwner?.onDestroy()
        windowOwner = null
        serviceScope.cancel()
        super.onDestroy()
    }

    /**
     * Opens a dictation session on the flavor-resolved profile. Since B-M2b
     * there is always a usable profile — the system on-device recognizer is
     * the zero-config floor — so the former `not_configured` throw is gone.
     * The context enables the system tier; network tiers ignore it.
     */
    private suspend fun openConfiguredSession(): StreamingSttSession =
        DictationController(
            profile = profileSource.currentProfile(),
            context = applicationContext,
        ).openSession()

    private fun switchBackToKeyboard() {
        val switched = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            switchToPreviousInputMethod()
        } else {
            @Suppress("DEPRECATION")
            (getSystemService(INPUT_METHOD_SERVICE) as InputMethodManager)
                .switchToLastInputMethod(window?.window?.attributes?.token)
        }
        if (!switched) {
            Timber.d("voice ime: no previous IME to switch back to")
            (getSystemService(INPUT_METHOD_SERVICE) as InputMethodManager)
                .showInputMethodPicker()
        }
    }

    private fun currentSubtype(): InputMethodSubtype? =
        (getSystemService(INPUT_METHOD_SERVICE) as InputMethodManager)
            .currentInputMethodSubtype

    companion object {
        internal val SUPPORTED_LANGUAGES = listOf("de", "en")

        internal fun subtypeLanguage(subtype: InputMethodSubtype?): String {
            // languageTag exists since API 24 (minSdk is 26); locale is the
            // legacy fallback for subtypes created without a tag.
            val locale = subtype?.let {
                it.languageTag.ifEmpty { @Suppress("DEPRECATION") it.locale }
            }.orEmpty()
            val language = locale.substringBefore('-').substringBefore('_').lowercase()
            return if (language in SUPPORTED_LANGUAGES) language else SUPPORTED_LANGUAGES.first()
        }

        internal fun nextLanguage(current: String): String {
            val index = SUPPORTED_LANGUAGES.indexOf(current)
            return SUPPORTED_LANGUAGES[(index + 1).mod(SUPPORTED_LANGUAGES.size)]
        }
    }
}
