package io.kombify.speechkit.app.keyboard

import android.Manifest
import android.app.Application
import android.content.pm.PackageManager
import android.inputmethodservice.InputMethodService
import android.view.inputmethod.EditorInfo
import android.view.View
import android.view.inputmethod.InputConnection
import android.widget.FrameLayout
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.ComposeView
import androidx.core.content.ContextCompat
import helium314.keyboard.latin.R
import helium314.keyboard.latin.SpeechKitVoiceBridge
import io.kombify.speechkit.ime.MicAudioCapture
import io.kombify.speechkit.ime.MicPermissionEvents
import io.kombify.speechkit.ime.MicPermissionGate
import io.kombify.speechkit.ime.MicPermissionTrampolineActivity
import io.kombify.speechkit.ime.VoicePanelController
import io.kombify.speechkit.ime.host.isDictationBlocked
import io.kombify.speechkit.ime.ui.VoicePanelUi
import io.kombify.speechkit.net.ConnectionProfileSource
import io.kombify.speechkit.net.DictationController
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.ui.ServiceWindowOwner
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import timber.log.Timber

/**
 * Answers the keyboard's voice key in place.
 *
 * Upstream HeliBoard hands the voice key to another input method, which takes
 * the user out of the keyboard entirely. This swaps the keyboard's input view
 * for SpeechKit's dictation panel instead, so the text lands in the same
 * editor and the keyboard comes back when dictation ends.
 *
 * ## Why this lives in `:app` and not in `:ime`
 *
 * It is the only class that imports both sides. `:ime` is Apache-2.0 and is
 * consumed by Companion; the moment it referenced [SpeechKitVoiceBridge] it
 * would become a GPL-derived work and stop being usable there. `:app` is
 * already GPL-3.0 as a whole because it links the keyboard, so the adapter
 * costs nothing extra here.
 *
 * ## Where the panel is mounted
 *
 * Inside the keyboard's own input view, in the container the fork's
 * `input_view.xml` provides, with the keyboard frame turned INVISIBLE behind
 * it. Not via [InputMethodService.setInputView]: LatinIME derives both the
 * window height and the touchable region from its own keyboard frame, so
 * replacing that frame produced a full-screen window whose upper half was
 * rendered but did not receive touches. Mounting inside keeps every one of
 * those calculations valid, and INVISIBLE rather than GONE keeps the frame
 * contributing its height.
 *
 * Everything is built per activation rather than cached: the panel is
 * short-lived, and a window owner cannot be restarted once destroyed.
 */
class InlineVoicePanel(
    private val application: Application,
    private val profileSource: ConnectionProfileSource,
) : SpeechKitVoiceBridge.Host {

    private var scope: CoroutineScope? = null
    private var controller: VoicePanelController? = null
    private var windowOwner: ServiceWindowOwner? = null
    private var keyboard: InputMethodService? = null

    override fun showPanel(
        service: InputMethodService,
        inputConnection: InputConnection,
        editorInfo: EditorInfo,
    ): Boolean {
        // Refusing here rather than showing a dead panel lets the bridge fall
        // back to upstream's behaviour, which is still better than nothing.
        if (isDictationBlocked(editorInfo.inputType)) {
            Timber.i("Inline dictation refused for this editor")
            return false
        }
        if (controller != null) hidePanel()

        val panelScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
        val panelController = VoicePanelController(
            scope = panelScope,
            sessionFactory = ::openConfiguredSession,
            audioCapture = MicAudioCapture(),
            micPermission = object : MicPermissionGate {
                override fun isGranted(): Boolean = ContextCompat.checkSelfPermission(
                    application, Manifest.permission.RECORD_AUDIO,
                ) == PackageManager.PERMISSION_GRANTED

                override fun request() = MicPermissionTrampolineActivity.launch(service)
            },
        )
        panelScope.launch {
            MicPermissionEvents.results.collect { panelController.onMicPermissionResult(it) }
        }

        val owner = ServiceWindowOwner()
        // The owners have to sit on the IME window's decor view as well, not
        // only on the ComposeView: Compose resolves the recomposer from the
        // window root, and attaching to the child alone kills the process the
        // first time the panel is asked to appear.
        service.window?.window?.decorView?.let(owner::attachTo)

        val view = ComposeView(service).also { composeView ->
            owner.attachTo(composeView)
            composeView.setContent {
                MaterialTheme {
                    val state by panelController.state.collectAsState()
                    val language by panelController.language.collectAsState()
                    VoicePanelUi(
                        state = state,
                        language = language,
                        onMicToggle = panelController::toggleMic,
                        onRetry = panelController::retry,
                        onLanguageToggle = {
                            panelController.setLanguage(if (language == "de") "en" else "de")
                        },
                        // Back to keys, not to another IME: the whole point of
                        // answering the voice key here is that the user never
                        // leaves this keyboard.
                        onSwitchKeyboard = ::hidePanel,
                        // Dictation only. The Voice Agent is a conversation
                        // surface, not a typing one, and does not belong on a
                        // keyboard's input view.
                        onStartAgent = null,
                    )
                }
            }
        }

        scope = panelScope
        controller = panelController
        windowOwner = owner
        keyboard = service

        val decor = service.window?.window?.decorView
        val container = decor?.findViewById<FrameLayout>(R.id.speechkit_voice_panel)
        if (container == null) {
            Timber.w("Keyboard input view has no panel container; falling back")
            panelScope.cancel()
            return false
        }
        container.removeAllViews()
        container.addView(view)
        container.visibility = View.VISIBLE
        decor.findViewById<View>(R.id.main_keyboard_frame)?.visibility = View.INVISIBLE

        owner.onResume()
        panelController.showPanel(inputConnection, editorInfo)
        return true
    }

    override fun hidePanel() {
        val service = keyboard ?: return
        controller?.hidePanel()
        // Guarded because the service may already be tearing down, and failing
        // to bring the keys back is worse than a logged error.
        runCatching {
            val decor = service.window?.window?.decorView
            decor?.findViewById<FrameLayout>(R.id.speechkit_voice_panel)?.let { container ->
                container.removeAllViews()
                container.visibility = View.GONE
            }
            decor?.findViewById<View>(R.id.main_keyboard_frame)?.visibility = View.VISIBLE
        }.onFailure { Timber.w(it, "Could not restore the keyboard view") }
        windowOwner?.onDestroy()
        scope?.cancel()
        windowOwner = null
        scope = null
        controller = null
        keyboard = null
    }

    private suspend fun openConfiguredSession(): StreamingSttSession =
        DictationController(
            profile = profileSource.currentProfile(),
            context = application,
        ).openSession()
}
