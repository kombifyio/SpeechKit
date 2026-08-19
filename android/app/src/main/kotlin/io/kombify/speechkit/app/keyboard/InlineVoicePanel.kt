package io.kombify.speechkit.app.keyboard

import android.Manifest
import android.app.Application
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Bitmap
import android.graphics.Canvas
import android.inputmethodservice.InputMethodService
import android.os.SystemClock
import android.view.inputmethod.EditorInfo
import android.view.View
import android.view.ViewGroup
import android.view.ViewTreeObserver
import android.view.inputmethod.InputConnection
import android.widget.FrameLayout
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.ComposeView
import androidx.core.content.ContextCompat
import helium314.keyboard.latin.R
import helium314.keyboard.latin.SpeechKitVoiceBridge
import io.kombify.speechkit.ime.ImeAgentAudioPlayer
import io.kombify.speechkit.ime.ImeVoiceAgentController
import io.kombify.speechkit.ime.MicAudioCapture
import io.kombify.speechkit.ime.MicPermissionEvents
import io.kombify.speechkit.ime.MicPermissionGate
import io.kombify.speechkit.ime.MicPermissionTrampolineActivity
import io.kombify.speechkit.ime.VoicePanelController
import io.kombify.speechkit.ime.host.isDictationBlocked
import io.kombify.speechkit.ime.ui.VoiceAgentPanelUi
import io.kombify.speechkit.ime.ui.VoicePanelUi
import io.kombify.speechkit.net.ConnectionProfile
import io.kombify.speechkit.net.ConnectionProfileSource
import io.kombify.speechkit.net.DictationController
import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.ui.ServiceWindowOwner
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import java.lang.ref.WeakReference
import timber.log.Timber

/**
 * Answers the keyboard's voice key in place, and hosts SpeechKit's own action
 * row above the keys.
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
 * The panel's controllers, scope and views are built per activation and torn
 * down in [hidePanel]; the action row is built per input view and torn down in
 * [detachActionRow]. The window owner both of them compose against is neither
 * — see below.
 *
 * ## One window owner, and why nothing mounted in the window may destroy it
 *
 * Compose resolves its recomposer from the *window root*, not from the view it
 * is asked to compose. `AbstractComposeView` falls through to
 * `windowRecomposer`, which builds one from the lifecycle owner found on the
 * decor view; owners attached to a ComposeView alone are invisible to that
 * lookup, and the first attempt at this killed the process the first time the
 * keyboard was asked to appear (the exception is quoted verbatim in
 * `SpeechKitVoiceImeService.onCreateInputView`). So whatever composes here has
 * to have an owner on the decor view.
 *
 * That pulls directly against the row surviving the panel. The panel used to
 * install an owner of its own on that same decor view and destroy it in
 * [hidePanel], which cancels the recomposer the row's composition also runs
 * on: the row would have gone blank after the first panel open/close cycle.
 *
 * Both hold once the owner belongs to the *window* rather than to either
 * surface: one owner per decor view, attached by whichever of the two mounts
 * first, and destroyed by neither. Recreating it per input-view cycle is not
 * an option either — the framework calls `onFinishInputView` and
 * `onStartInputView` back to back within one main-loop message when the user
 * moves between fields, while a cancelled recomposer is uninstalled from the
 * window root asynchronously, so the row would come back bound to a dead one.
 * It is dropped only when a different decor view turns up, and the view it is
 * keyed on is held weakly so this process-lifetime host cannot pin a destroyed
 * keyboard window.
 *
 * ## What the fork calls
 *
 * [showPanel]/[hidePanel] answer the voice key.
 * [attachActionRow]/[detachActionRow] are the input view's own lifecycle and
 * are driven from LatinIME's `onStartInputView`/`onFinishInputView`: the row
 * has to be there before anything is pressed, which no voice-key callback can
 * provide.
 */
class InlineVoicePanel(
    private val application: Application,
    private val profileSource: ConnectionProfileSource,
) : SpeechKitVoiceBridge.Host {

    // Outlives every panel activation on purpose: the microphone answer comes
    // back through a trampoline activity, and the panel it belongs to may
    // already have been torn down by then. Never cancelled — the host is
    // registered once for the process lifetime.
    private val hostScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private var scope: CoroutineScope? = null
    private var controller: VoicePanelController? = null
    private var agentController: ImeVoiceAgentController? = null
    private var agentPlayer: ImeAgentAudioPlayer? = null
    private var keyboard: InputMethodService? = null

    private var windowOwner: ServiceWindowOwner? = null
    private var windowRoot: WeakReference<View>? = null

    private var actionRowView: ComposeView? = null
    private var actionRowProfile: ConnectionProfile? = null
    private var actionRowPalette: MutableState<KeyboardActionRowPalette>? = null

    private var micRequest: PendingActivation? = null
    private var resumeAfterGrant: PendingActivation? = null

    init {
        hostScope.launch {
            MicPermissionEvents.results.collect { granted -> onMicPermissionResult(granted) }
        }
    }

    /**
     * The voice key: dictation on whatever tier the flavor resolves, which is
     * the zero-config on-device recognizer unless a server is paired.
     */
    override fun showPanel(
        service: InputMethodService,
        inputConnection: InputConnection,
        editorInfo: EditorInfo,
    ): Boolean = open(
        service = service,
        inputConnection = inputConnection,
        editorInfo = editorInfo,
        tier = null,
        agent = false,
        agentProvider = null,
    )

    /**
     * Mounts the action row into the keyboard's input view. Idempotent, so the
     * fork may call it from every `onStartInputView` without checking.
     */
    override fun attachActionRow(service: InputMethodService) {
        // Both halves are contained, and nothing rethrows: this hook runs
        // inside LatinIME's `onStartInputViewInternal`, where an escaping
        // throw kills the input method process on field focus and skips
        // everything the fork does after the call. The mount does its own
        // cleanup inside; this is the net around the lookups either side of
        // it, so the hook itself cannot be the thing that fails.
        runCatching { mountActionRow(service) }
            .onFailure { Timber.w(it, "Could not attach the action row") }
        // After the row, so the panel this may reopen is layered over an input
        // view that is already in its final shape.
        runCatching { resumeMicGrant(service) }
            .onFailure { Timber.w(it, "Could not resume the interrupted voice panel") }
    }

    /** Drops the action row when the input view goes away. */
    override fun detachActionRow() {
        val view = actionRowView ?: return
        actionRowView = null
        actionRowProfile = null
        actionRowPalette = null
        runCatching {
            (view.parent as? ViewGroup)?.let { container ->
                // Removing the view disposes its composition; GONE restores
                // the fork's "no host installed" state, which is what its
                // window geometry keys on.
                container.removeView(view)
                container.visibility = View.GONE
            }
        }.onFailure { Timber.w(it, "Could not remove the action row") }
    }

    override fun hidePanel() {
        val service = keyboard ?: return
        controller?.hidePanel()
        agentController?.stop()
        agentPlayer?.release()
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
        // The window owner is not destroyed here: the action row composes
        // against it too, and this runs on every `onFinishInputView`.
        scope?.cancel()
        scope = null
        controller = null
        agentController = null
        agentPlayer = null
        keyboard = null
    }

    private fun mountActionRow(service: InputMethodService) {
        val container = service.actionRowContainer() ?: run {
            Timber.w("Keyboard input view has no action row container")
            return
        }
        // The profile is read here rather than per composition: the row states
        // why an action is blocked, and re-resolving mid-frame could make the
        // label and the button disagree. Comparing it is also what rebuilds
        // the row after a server is paired — otherwise it would keep claiming
        // there is none until the keyboard is recreated.
        val profile = profileSource.currentProfile()
        val mounted = actionRowView
        if (mounted != null && mounted.parent === container && actionRowProfile == profile) {
            return
        }
        detachActionRow()

        // Everything below is contained, because this runs inside LatinIME's
        // onStartInputViewInternal: an escaping throw would take the IME
        // process down the moment a field takes focus, and would also skip the
        // rest of that method — the gesture consumer, mInputLogic.startInput
        // and the main keyboard reload. Losing the row costs one feature;
        // losing the method leaves a keyboard that cannot type. Compose in a
        // service window is the exact machinery this seam has been bitten by
        // before, so it does not get to be the one unguarded direction.
        runCatching {
            val owner = windowOwner(service)
                ?: error("keyboard window has no decor view")
            val items = keyboardActionRowItems(profile)
            val palette = mutableStateOf(service.keyboardPalette())
            val view = ComposeView(service).also { composeView ->
                owner.attachTo(composeView)
                composeView.setContent {
                    KeyboardActionRowTheme(palette.value) {
                        KeyboardActionRow(items = items, onAction = { service.perform(it) })
                    }
                }
            }
            container.removeAllViews()
            container.addView(view)
            container.visibility = View.VISIBLE
            owner.onResume()
            actionRowView = view
            actionRowProfile = profile
            actionRowPalette = palette
            view.repaintOnceKeyboardIsPainted(service)
        }.onFailure { failure ->
            Timber.w(failure, "Could not mount the action row")
            // Nothing half-attached: a ComposeView that threw out of
            // setContent is still a child of the container, and a container
            // left VISIBLE around it is measured into the keyboard height by
            // LatinIME.getSpeechKitActionRowHeight().
            runCatching {
                container.removeAllViews()
                container.visibility = View.GONE
            }
            actionRowView = null
            actionRowProfile = null
            actionRowPalette = null
        }
    }

    /**
     * Re-reads the keyboard colours once it has actually painted them.
     *
     * The fork paints main_keyboard_frame from InputView.onNextLayout, which
     * runs after the hook that mounts this row, so the first sample is always
     * taken from a frame that has no background yet. A pre-draw listener is
     * the first point at which the whole tree has been laid out; it removes
     * itself, so nothing outlives the view it was registered from.
     */
    private fun ComposeView.repaintOnceKeyboardIsPainted(service: InputMethodService) {
        val row: View = this
        row.viewTreeObserver.addOnPreDrawListener(
            object : ViewTreeObserver.OnPreDrawListener {
                override fun onPreDraw(): Boolean {
                    runCatching {
                        row.viewTreeObserver.takeIf { it.isAlive }
                            ?.removeOnPreDrawListener(this)
                        actionRowPalette?.value = service.keyboardPalette()
                    }
                    return true
                }
            },
        )
    }

    /**
     * The colours the row paints itself in, taken from the keyboard below it.
     *
     * Sampled off the pixel rather than read out of HeliBoard's settings: the
     * fork applies its palette as a colour *filter* over a white drawable, so
     * the drawable's own colour is white on every theme, and the object that
     * holds the real values is fork internals this module deliberately does
     * not reach into — the whole patch surface is one bridge file. Rasterising
     * a detached copy answers the only question the row has, "what is behind
     * me", for flat colours, gradients and user background images alike, and
     * never touches the drawable the keyboard is drawing with.
     */
    private fun InputMethodService.keyboardPalette(): KeyboardActionRowPalette {
        val decor = window?.window?.decorView
        val night = (resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK) ==
            Configuration.UI_MODE_NIGHT_YES
        return keyboardActionRowPalette(
            keyboardBackground = decor?.let { keyboardBackgroundColor(it) },
            nightMode = night,
        )
    }

    private fun keyboardBackgroundColor(decor: View): Int? = runCatching {
        val painted = decor.findViewById<View>(R.id.main_keyboard_frame)?.background
            ?: return@runCatching null
        val sample = painted.constantState?.newDrawable(decor.resources)?.mutate()
            ?: return@runCatching null
        // The colour lives in the filter, not in the drawable, and a constant
        // state does not carry a filter — without this the copy reports the
        // white the fork paints through.
        sample.colorFilter = painted.colorFilter
        sample.alpha = painted.alpha
        sample.setBounds(0, 0, 1, 1)
        val bitmap = Bitmap.createBitmap(1, 1, Bitmap.Config.ARGB_8888)
        try {
            sample.draw(Canvas(bitmap))
            bitmap.getPixel(0, 0).takeIf { (it ushr 24) != 0 }
        } finally {
            bitmap.recycle()
        }
    }.getOrNull()

    /**
     * The owner every composition in this window resolves its recomposer
     * through. See the class comment for why it is per window and not per
     * panel or per row, and why it is not recreated per input-view cycle.
     */
    private fun windowOwner(service: InputMethodService): ServiceWindowOwner? {
        val decor = service.window?.window?.decorView ?: return null
        val existing = windowOwner
        if (existing != null && windowRoot?.get() === decor) return existing
        // A different window: the old one is gone, so its recomposer should go
        // with it rather than linger with a lifecycle nobody advances.
        existing?.onDestroy()
        val owner = ServiceWindowOwner()
        owner.attachTo(decor)
        windowOwner = owner
        windowRoot = WeakReference(decor)
        return owner
    }

    /**
     * Routes a microphone answer to whichever surface asked for it.
     *
     * Both controllers hear every result and each ignores the ones it did not
     * ask for, so a grant resumes whichever mode the user was in. This lives
     * on [hostScope] rather than on the panel's own scope because the
     * trampoline activity takes window focus, which can make the fork call
     * [hidePanel] before the user has even answered the dialog — on the panel
     * scope the collector would be dead by the time the grant arrived, and
     * `MicPermissionEvents` has no replay to deliver it again.
     */
    private fun onMicPermissionResult(granted: Boolean) {
        val request = micRequest
        micRequest = null
        val live = controller
        live?.onMicPermissionResult(granted)
        agentController?.onMicPermissionResult(granted)
        if (live != null || request == null || !granted) return
        // The panel that asked is gone. Nothing can be reopened from here —
        // there is no editor to bind to until the input view comes back — so
        // the request is parked for the next one.
        resumeAfterGrant = request
    }

    /**
     * Reopens the panel the user lost to the permission dialog.
     *
     * Without this the microphone press is simply swallowed: the grant lands
     * while the keyboard is not showing, and the user has to press again.
     *
     * Bound to the editor that asked. This runs from every `onStartInputView`,
     * which is every field in every application, so without the identity check
     * a grant given in one app opened a live microphone in the next field the
     * user happened to tap somewhere else — dictation committing text into an
     * editor nobody pointed it at. A non-matching editor leaves the request
     * parked rather than eating it, so coming back to the original field still
     * works; [MIC_RESUME_WINDOW_MS] is what finally drops it.
     */
    private fun resumeMicGrant(service: InputMethodService) {
        val pending = resumeAfterGrant ?: return
        if (SystemClock.elapsedRealtime() - pending.requestedAtMs > MIC_RESUME_WINDOW_MS) {
            resumeAfterGrant = null
            return
        }
        val connection = service.currentInputConnection ?: return
        val editor = service.currentInputEditorInfo ?: return
        if (!pending.matches(editor)) return
        resumeAfterGrant = null
        Timber.i("Reopening the voice panel the microphone prompt interrupted")
        val reopened = open(
            service = service,
            inputConnection = connection,
            editorInfo = editor,
            tier = pending.tier,
            agent = pending.agent,
            agentProvider = pending.agentProvider,
        )
        // The user had already asked to speak, and the dialog is the only
        // reason nothing happened; reopening a panel that then sits idle would
        // still cost them a second press. The agent side needs nothing here,
        // because open() starts the conversation itself.
        if (reopened && !pending.agent) controller?.toggleMic()
    }

    /**
     * Opens the panel for one activation.
     *
     * @param tier the dictation tier the user picked, or null to take whatever
     *   the flavor resolves. Resolved once here rather than per session open:
     *   [ConnectionProfileSource] re-resolves by design, which would quietly
     *   undo an explicit choice on the second mic press.
     * @param agent opens straight into Voice Agent mode instead of dictation.
     * @param agentProvider the realtime backend to open conversations on, or
     *   null for whatever the server defaults to.
     */
    private fun open(
        service: InputMethodService,
        inputConnection: InputConnection,
        editorInfo: EditorInfo,
        tier: ConnectionProfile?,
        agent: Boolean,
        agentProvider: String?,
    ): Boolean {
        // Refusing here rather than showing a dead panel lets the bridge fall
        // back to upstream's behaviour, which is still better than nothing.
        if (isDictationBlocked(editorInfo.inputType)) {
            Timber.i("Inline dictation refused for this editor")
            return false
        }
        if (controller != null) hidePanel()
        // An explicit activation supersedes anything a previous one was still
        // waiting on.
        micRequest = null
        resumeAfterGrant = null

        val profile = tier ?: profileSource.currentProfile()
        val panelScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
        // Which mode is on screen when the trampoline takes focus: the panel
        // can switch between dictation and the agent while it is open, so this
        // is read at request time rather than frozen at activation time.
        var requestedAgent = agent
        // One gate for both modes: they take turns, and two gates would each
        // fire their own trampoline for the same permission.
        val gate = object : MicPermissionGate {
            override fun isGranted(): Boolean = ContextCompat.checkSelfPermission(
                application, Manifest.permission.RECORD_AUDIO,
            ) == PackageManager.PERMISSION_GRANTED

            override fun request() {
                // The editor is captured with the request, not with the grant:
                // by the time the answer arrives the trampoline has taken
                // focus and this panel may be gone, and the resume has to know
                // which field it is allowed to come back to.
                val target = service.currentInputEditorInfo
                micRequest = PendingActivation(
                    tier = profile,
                    agent = requestedAgent,
                    agentProvider = agentProvider,
                    editorPackage = target?.packageName,
                    editorFieldId = target?.fieldId ?: View.NO_ID,
                    requestedAtMs = SystemClock.elapsedRealtime(),
                )
                MicPermissionTrampolineActivity.launch(service)
            }
        }
        val panelController = VoicePanelController(
            scope = panelScope,
            sessionFactory = { openSession(profile) },
            audioCapture = MicAudioCapture(),
            micPermission = gate,
        )
        val panelAgent = ImeVoiceAgentController(
            scope = panelScope,
            controllerFactory = { VoiceAgentController(profile) },
            audioCapture = MicAudioCapture(),
            micPermission = gate,
        )
        val player = ImeAgentAudioPlayer()

        val owner = windowOwner(service) ?: run {
            Timber.w("Keyboard window has no decor view; falling back")
            panelScope.cancel()
            return false
        }

        val view = ComposeView(service).also { composeView ->
            owner.attachTo(composeView)
            composeView.setContent {
                MaterialTheme {
                    // Two surfaces, not one panel with a flag: dictation
                    // commits into the editor, the agent never touches it.
                    var agentMode by remember { mutableStateOf(agent) }
                    var holding by remember { mutableStateOf(false) }

                    if (agentMode) {
                        val agentState by panelAgent.state.collectAsState()
                        // One collector for the whole agent session rather than
                        // an effect keyed on the newest frame: the queue is
                        // ordered and every frame has to be played, while an
                        // effect could only ever see the frames that survived
                        // to the next composition. play() moves itself off this
                        // dispatcher, which is the keyboard's main thread.
                        LaunchedEffect(Unit) {
                            panelAgent.audio.collect { pcm -> player.play(pcm) }
                        }
                        VoiceAgentPanelUi(
                            state = agentState,
                            holding = holding,
                            provider = agentProvider,
                            onHoldChange = { hold ->
                                holding = hold
                                if (hold) {
                                    // Taking the turn is the barge-in: drop the
                                    // answer still queued and cut what the
                                    // speaker is already reading out, instead
                                    // of talking over it.
                                    panelAgent.discardPendingAudio()
                                    player.flush()
                                    panelAgent.beginTurn()
                                } else {
                                    panelAgent.endTurn()
                                }
                            },
                            // Leaving the conversation returns to dictation, not
                            // to the keys: tearing the panel down here would make
                            // "end the conversation" and "back to typing" the
                            // same gesture.
                            onEnd = {
                                holding = false
                                panelAgent.stop()
                                player.release()
                                agentMode = false
                                requestedAgent = false
                            },
                            onSwitchToDictation = {
                                holding = false
                                panelAgent.stop()
                                player.release()
                                agentMode = false
                                requestedAgent = false
                            },
                        )
                    } else {
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
                            // Back to keys, not to another IME: the whole point
                            // of answering the voice key here is that the user
                            // never leaves this keyboard.
                            onSwitchKeyboard = ::hidePanel,
                            // A conversation has nowhere to go without a server,
                            // and VoiceAgentController throws for every other
                            // profile before a frame moves.
                            onStartAgent = if (profile is ConnectionProfile.Server) {
                                {
                                    agentMode = true
                                    requestedAgent = true
                                    // The backend the row picked, not the
                                    // server default: coming back from one
                                    // conversation and starting another must
                                    // not silently change which agent answers.
                                    panelAgent.start(agentProvider)
                                }
                            } else {
                                null
                            },
                        )
                    }
                }
            }
        }

        scope = panelScope
        controller = panelController
        agentController = panelAgent
        agentPlayer = player
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
        if (agent) panelAgent.start(agentProvider)
        return true
    }

    /**
     * Runs one action-row button. The editor is read from the service exactly
     * as the voice key does, so a row press and a key press reach the same
     * panel through the same guards.
     */
    private fun InputMethodService.perform(action: KeyboardAction) {
        val connection = currentInputConnection ?: return
        val editor = currentInputEditorInfo ?: return
        val provider = action.agentProvider
        val tier = when (action) {
            KeyboardAction.OnDeviceDictation -> ConnectionProfile.SystemOnDevice()
            // The row disables these without a paired server, so the cast is
            // the one the user's choice implies rather than a guess.
            KeyboardAction.ServerDictation,
            KeyboardAction.AgentDeepgram,
            KeyboardAction.AgentAssemblyAi,
            KeyboardAction.AgentOpenAi,
            -> profileSource.currentProfile()

            // Nothing to bind to yet; the row renders it disabled.
            KeyboardAction.CompanionApp -> return
        }
        open(
            service = this,
            inputConnection = connection,
            editorInfo = editor,
            tier = tier,
            agent = provider != null,
            agentProvider = provider,
        )
    }

    /**
     * The action row's container. Resolved through this module's own R because
     * the id is declared here as well as in the fork's layout — see
     * `res/values/ids.xml`.
     */
    private fun InputMethodService.actionRowContainer(): ViewGroup? =
        window?.window?.decorView
            ?.findViewById(io.kombify.speechkit.R.id.speechkit_action_row)

    private suspend fun openSession(profile: ConnectionProfile): StreamingSttSession =
        DictationController(
            profile = profile,
            context = application,
        ).openSession()

    /** What the user asked for when the permission trampoline took over. */
    private data class PendingActivation(
        val tier: ConnectionProfile,
        val agent: Boolean,
        val agentProvider: String?,
        /** Application the microphone was pressed in, per [EditorInfo.packageName]. */
        val editorPackage: String?,
        /** Field it was pressed in, per [EditorInfo.fieldId]. */
        val editorFieldId: Int,
        val requestedAtMs: Long,
    ) {
        /**
         * Whether [editor] is the field this activation belongs to.
         *
         * Package and field together, because either alone is too coarse: the
         * package would resume in any field of the same application, and the
         * field id is a view id that different applications reuse freely. A
         * null package never matches, so an activation whose editor could not
         * be read expires instead of resuming somewhere unrelated. Custom
         * editors that report [View.NO_ID] collapse to their package, which
         * keeps the answer inside the application the user was in.
         */
        fun matches(editor: EditorInfo): Boolean =
            editorPackage != null &&
                editorPackage == editor.packageName &&
                editorFieldId == editor.fieldId
    }

    private companion object {
        /**
         * How long a granted microphone may still reopen the panel it was
         * asked for.
         *
         * Thirty seconds, not two minutes: the resume exists for one specific
         * interruption — the permission dialog took the window away mid-press
         * — and answering it takes a few seconds, not minutes. Past that the
         * user has moved on, and a keyboard that opens a live microphone by
         * itself is no longer explainable by anything they remember doing.
         */
        const val MIC_RESUME_WINDOW_MS = 30_000L
    }
}
