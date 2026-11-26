package io.kombify.speechkit.app.hybrid

import android.content.Intent
import android.content.pm.PackageManager
import android.inputmethodservice.InputMethodService
import android.os.Bundle
import android.os.SystemClock
import android.provider.Settings
import android.graphics.Rect
import android.view.InputDevice
import android.view.MotionEvent
import android.view.View
import android.view.WindowInsets
import android.view.accessibility.AccessibilityNodeInfo
import android.view.inputmethod.InputMethodManager
import androidx.test.platform.app.InstrumentationRegistry
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isPopup
import androidx.compose.ui.test.isEnabled
import androidx.compose.ui.test.isNotEnabled
import androidx.compose.ui.test.isSelected
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import org.junit.Rule
import com.google.firebase.FirebaseApp
import com.google.firebase.appcheck.debug.testing.DebugAppCheckTestHelper
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.hybrid.DeviceReadiness
import io.kombify.speechkit.hybrid.InferenceLocation
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.app.keyboard.InlineVoicePanel
import io.kombify.speechkit.domain.ExecutionPolicy
import io.kombify.speechkit.domain.forInvocation
import io.kombify.speechkit.domain.restrictTo
import io.kombify.speechkit.net.StoredExecutionPolicy
import io.kombify.speechkit.domain.ExecutionMode
import helium314.keyboard.latin.SpeechKitVoiceBridge
import helium314.keyboard.keyboard.MainKeyboardView
import helium314.keyboard.latin.utils.ToolbarKey
import helium314.keyboard.latin.R as KeyboardR
import io.kombify.speechkit.ime.R as ImeR
import io.kombify.speechkit.stt.LANGUAGE_MULTI
import io.kombify.speechkit.stt.LocalSpeechAdapter
import io.kombify.speechkit.stt.LocalSpeechAudio
import io.kombify.speechkit.stt.LocalSpeechFailure
import io.kombify.speechkit.stt.LocalSpeechFailureKind
import io.kombify.speechkit.stt.LocalSpeechAvailability
import io.kombify.speechkit.stt.LocalSpeechLimits
import io.kombify.speechkit.stt.NativeLocalSpeechAdapter
import io.kombify.speechkit.stt.readPcm16
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.hybrid.MlKitLocalSpeechAdapter
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.Locale
import kotlin.math.sin
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.withTimeout
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Installed-app foreground and real Firebase SDK acceptance on a physical device. */
class HybridDeviceJourney {
    @get:Rule val compose = createEmptyComposeRule()
    @Test fun keyboardOpalPresentationJourney() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue("Keyboard capture is an explicit device journey", args.getString("requireKeyboardOpal") == "true")
        assertTrue("Keyboard capture requires the debug editor", BuildConfig.DEBUG)
        assertTrue("Keyboard capture requires the Opal feature", BuildConfig.VOICE_AGENT_OPAL_ENABLED)
        assertTrue("Keyboard gesture acceptance requires the local Hybrid destination", BuildConfig.HYBRID_ENABLED)
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val automation = instrumentation.uiAutomation
        automation.serviceInfo = automation.serviceInfo.apply {
            flags = flags or android.accessibilityservice.AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS
        }
        val imeId = "${context.packageName}/helium314.keyboard.latin.LatinIME"
        val previous = Settings.Secure.getString(context.contentResolver, Settings.Secure.DEFAULT_INPUT_METHOD)
        val previousHardKeyboardSetting = Settings.Secure.getString(context.contentResolver, "show_ime_with_hard_keyboard")
        val imm = context.getSystemService(InputMethodManager::class.java)
        val wasEnabled = imm.enabledInputMethodList.any { it.id == imeId }
        val hadMic = context.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
        val storedPolicy = StoredExecutionPolicy(context)
        val previousPolicy = storedPolicy.read(ConnectionProfile.Local)
        assertEquals("Fresh install must be device-only", ExecutionMode.DEVICE_ONLY, previousPolicy.mode)
        assertFalse("Fresh install must not permit network processing", previousPolicy.allowNetwork)
        assertFalse("Fresh install must not have microphone permission", hadMic)
        fun shell(command: String): String = automation.executeShellCommand(command).use { descriptor ->
            java.io.FileInputStream(descriptor.fileDescriptor).bufferedReader().use { it.readText() }
        }
        fun waitUntil(message: String, condition: () -> Boolean) {
            val deadline = SystemClock.uptimeMillis() + 15_000
            while (SystemClock.uptimeMillis() < deadline) {
                if (condition()) return
                SystemClock.sleep(100)
            }
            fail(message)
        }
        fun onMain(condition: () -> Boolean): Boolean {
            var result = false
            instrumentation.runOnMainSync { result = condition() }
            return result
        }
        fun screenshot(name: String) {
            instrumentation.waitForIdleSync()
            SystemClock.sleep(200) // Let the actual IME window render its first frame.
            val bitmap = automation.takeScreenshot() ?: error("No screenshot for $name")
            val output = File(context.getExternalFilesDir("acceptance"), name)
            output.outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
            bitmap.recycle()
            instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_keyboard_screenshot", output.absolutePath) })
        }
        fun findDescriptionNode(node: AccessibilityNodeInfo?, description: String): AccessibilityNodeInfo? {
            node ?: return null
            if (node.contentDescription?.toString() == description && node.isVisibleToUser) return node
            for (i in 0 until node.childCount) findDescriptionNode(node.getChild(i), description)?.let { return it }
            return null
        }
        fun findClickableAncestor(node: AccessibilityNodeInfo?): AccessibilityNodeInfo? {
            var candidate = node
            while (candidate != null && !candidate.isClickable) candidate = candidate.parent
            return candidate
        }
        fun findVisibleText(node: AccessibilityNodeInfo?, label: String): AccessibilityNodeInfo? {
            node ?: return null
            val matches = node.text?.toString()?.contains(label, ignoreCase = true) == true ||
                node.contentDescription?.toString()?.contains(label, ignoreCase = true) == true
            if (matches && node.isVisibleToUser) {
                var actionable: AccessibilityNodeInfo? = node
                while (actionable != null && !actionable.isClickable) actionable = actionable.parent
                if (actionable != null) return actionable
            }
            for (i in 0 until node.childCount) findVisibleText(node.getChild(i), label)?.let { return it }
            return null
        }
        fun hasVisibleText(node: AccessibilityNodeInfo?, label: String): Boolean {
            node ?: return false
            if (node.isVisibleToUser && (node.text?.toString()?.contains(label, ignoreCase = true) == true ||
                    node.contentDescription?.toString()?.contains(label, ignoreCase = true) == true)) return true
            return (0 until node.childCount).any { hasVisibleText(node.getChild(it), label) }
        }
        fun tap(x: Float, y: Float) {
            val now = SystemClock.uptimeMillis()
            val down = MotionEvent.obtain(now, now, MotionEvent.ACTION_DOWN, x, y, 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
            try {
                assertTrue("Touch down must reach the window", automation.injectInputEvent(down, true))
                SystemClock.sleep(80)
                val up = MotionEvent.obtain(now, SystemClock.uptimeMillis(), MotionEvent.ACTION_UP, x, y, 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
                try { assertTrue("Touch up must reach the window", automation.injectInputEvent(up, true)) }
                finally { up.recycle() }
            } finally { down.recycle() }
        }
        fun descriptionBounds(description: String): Rect {
            var target: AccessibilityNodeInfo? = null
            waitUntil("No clickable keyboard control: $description") {
                val roots = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                target = roots.firstNotNullOfOrNull { root -> findDescriptionNode(root, description) }
                target != null && findClickableAncestor(target) != null
            }
            val bounds = Rect().also { target!!.getBoundsInScreen(it) }
            assertTrue("Keyboard control must have visible bounds", bounds.width() > 0 && bounds.height() > 0)
            val ancestor = findClickableAncestor(target) ?: error("Keyboard control has no clickable ancestor: $description")
            val ancestorBounds = Rect().also { ancestor.getBoundsInScreen(it) }
            instrumentation.sendStatus(0, Bundle().apply {
                putString("speechkit_control_description", description)
                putString("speechkit_description_node_bounds", bounds.toShortString())
                putBoolean("speechkit_description_node_clickable", target!!.isClickable)
                putString("speechkit_clickable_ancestor_bounds", ancestorBounds.toShortString())
            })
            return bounds
        }
        fun clickDescription(description: String) {
            val bounds = descriptionBounds(description)
            tap(bounds.exactCenterX(), bounds.exactCenterY())
        }
        var activity: KeyboardJourneyActivity? = null
        var changedHardKeyboardSetting = false
        try {
            // Some virtual test devices report a hardware keyboard and suppress
            // the selected IME unless this user setting is enabled. Keep the
            // same installed IME path under test, then restore the device value.
            if (previousHardKeyboardSetting != "1") {
                shell("settings put secure show_ime_with_hard_keyboard 1")
                changedHardKeyboardSetting = true
            }
            val enableOutput = shell("ime enable $imeId").trim()
            val selectOutput = shell("ime set $imeId").trim()
            waitUntil("SpeechKit IME was not selected") {
                Settings.Secure.getString(context.contentResolver, Settings.Secure.DEFAULT_INPUT_METHOD) == imeId
            }
            instrumentation.sendStatus(0, Bundle().apply {
                putString("speechkit_ime_enable", enableOutput)
                putString("speechkit_ime_select", selectOutput)
                putString("speechkit_ime_selected", Settings.Secure.getString(context.contentResolver, Settings.Secure.DEFAULT_INPUT_METHOD))
            })
            activity = instrumentation.startActivitySync(
                Intent(context, KeyboardJourneyActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            ) as KeyboardJourneyActivity
            val editor = activity.editor
            val expected = "Synthetic keyboard journey note"
            waitUntil("Editor was not served by the selected input method") {
                onMain { editor.hasWindowFocus() && imm.isActive(editor) }
            }
            instrumentation.runOnMainSync { imm.showSoftInput(editor, InputMethodManager.SHOW_IMPLICIT) }
            val host = SpeechKitVoiceBridge.host as? InlineVoicePanel ?: error("SpeechKit IME host is missing")
            val field = InlineVoicePanel::class.java.getDeclaredField("currentService").apply { isAccessible = true }
            fun service(): InputMethodService? = field.get(host) as? InputMethodService
            waitUntil("SpeechKit keyboard did not open") { onMain { service()?.isInputViewShown == true } }
            val service = service() ?: error("SpeechKit IME service is missing")
            val decor = service.window?.window?.decorView ?: error("IME window is missing")
            val keys = decor.findViewById<View>(KeyboardR.id.main_keyboard_frame) ?: error("Keyboard frame is missing")
            val container = decor.findViewById<View>(KeyboardR.id.speechkit_voice_panel) ?: error("Voice host is missing")
            val strip = decor.findViewById<View>(KeyboardR.id.strip_container) ?: error("Suggestion strip is missing")
            val keyboardView = decor.findViewById<MainKeyboardView>(KeyboardR.id.keyboard_view) ?: error("Keyboard view is missing")
            waitUntil("Keyboard frame is not visible") { onMain { keys.isShown && keys.height > 0 && keys.width > 0 } }
            fun screenBounds(view: View): Rect {
                val point = IntArray(2)
                view.getLocationOnScreen(point)
                return Rect(point[0], point[1], point[0] + view.width, point[1] + view.height)
            }
            var originalStripBounds = Rect()
            var originalKeyboardBounds = Rect()
            var originalPadding = 0
            var originalHeight = 0
            var originalAlpha = 0f
            var originalAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_AUTO
            instrumentation.runOnMainSync {
                originalStripBounds = screenBounds(strip)
                originalKeyboardBounds = screenBounds(keyboardView)
                originalPadding = strip.paddingTop
                originalHeight = strip.layoutParams.height
                originalAlpha = keys.alpha
                originalAccessibility = keys.importantForAccessibility
            }
            val pinnedKeys = decor.findViewById<View>(KeyboardR.id.pinned_keys) ?: error("Pinned shortcut group is missing")
            fun openAgentFromPinnedShortcut() {
                var bounds = Rect()
                waitUntil("Pinned voice agent shortcut is not visible") {
                    onMain {
                        val shortcut = pinnedKeys.findViewWithTag<View>(ToolbarKey.SPEECHKIT_AGENT_DEEPGRAM)
                        if (shortcut != null && shortcut.isShown && shortcut.width > 0 && shortcut.height > 0) {
                            bounds = screenBounds(shortcut)
                            true
                        } else false
                    }
                }
                instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_pinned_agent_bounds", bounds.toShortString()) })
                tap(bounds.exactCenterX(), bounds.exactCenterY())
            }
            screenshot("keyboard-before.png")
            openAgentFromPinnedShortcut()
            waitUntil("Compact Opal did not occupy the existing suggestion strip") {
                onMain {
                    keys.isShown && keys.alpha == originalAlpha && container.isShown &&
                        originalStripBounds.contains(screenBounds(container)) && container.height == originalStripBounds.height() &&
                        screenBounds(strip) == originalStripBounds &&
                        strip.paddingTop == originalPadding && strip.layoutParams.height == originalHeight &&
                        keyboardView.isShown && screenBounds(keyboardView) == originalKeyboardBounds
                }
            }
            instrumentation.sendStatus(0, Bundle().apply {
                putString("speechkit_original_strip_bounds", originalStripBounds.toShortString())
                putString("speechkit_original_keyboard_bounds", originalKeyboardBounds.toShortString())
                putString("speechkit_compact_overlay_bounds", screenBounds(container).toShortString())
                putString("speechkit_compact_keyboard_bounds", screenBounds(keyboardView).toShortString())
                putInt("speechkit_strip_padding_top", originalPadding)
                putInt("speechkit_strip_layout_height", originalHeight)
            })
            // A fresh revision-zero install keeps the toolbar's first visit on
            // the setup surface. The explicit local-only Voice Agent action is
            // exercised below through Shortcuts and must open Hybrid directly.
            val longSetupMessage = context.getString(ImeR.string.speechkit_ime_agent_error_no_server)
            val roots = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
            assertTrue("Compact Opal must not show the long server setup error", roots.none { root ->
                root.findAccessibilityNodeInfosByText(longSetupMessage).any { it.isVisibleToUser }
            })
            screenshot("keyboard-compact.png")
            val keyPoint = IntArray(2)
            instrumentation.runOnMainSync {
                val key = keyboardView.keyboard?.getKey('x'.code) ?: error("The active keyboard has no x key")
                keyboardView.getLocationOnScreen(keyPoint)
                keyPoint[0] += keyboardView.paddingLeft + key.x + key.width / 2
                keyPoint[1] += keyboardView.paddingTop + key.y + key.height / 2
            }
            tap(keyPoint[0].toFloat(), keyPoint[1].toFloat())
            waitUntil("A real compact keyboard tap did not type into the active editor") {
                onMain { editor.text.toString() == expected + "x" }
            }
            clickDescription(context.getString(ImeR.string.speechkit_ime_agent_expand))
            screenshot("keyboard-after-expand-click.png")
            waitUntil("Expanded Opal did not replace the keyboard area") {
                onMain {
                    keys.isShown && keys.alpha == 0f &&
                        keys.importantForAccessibility == View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS &&
                        container.isShown && container.height > 0 && container.height <= keys.height
                }
            }
            val closeBounds = descriptionBounds(context.getString(ImeR.string.speechkit_ime_agent_action_end))
            var navigationBottom = 0
            instrumentation.runOnMainSync {
                navigationBottom = decor.rootWindowInsets?.getInsets(WindowInsets.Type.navigationBars())?.bottom ?: 0
            }
            val frame = automation.takeScreenshot() ?: error("Cannot measure the screen")
            val screenHeight = frame.height
            frame.recycle()
            assertTrue("The navigation inset must be reported on this device", navigationBottom > 0)
            assertTrue("The expanded close control must stay above the navigation bar", closeBounds.bottom <= screenHeight - navigationBottom)
            instrumentation.sendStatus(0, Bundle().apply {
                putInt("speechkit_navigation_bottom", navigationBottom)
                putInt("speechkit_expanded_close_bottom", closeBounds.bottom)
                putInt("speechkit_screen_height", screenHeight)
            })
            screenshot("keyboard-expanded.png")
            val panelPoint = IntArray(2)
            instrumentation.runOnMainSync {
                container.getLocationOnScreen(panelPoint)
                panelPoint[0] += 4
                panelPoint[1] += container.height / 2
            }
            tap(panelPoint[0].toFloat(), panelPoint[1].toFloat())
            instrumentation.waitForIdleSync()
            assertTrue("Tapping blank expanded space must not type an invisible key", onMain { editor.text.toString() == expected + "x" })
            clickDescription(context.getString(ImeR.string.speechkit_ime_agent_collapse))
            waitUntil("Collapsed Opal did not restore the existing strip and keys") {
                onMain {
                    keys.isShown && keys.alpha == originalAlpha && container.isShown &&
                        originalStripBounds.contains(screenBounds(container)) && container.height == originalStripBounds.height() &&
                        screenBounds(strip) == originalStripBounds &&
                        strip.paddingTop == originalPadding && strip.layoutParams.height == originalHeight &&
                        screenBounds(keyboardView) == originalKeyboardBounds
                }
            }
            screenshot("keyboard-collapsed.png")
            clickDescription(context.getString(ImeR.string.speechkit_ime_agent_action_end))
            waitUntil("Closing Opal did not restore the keyboard") {
                onMain {
                    keys.isShown && keys.alpha == originalAlpha && keys.importantForAccessibility == originalAccessibility &&
                        strip.paddingTop == originalPadding && strip.layoutParams.height == originalHeight &&
                        screenBounds(strip) == originalStripBounds && screenBounds(keyboardView) == originalKeyboardBounds
                }
            }
            screenshot("keyboard-restored.png")
            assertTrue("Only the tapped key may change the active editor", onMain { editor.text.toString() == expected + "x" && editor.hasFocus() })

            val shortcutTitle = context.getString(io.kombify.speechkit.R.string.shortcuts_title)
            val voiceShortcut = context.getString(io.kombify.speechkit.R.string.shortcut_voice_agent)
            val transcribeShortcut = context.getString(io.kombify.speechkit.R.string.shortcut_transcribe)
            val summarizeShortcut = context.getString(io.kombify.speechkit.R.string.speechkit_ime_shortcut_summarize)
            val rewriteShortcut = context.getString(io.kombify.speechkit.R.string.speechkit_ime_shortcut_rewrite)
            val correctShortcut = context.getString(io.kombify.speechkit.R.string.speechkit_ime_shortcut_correct)
            val connectionTitle = context.getString(io.kombify.speechkit.R.string.settings_connection_title)
            val selection = io.kombify.speechkit.net.StoredConnectionSelection(context)
            val gesturePolicy = storedPolicy.read(ConnectionProfile.Local)
            assertEquals("Gesture journey starts local-only", ExecutionMode.DEVICE_ONLY, gesturePolicy.mode)
            assertFalse("Local-only gesture journey must not permit network use", gesturePolicy.allowNetwork)
            assertTrue("Shortcuts page must render", host.showShortcuts(service))
            compose.onNodeWithText(shortcutTitle).assertExists()
            listOf(voiceShortcut, transcribeShortcut, summarizeShortcut, rewriteShortcut, correctShortcut).forEach {
                compose.onNodeWithText(it).assertExists()
            }
            screenshot("keyboard-shortcuts-idle.png")

            // A regular text action must open the foreground preview path; the
            // real model may be unavailable on this device, so cancel without
            // claiming generated-model evidence or inserting text.
            compose.onNodeWithText(summarizeShortcut).performClick()
            val previewTitle = context.getString(io.kombify.speechkit.R.string.keyboard_text_title)
            waitUntil("Summarize shortcut did not open its text action") {
                val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                windows.any { root -> hasVisibleText(root, previewTitle) }
            }
            assertEquals("Text action must retain local-only routing", gesturePolicy, storedPolicy.read(ConnectionProfile.Local))
            val cancelPreview = context.getString(io.kombify.speechkit.R.string.keyboard_text_cancel)
            val cancelPreviewBounds = Rect().also { target ->
                waitUntil("Text preview cancel action did not appear") {
                    val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                    val node = windows.firstNotNullOfOrNull { root -> findVisibleText(root, cancelPreview) }
                    if (node == null) false else { node.getBoundsInScreen(target); true }
                }
            }
            tap(cancelPreviewBounds.exactCenterX(), cancelPreviewBounds.exactCenterY())
            waitUntil("Cancel did not return to the keyboard") { onMain { service.isInputViewShown && editor.hasFocus() } }
            assertTrue("Could not reopen shortcuts after canceling text preview", host.showShortcuts(service))
            compose.onNodeWithText(shortcutTitle).assertExists()

            // The regular Voice Agent tap in local-only opens the on-device
            // Hybrid surface, never the endpoint setup screen.
            compose.onNodeWithText(voiceShortcut).performClick()
            val hybridTitle = context.getString(io.kombify.speechkit.R.string.hybrid_title)
            waitUntil("Local-only Voice Agent tap did not open Hybrid") {
                val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                windows.any { root -> hasVisibleText(root, hybridTitle) }
            }
            assertTrue("Local-only Voice Agent tap must not open connection setup", automation.windows.mapNotNull { it.root }
                .plus(listOfNotNull(automation.rootInActiveWindow)).none { root -> hasVisibleText(root, connectionTitle) })
            assertEquals("Local-only Voice Agent tap must preserve policy", gesturePolicy, storedPolicy.read(ConnectionProfile.Local))
            assertTrue("Could not return from local Hybrid", automation.performGlobalAction(android.accessibilityservice.AccessibilityService.GLOBAL_ACTION_BACK))
            waitUntil("Back did not return to the keyboard") { onMain { service.isInputViewShown && editor.hasFocus() } }
            assertTrue("Could not reopen shortcuts after returning from local Hybrid", host.showShortcuts(service))
            compose.onNodeWithText(shortcutTitle).assertExists()

            fun boundsFor(label: String): Rect {
                var result: Rect? = null
                waitUntil("Shortcut is not visible: $label") {
                    val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                    val node = windows.firstNotNullOfOrNull { root -> findVisibleText(root, label) }
                    if (node == null) false else { result = Rect().also(node::getBoundsInScreen); true }
                }
                return checkNotNull(result)
            }
            fun changedHoldEdgePixels(before: android.graphics.Bitmap, during: android.graphics.Bitmap, bounds: Rect): Int {
                val left = bounds.left.coerceAtLeast(0)
                val top = bounds.top.coerceAtLeast(0)
                val right = bounds.right.coerceAtMost(minOf(before.width, during.width))
                val bottom = bounds.bottom.coerceAtMost(minOf(before.height, during.height))
                var changed = 0
                for (y in top until bottom) for (x in left until right) {
                    val edge = x < left + 6 || x >= right - 6 || y < top + 6 || y >= bottom - 6
                    if (!edge) continue
                    val a = before.getPixel(x, y)
                    val b = during.getPixel(x, y)
                    if (kotlin.math.abs(android.graphics.Color.red(a) - android.graphics.Color.red(b)) > 24 ||
                        kotlin.math.abs(android.graphics.Color.green(a) - android.graphics.Color.green(b)) > 24 ||
                        kotlin.math.abs(android.graphics.Color.blue(a) - android.graphics.Color.blue(b)) > 24
                    ) changed++
                }
                return changed
            }

            val summarizeBounds = boundsFor(summarizeShortcut)
            val beforeHold = automation.takeScreenshot() ?: error("No idle shortcut screenshot")
            var duringHold: android.graphics.Bitmap? = null
            compose.onNodeWithText(summarizeShortcut).performTouchInput {
                down(center)
                advanceEventTime(720)
                duringHold = automation.takeScreenshot()
                cancel()
            }
            val held = checkNotNull(duringHold) { "No during-hold screenshot" }
            val ringPixels = changedHoldEdgePixels(beforeHold, held, summarizeBounds)
            assertTrue("650 ms hold ring was not visible at the shortcut edge (changed pixels=$ringPixels)", ringPixels > 20)
            val holdScreenshot = File(context.getExternalFilesDir("acceptance"), "keyboard-shortcut-hold-ring.png")
            holdScreenshot.outputStream().use { held.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
            instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_hold_ring_screenshot", holdScreenshot.absolutePath); putInt("speechkit_hold_ring_changed_pixels", ringPixels) })
            beforeHold.recycle()
            held.recycle()
            compose.onNodeWithText(shortcutTitle).assertExists()
            assertEquals("Canceling a hold must discard the action and keep local-only policy", gesturePolicy, storedPolicy.read(ConnectionProfile.Local))

            compose.onNodeWithText(summarizeShortcut).performTouchInput {
                down(center)
                advanceEventTime(720)
                up()
            }
            waitUntil("Completed hold did not route the alternative text action") {
                val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                windows.any { root -> hasVisibleText(root, previewTitle) }
            }
            assertEquals("Alternative invocation must retain the exclusive local destination", gesturePolicy, storedPolicy.read(ConnectionProfile.Local))
            val alternateCancel = Rect().also { target ->
                waitUntil("Alternative preview cancel action did not appear") {
                    val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                    val node = windows.firstNotNullOfOrNull { root -> findVisibleText(root, cancelPreview) }
                    if (node == null) false else { node.getBoundsInScreen(target); true }
                }
            }
            tap(alternateCancel.exactCenterX(), alternateCancel.exactCenterY())
            waitUntil("Alternative action did not return to the keyboard") { onMain { service.isInputViewShown && editor.hasFocus() } }
            assertTrue("Could not reopen shortcuts after canceling alternative action", host.showShortcuts(service))
            compose.onNodeWithText(shortcutTitle).assertExists()
            assertEquals("Gesture actions must not mutate the persisted preference", gesturePolicy, storedPolicy.read(ConnectionProfile.Local))
            assertEquals(PackageManager.PERMISSION_DENIED, context.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO))

            openAgentFromPinnedShortcut()
            clickDescription(context.getString(ImeR.string.speechkit_ime_agent_expand))
            waitUntil("Reopened Opal did not expand") { onMain { keys.alpha == 0f } }
            val setupText = context.getString(ImeR.string.speechkit_ime_agent_setup)
            var setup: AccessibilityNodeInfo? = null
            waitUntil("Setup action did not appear") {
                val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                setup = windows.firstNotNullOfOrNull { root -> findVisibleText(root, setupText) }
                setup != null
            }
            val setupBounds = Rect().also { setup!!.getBoundsInScreen(it) }
            assertTrue("Setup action must have visible bounds", setupBounds.width() > 0 && setupBounds.height() > 0)
            tap(setupBounds.exactCenterX(), setupBounds.exactCenterY())
            waitUntil("Setup did not open SpeechKit connection settings") {
                val windows = automation.windows.mapNotNull { it.root } + listOfNotNull(automation.rootInActiveWindow)
                windows.any { root -> hasVisibleText(root, connectionTitle) }
            }
            assertEquals("Setup must preserve the execution policy", previousPolicy, storedPolicy.read(ConnectionProfile.Local))
            assertEquals("Setup must not grant microphone permission", PackageManager.PERMISSION_DENIED,
                context.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO))
            instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_setup_destination", connectionTitle) })
            screenshot("keyboard-setup-settings.png")
        } catch (failure: Throwable) {
            // Preserve the original error if the window disappeared before capture.
            runCatching { screenshot("keyboard-failure.png") }
            throw failure
        } finally {
            instrumentation.runOnMainSync { activity?.finish() }
            if (!previous.isNullOrBlank() && previous.matches(Regex("[A-Za-z0-9_.$/]+"))) shell("ime set $previous")
            if (!wasEnabled) shell("ime disable $imeId")
            if (changedHardKeyboardSetting) {
                if (previousHardKeyboardSetting == null) shell("settings delete secure show_ime_with_hard_keyboard")
                else if (previousHardKeyboardSetting.matches(Regex("[0-9]+"))) {
                    shell("settings put secure show_ime_with_hard_keyboard $previousHardKeyboardSetting")
                }
            }
        }
    }
    @Test fun localSpeechSdkReadinessJourney() {
        assumeTrue(
            "Local SDK readiness is an explicit installed-device journey",
            InstrumentationRegistry.getArguments().getString("requireLocalSpeechSdk") == "true",
        )
        assertTrue("The local SDK journey requires the Hybrid module", BuildConfig.HYBRID_ENABLED)
        val context = InstrumentationRegistry.getInstrumentation().targetContext.applicationContext

        // A fixed 100 ms 8 kHz stereo WAV exercises the public core input
        // contract and the installed Android decoder without recording user audio.
        val sourceRate = 8_000
        val sourceChannels = 2
        val sourceFrames = 800
        val wav = ByteBuffer.allocate(44 + sourceFrames * sourceChannels * 2).order(ByteOrder.LITTLE_ENDIAN)
        fun fourCc(value: String) {
            value.forEach { wav.put(it.code.toByte()) }
        }
        fourCc("RIFF")
        wav.putInt(wav.capacity() - 8)
        fourCc("WAVE")
        fourCc("fmt ")
        wav.putInt(16)
        wav.putShort(1)
        wav.putShort(sourceChannels.toShort())
        wav.putInt(sourceRate)
        wav.putInt(sourceRate * sourceChannels * 2)
        wav.putShort((sourceChannels * 2).toShort())
        wav.putShort(16)
        fourCc("data")
        wav.putInt(sourceFrames * sourceChannels * 2)
        for (frame in 0 until sourceFrames) {
            val sample = (sin(2.0 * Math.PI * 440.0 * frame / sourceRate) * 12_000).toInt().toShort()
            wav.putShort(sample)
            wav.putShort((sample / 3).toShort())
        }

        var normalized = byteArrayOf()
        try {
            normalized = runBlocking {
                LocalSpeechAudio.Encoded(wav.array()).readPcm16(context)
            }
            // Decoding must return mono signed 16-bit PCM at the SDK's fixed
            // 16 kHz rate: 100 ms is 1,600 samples, or 3,200 bytes.
            assertEquals(1_600 * 2, normalized.size)
            assertTrue(normalized.size <= LocalSpeechLimits.MAX_PCM_BYTES)
            val samples = ByteBuffer.wrap(normalized).order(ByteOrder.LITTLE_ENDIAN).asShortBuffer()
            var peak = 0
            while (samples.hasRemaining()) peak = maxOf(peak, kotlin.math.abs(samples.get().toInt()))
            assertTrue("Stereo downmix must preserve the synthetic signal", peak in 2_000..10_000)
        } finally {
            normalized.fill(0)
            wav.array().fill(0)
        }

        val invalidPcm = runBlocking {
            runCatching { LocalSpeechAudio.Pcm16(byteArrayOf(1, 2, 3)).readPcm16(context) }.exceptionOrNull()
        }
        assertTrue("Odd PCM byte counts must be rejected as invalid input", invalidPcm is LocalSpeechFailure)
        assertEquals(LocalSpeechFailureKind.INPUT, (invalidPcm as LocalSpeechFailure).kind)
        val oversizedPcm = ByteArray(LocalSpeechLimits.MAX_PCM_BYTES + 2)
        val capacityFailure = try {
            runBlocking {
                runCatching { LocalSpeechAudio.Pcm16(oversizedPcm).readPcm16(context) }.exceptionOrNull()
            }
        } finally {
            oversizedPcm.fill(0)
        }
        assertTrue("Local PCM capacity must be rejected", capacityFailure is LocalSpeechFailure)
        assertEquals(
            "A local capacity miss may allow the caller to choose another execution destination",
            LocalSpeechFailureKind.UNAVAILABLE,
            (capacityFailure as LocalSpeechFailure).kind,
        )

        val deviceLocale = Locale.getDefault().toLanguageTag()
        val explicit = DictationSegmentOptions(language = deviceLocale)
        val multi = DictationSegmentOptions(language = LANGUAGE_MULTI)
        suspend fun readiness(adapter: LocalSpeechAdapter, options: DictationSegmentOptions): String = try {
            adapter.availability(options).name
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (failure: LocalSpeechFailure) {
            "ERROR_${failure.kind.name}"
        } catch (failure: Exception) {
            "ERROR_${failure.javaClass.simpleName}"
        } catch (failure: LinkageError) {
            "ERROR_${failure.javaClass.simpleName}"
        }
        val native = NativeLocalSpeechAdapter(context)
        val mlKit = MlKitLocalSpeechAdapter(context)
        val statuses = runBlocking {
            mapOf(
                "nativeExplicitLocale" to readiness(native, explicit),
                "nativeMulti" to readiness(native, multi),
                "mlKitExplicitLocale" to readiness(mlKit, explicit),
                "mlKitMulti" to readiness(mlKit, multi),
            )
        }
        assertEquals(
            "ML Kit must report multi-language as unsupported instead of silently using the device locale",
            LocalSpeechAvailability.LANGUAGE_UNSUPPORTED.name,
            statuses.getValue("mlKitMulti"),
        )
        val receipt = org.json.JSONObject()
            .put("inputSampleRateHz", sourceRate)
            .put("inputChannels", sourceChannels)
            .put("normalizedSampleRateHz", LocalSpeechLimits.SAMPLE_RATE)
            .put("normalizedChannels", 1)
            .put("normalizedSampleCount", normalized.size / 2)
            .put("invalidOddPcmRejected", true)
            .put("oversizedPcmReadiness", (capacityFailure as LocalSpeechFailure).kind.name)
            .put("deviceLocale", deviceLocale)
            .put("nativeExplicitLocaleReadiness", statuses.getValue("nativeExplicitLocale"))
            .put("nativeMultiReadiness", statuses.getValue("nativeMulti"))
            .put("mlKitExplicitLocaleReadiness", statuses.getValue("mlKitExplicitLocale"))
            .put("mlKitMultiReadiness", statuses.getValue("mlKitMulti"))
            .put("modelDownloadAttempted", false)
            .put("modelInferenceAttempted", false)
        val file = File(context.getExternalFilesDir("acceptance"), "local-speech-sdk-receipt.json")
        file.writeText(receipt.toString())
        InstrumentationRegistry.getInstrumentation().sendStatus(0, Bundle().apply {
            putString("speechkit_local_speech_sdk_receipt", file.absolutePath)
        })
    }

    @Test fun connectionSetupJourney() {
        assumeTrue("Connection setup is an explicit fresh-device journey", InstrumentationRegistry.getArguments().getString("requireConnectionSetup") == "true")
        assertTrue("Connection setup flag must be enabled", BuildConfig.CONNECTION_SETUP_ENABLED)
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val automation = instrumentation.uiAutomation
        val selection = io.kombify.speechkit.net.StoredConnectionSelection(context)
        val config = context.getSharedPreferences(io.kombify.speechkit.net.StoredServerProfile.PREFS_NAME, android.content.Context.MODE_PRIVATE)
        assertFalse("Journey needs a fresh synthetic installation", selection.hasSavedByok())
        assertNull("Journey needs a fresh synthetic server", io.kombify.speechkit.net.StoredServerProfile.load(context))
        val micBefore = context.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO)
        val activity = instrumentation.startActivitySync(Intent(context, io.kombify.speechkit.app.ui.MainActivity::class.java)
            .putExtra(io.kombify.speechkit.app.ui.MainActivity.EXTRA_OPEN_CONNECTION, true)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        fun waitFor(message: String, predicate: () -> Boolean) {
            val deadline = SystemClock.uptimeMillis() + 15_000
            while (SystemClock.uptimeMillis() < deadline) {
                if (predicate()) return
                SystemClock.sleep(100)
            }
            fail(message)
        }
        fun verifyChoice(label: String) {
            compose.onNodeWithText(label).performScrollTo().assertExists()
        }
        fun uiContains(label: String): Boolean = runCatching {
            compose.onNodeWithText(label).assertExists()
            true
        }.getOrDefault(false)
        fun uiHasField(label: String): Boolean = runCatching {
            compose.onNode(hasText(label) and hasSetTextAction()).assertExists()
            true
        }.getOrDefault(false)
        fun click(label: String) {
            compose.onNode(hasText(label) and hasClickAction()).performScrollTo().performClick()
        }
        fun selectMenu(menuLabel: String, option: String) {
            compose.onNode(hasContentDescription(menuLabel) and hasClickAction()).performScrollTo().performClick()
            compose.onNode(hasText(option) and hasClickAction() and hasAnyAncestor(isPopup())).performClick()
        }
        fun setField(label: String, value: String) {
            compose.onNode(hasText(label) and hasSetTextAction()).performScrollTo().performTextReplacement(value)
        }
        fun screenshot(name: String) {
            instrumentation.waitForIdleSync()
            val bitmap = automation.takeScreenshot() ?: error("No setup screenshot")
            val file = File(context.getExternalFilesDir("acceptance"), name)
            file.outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
            bitmap.recycle()
            instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_setup_screenshot", file.absolutePath) })
        }
        fun label(id: Int) = context.getString(id)
        val local = label(io.kombify.speechkit.R.string.connection_setup_local)
        val own = label(io.kombify.speechkit.R.string.connection_setup_own)
        val account = label(io.kombify.speechkit.R.string.connection_setup_account)
        val key = "synthetic-setup-credential-7a2f"
        val serverToken = "synthetic-server-credential-9b41"
        try {
            verifyChoice(local)
            verifyChoice(own)
            verifyChoice(account)
            screenshot("setup-three-modes.png")
            click(account)
            assertFalse("Viewing account setup must not sign in", io.kombify.speechkit.net.StoredServerProfile.cloudSelected(context))
            click(local)
            waitFor("Local mode not selected") { selection.snapshot().mode == io.kombify.speechkit.domain.ConnectionMode.ON_DEVICE }
            assertEquals(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, selection.snapshot().policy.mode)
            assertFalse(selection.snapshot().policy.allowNetwork)
            click(own)
            waitFor("Own-key setup controls were not rendered") {
                runCatching { compose.onNode(hasText(own) and isSelected()).assertExists() }.isSuccess &&
                    uiContains(label(io.kombify.speechkit.R.string.connection_setup_method)) &&
                    uiContains(label(io.kombify.speechkit.R.string.connection_setup_profile))
            }
            screenshot("setup-own-before-key.png")
            val profiles = io.kombify.speechkit.net.VoiceAgentCatalog.bundled().filter { it.isNativeRealtimeEndpoint }
            val gemini = profiles.first { it.adapterKind == "gemini_live" }
            val alternate = profiles.first { it.id != gemini.id }
            selectMenu(label(io.kombify.speechkit.R.string.connection_setup_profile), alternate.name)
            selectMenu(label(io.kombify.speechkit.R.string.connection_setup_profile), gemini.name)
            selectMenu(label(io.kombify.speechkit.R.string.connection_setup_method), label(io.kombify.speechkit.R.string.connection_setup_own_key))
            setField(label(io.kombify.speechkit.R.string.connection_setup_key), key)
            click(label(io.kombify.speechkit.R.string.settings_connection_save))
            waitFor("Gemini selection not saved") { selection.snapshot().mode == io.kombify.speechkit.domain.ConnectionMode.BYOK }
            val captured = selection.snapshot().profile as ConnectionProfile.Byok
            assertEquals(key, captured.apiKey)
            val selectedAgentChoice = io.kombify.speechkit.app.keyboard.KeyboardAgentPreferences.choice(context, captured)
            assertEquals("Saved catalog profile must stay bound to the selected Gemini connection", gemini.id, selectedAgentChoice.profileId)
            assertEquals(gemini.provider, selectedAgentChoice.provider)
            assertEquals(captured.model, selectedAgentChoice.model)
            assertTrue("Credential must not be in normal preferences", config.all.values.none { it?.toString()?.contains(key) == true })
            val custody = File(context.noBackupFilesDir, "speechkit-provider-keys")
            assertTrue("Encrypted credential custody missing", custody.listFiles()?.isNotEmpty() == true)
            assertTrue("Credential must not appear in custody plaintext", custody.listFiles().orEmpty().none { it.readBytes().toString(Charsets.ISO_8859_1).contains(key) })
            waitFor("Saved Gemini connection was not reflected in the settings UI") {
                uiContains(label(io.kombify.speechkit.R.string.connection_setup_key_saved)) &&
                    uiContains(label(io.kombify.speechkit.R.string.connection_setup_gemini)) &&
                    runCatching {
                        compose.onNode(
                            hasText(label(io.kombify.speechkit.R.string.settings_connection_save)) and isNotEnabled(),
                        ).assertExists()
                        compose.onNode(
                            hasText(label(io.kombify.speechkit.R.string.settings_connection_test)) and isEnabled(),
                        ).assertExists()
                    }.isSuccess
            }
            screenshot("setup-gemini-saved.png")
            val preference = label(io.kombify.speechkit.R.string.connection_setup_preference)
            val policies = listOf(
                io.kombify.speechkit.R.string.connection_setup_local_first to io.kombify.speechkit.domain.ExecutionMode.PREFER_DEVICE,
                io.kombify.speechkit.R.string.connection_setup_local_only to io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY,
                io.kombify.speechkit.R.string.connection_setup_cloud_only to io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY,
                io.kombify.speechkit.R.string.connection_setup_remote_first to io.kombify.speechkit.domain.ExecutionMode.PREFER_ENDPOINT,
            )
            for ((resource, expected) in policies) {
                selectMenu(preference, label(resource))
                waitFor("Execution preference did not change") { selection.snapshot().policy.mode == expected }
                assertEquals(expected != io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, selection.snapshot().policy.allowNetwork)
                assertEquals(io.kombify.speechkit.domain.ConnectionMode.BYOK, selection.snapshot().mode)
                assertEquals("Policy changes on the same connection must retain its catalog choice", selectedAgentChoice,
                    io.kombify.speechkit.app.keyboard.KeyboardAgentPreferences.choice(context, selection.snapshot().profile))
            }
            val persistedPolicy = selection.snapshot().policy
            val persistedMode = config.getString("connection_execution_mode", null)
            val persistedNetwork = config.getBoolean("connection_allow_network", false)
            val modeMatrix = listOf(
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.PREFER_ENDPOINT, true) to
                    (io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY to false),
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.PREFER_DEVICE, true) to
                    (io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY to true),
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, false) to
                    (io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY to false),
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY, true) to
                    (io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY to true),
            )
            for ((policy, expectedAlternative) in modeMatrix) {
                assertEquals("Normal invocation must preserve its selected mode", policy, policy.forInvocation(alternative = false))
                assertEquals(
                    "Alternative invocation must preserve exclusive destinations",
                    ExecutionPolicy(expectedAlternative.first, expectedAlternative.second),
                    policy.forInvocation(alternative = true),
                )
            }
            val exclusivePolicies = listOf(
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, false) to
                    ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY, true),
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY, true) to
                    ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, false),
            )
            for ((selectedPolicy, conflictingOverride) in exclusivePolicies) {
                val rejected = runCatching { selectedPolicy.restrictTo(conflictingOverride) }.isFailure
                assertTrue("A stale gesture must not override an exclusive execution mode", rejected)
            }
            assertEquals(
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, false),
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.PREFER_ENDPOINT, true)
                    .restrictTo(ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, false)),
            )
            assertEquals(
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY, true),
                ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.PREFER_DEVICE, true)
                    .restrictTo(ExecutionPolicy(io.kombify.speechkit.domain.ExecutionMode.ENDPOINT_ONLY, true)),
            )
            assertEquals("Policy calculations must not write preferences", persistedPolicy, selection.snapshot().policy)
            assertEquals(persistedMode, config.getString("connection_execution_mode", null))
            assertEquals(persistedNetwork, config.getBoolean("connection_allow_network", false))
            click(local)
            waitFor("Switch to local failed") { selection.snapshot().mode == io.kombify.speechkit.domain.ConnectionMode.ON_DEVICE }
            assertFalse("Captured remote profile must become stale", selection.isCurrent(captured))
            assertEquals(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, selection.snapshot().policy.mode)
            click(own)
            click(label(io.kombify.speechkit.R.string.connection_setup_use_saved))
            waitFor("Saved Gemini key was not restored") { selection.snapshot().mode == io.kombify.speechkit.domain.ConnectionMode.BYOK }
            assertEquals(key, (selection.snapshot().profile as ConnectionProfile.Byok).apiKey)
            selectMenu(label(io.kombify.speechkit.R.string.connection_setup_method), label(io.kombify.speechkit.R.string.connection_setup_server_method))
            setField(label(io.kombify.speechkit.R.string.settings_connection_url), "https://synthetic.invalid")
            setField(label(io.kombify.speechkit.R.string.settings_connection_token), serverToken)
            click(label(io.kombify.speechkit.R.string.settings_connection_save))
            waitFor("Server selection not saved") { selection.snapshot().mode == io.kombify.speechkit.domain.ConnectionMode.SELF_HOST }
            val serverProfile = selection.snapshot().profile as ConnectionProfile.Server
            assertEquals("https://synthetic.invalid", serverProfile.baseUrl)
            val serverAgentChoice = io.kombify.speechkit.app.keyboard.KeyboardAgentPreferences.choice(context, serverProfile)
            assertEquals("Switching connections must not reuse the previous catalog profile", "", serverAgentChoice.profileId)
            assertNull("Switching connections must not reuse the previous provider alias", serverAgentChoice.provider)
            assertEquals("", serverAgentChoice.model)
            assertTrue("Server token must not be in normal preferences", config.all.values.none { it?.toString()?.contains(serverToken) == true })
            screenshot("setup-server-saved.png")
            click(label(io.kombify.speechkit.R.string.settings_connection_clear))
            waitFor("Server was not cleared") { io.kombify.speechkit.net.StoredServerProfile.load(context) == null }
            assertEquals(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, selection.snapshot().policy.mode)
            assertEquals("Setup must not grant microphone permission", micBefore, context.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO))
        } catch (failure: Throwable) {
            runCatching { screenshot("setup-failed.png") }
            throw failure
        } finally {
            instrumentation.runOnMainSync { activity.finish() }
        }
    }

    @Test fun directGeminiVoiceJourney() {
        assumeTrue("Direct Gemini Live acceptance is explicitly enabled", InstrumentationRegistry.getArguments().getString("requireGeminiLive") == "true")
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val automation = instrumentation.uiAutomation
        val keyPath = "/sdcard/Download/speechkit-gemini-key.txt"
        val audioPath = "/sdcard/Download/speechkit-gemini-followup.pcm"
        fun removePrivateInputs() {
            automation.executeShellCommand("rm -f $keyPath $audioPath").close()
        }
        fun readPrivateInput(path: String): ByteArray =
            automation.executeShellCommand("cat $path").use { descriptor ->
                java.io.FileInputStream(descriptor.fileDescriptor).use { it.readBytes() }
            }
        val selection = io.kombify.speechkit.net.StoredConnectionSelection(context)
        var driver: io.kombify.speechkit.net.GeminiDirectVoiceAgentDriver? = null
        try {
            val keyBytes = readPrivateInput(keyPath)
            val pcm = readPrivateInput(audioPath)
            removePrivateInputs()
            val key = keyBytes.toString(Charsets.UTF_8).trim()
            keyBytes.fill(0)
            assertTrue("Development credential must be present", key.isNotBlank())
            assertTrue("Follow-up PCM fixture must be nonempty and 16-bit aligned", pcm.isNotEmpty() && pcm.size % 2 == 0)
            runBlocking {
                kotlinx.coroutines.withTimeout(90_000) {
                    selection.selectByok(key)
                    val profile = selection.snapshot().profile as ConnectionProfile.Byok
                    val activeDriver = io.kombify.speechkit.net.GeminiDirectVoiceAgentDriver(profile, context)
                    driver = activeDriver
                    val events = activeDriver.start()
                    val firstAudio = kotlinx.coroutines.CompletableDeferred<Unit>()
                    val providerInterrupted = kotlinx.coroutines.CompletableDeferred<Unit>()
                    val followupAudio = kotlinx.coroutines.CompletableDeferred<Unit>()
                    val followupListening = kotlinx.coroutines.CompletableDeferred<Unit>()
                    var audioFrames = 0
                    var interruptionEvents = 0
                    var followupStarted = false
                    var followupFrames = 0
                    var providerFailure = false
                    val collector = kotlinx.coroutines.CoroutineScope(kotlin.coroutines.coroutineContext).launch(start = kotlinx.coroutines.CoroutineStart.UNDISPATCHED) {
                        events.collect { event ->
                            activeDriver.accept(event)
                            when (event) {
                                is io.kombify.speechkit.net.VoiceAgentEvent.Audio -> if (event.pcm.isNotEmpty()) {
                                    audioFrames += 1
                                    if (!followupStarted) firstAudio.complete(Unit)
                                    else { followupFrames += 1; followupAudio.complete(Unit) }
                                }
                                io.kombify.speechkit.net.VoiceAgentEvent.Interrupted -> {
                                    interruptionEvents += 1
                                    // The first event is the local playback cut; the second is the provider acknowledgement.
                                    if (interruptionEvents >= 2) providerInterrupted.complete(Unit)
                                }
                                is io.kombify.speechkit.net.VoiceAgentEvent.State ->
                                    if (followupFrames > 0 && event.state == io.kombify.speechkit.net.VoiceAgentStates.LISTENING) followupListening.complete(Unit)
                                is io.kombify.speechkit.net.VoiceAgentEvent.Failure -> { providerFailure = true }
                                else -> Unit
                            }
                        }
                    }
                    try {
                        activeDriver.sendText("Count slowly from one to fifty.")
                        firstAudio.await()
                        activeDriver.cancelReply()
                        providerInterrupted.await()
                        followupStarted = true
                        var offset = 0
                        while (offset < pcm.size) {
                            val next = minOf(offset + 3200, pcm.size)
                            activeDriver.sendAudio(pcm.copyOfRange(offset, next))
                            offset = next
                            kotlinx.coroutines.delay(80)
                        }
                        activeDriver.endTurn()
                        followupAudio.await()
                        followupListening.await()
                        assertFalse("Provider reported a voice failure", providerFailure)
                        selection.selectLocal()
                        assertFalse("Previous direct profile must be stale", selection.isCurrent(profile))
                        assertEquals(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, selection.snapshot().policy.mode)
                        val staleDriver = io.kombify.speechkit.net.GeminiDirectVoiceAgentDriver(profile, context)
                        val staleAdmitted = try { staleDriver.start(); true }
                            catch (e: kotlinx.coroutines.CancellationException) { throw e }
                            catch (e: io.kombify.speechkit.domain.ExecutionFailure) {
                                assertEquals(io.kombify.speechkit.domain.ExecutionFailureKind.UNAVAILABLE, e.kind)
                                false
                            } finally { staleDriver.stop() }
                        assertFalse("A stale direct profile must not admit a remote session", staleAdmitted)
                        val receipt = org.json.JSONObject()
                            .put("source", "android_gemini_live")
                            .put("setup", true)
                            .put("initialAudioFrames", audioFrames - followupFrames)
                            .put("interruptionEvents", interruptionEvents)
                            .put("providerInterrupted", providerInterrupted.isCompleted)
                            .put("followupAudioFrames", followupFrames)
                            .put("followupListening", followupListening.isCompleted)
                            .put("staleProfileDenied", !staleAdmitted)
                        val file = File(context.getExternalFilesDir("acceptance"), "gemini-live-receipt.json")
                        file.writeText(receipt.toString())
                        instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_gemini_receipt", file.absolutePath) })
                    } finally {
                        collector.cancel()
                        pcm.fill(0)
                    }
                }
            }
        } finally {
            runCatching { removePrivateInputs() }
            runBlocking(kotlinx.coroutines.NonCancellable) {
                try { driver?.stop() } finally { selection.selectLocal() }
            }
        }
    }

    @Test fun foregroundModelEvidence() {
        assertTrue("Build the candidate with speechkitHybridEnabled=true", BuildConfig.HYBRID_ENABLED)
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val activity = instrumentation.startActivitySync(
            Intent(context, HybridActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
        try {
            instrumentation.waitForIdleSync()
            runBlocking {
                withTimeout(120_000) {
                    val model = HybridRuntime.model(context)
                    val status = model.readiness()
                    val evidence = Bundle().apply {
                        putString("speechkit_device", "${android.os.Build.MANUFACTURER} ${android.os.Build.MODEL}")
                        putString("speechkit_android", android.os.Build.VERSION.RELEASE)
                        putString("speechkit_model_status", status.name)
                    }
                    instrumentation.sendStatus(0, evidence)
                    if (InstrumentationRegistry.getArguments().getString("requireNano4") != "true") return@withTimeout
                    assertEquals("Nano 4 acceptance requires a provisioned device", DeviceReadiness.AVAILABLE, status)
                    val name = model.deviceModelName()
                    instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_model_name", name) })
                    assertTrue("SDK must identify Nano 4, observed: $name", Regex("(?i)(nano.*4|4.*nano)").containsMatchIn(name))
                    val result = model.generateOnDevice("Summarize in one sentence: SpeechKit saves notes on this device and can work offline.")
                    assertEquals(InferenceLocation.DEVICE, result.source)
                    assertTrue(result.text.isNotBlank())
                    instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_inference_source", result.source.name) })
                }
            }
        } finally {
            val screenshot = instrumentation.uiAutomation.takeScreenshot()
            assertNotNull("The foreground journey must produce a review screenshot", screenshot)
            val output = File(context.getExternalFilesDir("acceptance"), "hybrid-device.png")
            output.outputStream().use { screenshot.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
            screenshot.recycle()
            instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_screenshot", output.absolutePath) })
            instrumentation.runOnMainSync { activity.finish() }
        }
    }

    @Test fun firebaseSdkJourney() {
        assumeTrue("Firebase SDK acceptance is explicitly enabled", InstrumentationRegistry.getArguments().getString("requireFirebaseSdk") == "true")
        assertTrue("Firebase SDK acceptance requires a debug candidate", BuildConfig.DEBUG)
        assertTrue("Firebase SDK acceptance requires connection setup", BuildConfig.CONNECTION_SETUP_ENABLED)
        assertTrue("Firebase SDK acceptance requires hybrid inference", BuildConfig.HYBRID_ENABLED)
        assertTrue("Firebase SDK acceptance requires the configured Firebase cloud project", BuildConfig.FIREBASE_CLOUD_ENABLED)

        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val automation = instrumentation.uiAutomation
        val appCheckPath = "/sdcard/Download/speechkit-appcheck-token.txt"
        val audioPath = "/sdcard/Download/speechkit-gemini-followup.pcm"
        fun removePrivateInputs() {
            automation.executeShellCommand("rm -f $appCheckPath $audioPath").close()
        }
        fun readPrivateInput(path: String): ByteArray = automation.executeShellCommand("cat $path").use { descriptor ->
            java.io.FileInputStream(descriptor.fileDescriptor).use { it.readBytes() }
        }

        val config = io.kombify.speechkit.net.FirebaseAiProject(
            BuildConfig.FIREBASE_APPLICATION_ID,
            BuildConfig.FIREBASE_PROJECT_ID,
            BuildConfig.FIREBASE_API_KEY,
            allowCloud = true,
            debugAppCheck = true,
        )
        val firebase = ConnectionProfile.Firebase(
            applicationId = BuildConfig.FIREBASE_APPLICATION_ID,
            projectId = BuildConfig.FIREBASE_PROJECT_ID,
            apiKey = BuildConfig.FIREBASE_API_KEY,
            cloudModel = BuildConfig.FIREBASE_CLOUD_MODEL.ifBlank { "gemini-3.5-flash-lite" },
            debugAppCheck = true,
        )
        val selection = io.kombify.speechkit.net.StoredConnectionSelection(context)
        val args = InstrumentationRegistry.getArguments()
        val originalArgs = Bundle(args)
        var debugSecret = ""
        var tokenBytes = byteArrayOf()
        var pcm = byteArrayOf()
        var wav = byteArrayOf()
        try {
            tokenBytes = readPrivateInput(appCheckPath)
            pcm = readPrivateInput(audioPath)
            removePrivateInputs()
            debugSecret = tokenBytes.toString(Charsets.UTF_8).trim()
            tokenBytes.fill(0)
            assertTrue("App Check debug secret must be present", debugSecret.isNotBlank())
            assertTrue("Synthetic PCM fixture must be nonempty and 16-bit aligned", pcm.isNotEmpty() && pcm.size % 2 == 0)
            // The helper reads this Bundle in-process. The secret never enters
            // a runner command, shell command, receipt, or log.
            args.putString("firebaseAppCheckDebugSecret", debugSecret)
            InstrumentationRegistry.registerInstance(instrumentation, args)

            runBlocking { selection.selectLocal() }
            io.kombify.speechkit.net.StoredConnectionSelection.configureFirebase(false)
            val rejectedByBuildGate = try {
                runBlocking { selection.selectFirebase(firebase) }
                false
            } catch (_: IllegalStateException) {
                true
            }
            assertTrue("A disabled build gate must reject Firebase selection", rejectedByBuildGate)
            assertEquals("A rejected Firebase selection must retain local-only policy", io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, selection.snapshot().policy.mode)
            assertFalse(selection.snapshot().policy.allowNetwork)

            io.kombify.speechkit.net.StoredConnectionSelection.configureFirebase(
                BuildConfig.CONNECTION_SETUP_ENABLED && BuildConfig.HYBRID_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED,
            )
            runBlocking { selection.selectFirebase(firebase) }
            val profile = selection.snapshot().profile as ConnectionProfile.Firebase
            val appName = io.kombify.speechkit.net.FirebaseAiServices.appName(config)
            // Initialize the named app first; the test helper then replaces
            // its App Check factory with this process-only debug secret.
            io.kombify.speechkit.net.FirebaseAiServices.get(context, config)
            val firebaseApp = FirebaseApp.getInstance(appName)
            val debugAppCheck = DebugAppCheckTestHelper.fromInstrumentationArgs()

            debugAppCheck.withDebugProvider<Exception>(firebaseApp) {
                runBlocking {
                    withTimeout(240_000) {
                        val text = HybridRuntime.generate(
                            context,
                            profile,
                            "Summarize in one short English sentence: SpeechKit keeps notes local and supports explicitly selected cloud assistance.",
                            firebaseCloud = true,
                        ).value
                        assertEquals(HybridTextSource.FIREBASE_CLOUD, text.source)
                        assertTrue("Firebase cloud SDK returned no text", text.text.isNotBlank())

                        wav = io.kombify.speechkit.net.WavEncoder.pcm16ToWav(pcm, sampleRateHz = 16_000)
                        assertTrue("Synthetic WAV fixture exceeds Firebase transcription limits", wav.size <= io.kombify.speechkit.net.FirebaseAudioTranscriber.MAX_AUDIO_BYTES)
                        val transcript = io.kombify.speechkit.net.FirebaseAudioTranscriber(profile, context)
                            .transcribe(wav, "audio/wav")
                        assertTrue("Firebase cloud audio SDK returned no transcript", transcript.isNotBlank())

                        val activeDriver = io.kombify.speechkit.net.FirebaseVoiceAgentDriver(profile, context)
                        val events = activeDriver.start()
                        val firstAudio = kotlinx.coroutines.CompletableDeferred<Unit>()
                        val providerInterrupted = kotlinx.coroutines.CompletableDeferred<Unit>()
                        val followupAudio = kotlinx.coroutines.CompletableDeferred<Unit>()
                        val followupListening = kotlinx.coroutines.CompletableDeferred<Unit>()
                        val followupTranscript = kotlinx.coroutines.CompletableDeferred<Unit>()
                        var audioFrames = 0
                        var interruptionEvents = 0
                        var followupStarted = false
                        var followupFrames = 0
                        var inputTranscriptCharacters = 0
                        var followupTranscriptCharacters = 0
                        var providerFailure = false
                        val collector = kotlinx.coroutines.CoroutineScope(kotlin.coroutines.coroutineContext)
                            .launch(start = kotlinx.coroutines.CoroutineStart.UNDISPATCHED) {
                                events.collect { event ->
                                    activeDriver.accept(event)
                                    when (event) {
                                        is io.kombify.speechkit.net.VoiceAgentEvent.Audio -> if (event.pcm.isNotEmpty()) {
                                            audioFrames += 1
                                            if (!followupStarted) firstAudio.complete(Unit)
                                            else { followupFrames += 1; followupAudio.complete(Unit) }
                                        }
                                        io.kombify.speechkit.net.VoiceAgentEvent.Interrupted -> {
                                            interruptionEvents += 1
                                            if (interruptionEvents >= 2) providerInterrupted.complete(Unit)
                                        }
                                        is io.kombify.speechkit.net.VoiceAgentEvent.Transcript -> if (event.input && event.text.isNotBlank()) {
                                            inputTranscriptCharacters = maxOf(inputTranscriptCharacters, event.text.length)
                                            if (followupStarted) {
                                                followupTranscriptCharacters = maxOf(followupTranscriptCharacters, event.text.length)
                                                followupTranscript.complete(Unit)
                                            }
                                        }
                                        is io.kombify.speechkit.net.VoiceAgentEvent.State ->
                                            if (followupFrames > 0 && event.state == io.kombify.speechkit.net.VoiceAgentStates.LISTENING) followupListening.complete(Unit)
                                        is io.kombify.speechkit.net.VoiceAgentEvent.Failure -> providerFailure = true
                                        else -> Unit
                                    }
                                }
                            }
                        try {
                            activeDriver.sendText("Count slowly from one to fifty.")
                            firstAudio.await()
                            activeDriver.cancelReply()
                            providerInterrupted.await()
                            followupStarted = true
                            var offset = 0
                            while (offset < pcm.size) {
                                val next = minOf(offset + 3_200, pcm.size)
                                activeDriver.sendAudio(pcm.copyOfRange(offset, next))
                                offset = next
                                kotlinx.coroutines.delay(80)
                            }
                            activeDriver.endTurn()
                            followupAudio.await()
                            followupTranscript.await()
                            followupListening.await()
                            assertFalse("Firebase Live reported a provider failure", providerFailure)
                            assertTrue("Firebase Live must report input transcription", inputTranscriptCharacters > 0)
                            assertTrue("Real PCM follow-up must produce input transcription", followupTranscriptCharacters > 0)
                        } finally {
                            collector.cancel()
                            activeDriver.stop()
                        }

                        selection.selectLocal()
                        assertFalse("The previous Firebase profile must become stale", selection.isCurrent(profile))
                        assertEquals(io.kombify.speechkit.domain.ExecutionMode.DEVICE_ONLY, selection.snapshot().policy.mode)
                        val staleDriver = io.kombify.speechkit.net.FirebaseVoiceAgentDriver(profile, context)
                        val staleAdmitted = try { staleDriver.start(); true }
                            catch (e: kotlinx.coroutines.CancellationException) { throw e }
                            catch (_: Exception) { false }
                            finally { staleDriver.stop() }
                        assertFalse("A stale Firebase profile must not admit a remote session", staleAdmitted)

                        val receipt = org.json.JSONObject()
                            .put("textSource", text.source.name)
                            .put("audioTranscriptionSource", "FIREBASE_CLOUD")
                            .put("audioTranscriptionCharacters", transcript.length)
                            .put("liveSource", "FIREBASE_LIVE")
                            .put("initialAudioFrames", audioFrames - followupFrames)
                            .put("interruptionEvents", interruptionEvents)
                            .put("providerInterrupted", providerInterrupted.isCompleted)
                            .put("followupAudioFrames", followupFrames)
                            .put("followupInputTranscriptCharacters", followupTranscriptCharacters)
                            .put("followupListening", followupListening.isCompleted)
                            .put("staleProfileDenied", !staleAdmitted)
                            .put("buildGateDeniedFirebaseSelection", rejectedByBuildGate)
                        val file = File(context.getExternalFilesDir("acceptance"), "firebase-sdk-receipt.json")
                        file.writeText(receipt.toString())
                        instrumentation.sendStatus(0, Bundle().apply { putString("speechkit_firebase_sdk_receipt", file.absolutePath) })
                    }
                }
            }
        } finally {
            runCatching { removePrivateInputs() }
            debugSecret = ""
            tokenBytes.fill(0)
            pcm.fill(0)
            wav.fill(0)
            InstrumentationRegistry.registerInstance(instrumentation, originalArgs)
            io.kombify.speechkit.net.StoredConnectionSelection.configureFirebase(
                BuildConfig.CONNECTION_SETUP_ENABLED && BuildConfig.HYBRID_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED,
            )
            runBlocking(kotlinx.coroutines.NonCancellable) { runCatching { selection.selectLocal() } }
        }
    }

    @Test fun configuredFirebaseCloudReturnsAttributedResult() {
        assumeTrue("Cloud acceptance is an explicit test run", InstrumentationRegistry.getArguments().getString("requireCloud") == "true")
        assertTrue("Cloud acceptance requires a debug candidate", BuildConfig.DEBUG)
        assertTrue("Cloud acceptance requires an enabled project", BuildConfig.FIREBASE_CLOUD_ENABLED)
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val config = io.kombify.speechkit.net.FirebaseAiProject(
            BuildConfig.FIREBASE_APPLICATION_ID,
            BuildConfig.FIREBASE_PROJECT_ID,
            BuildConfig.FIREBASE_API_KEY,
            allowCloud = true,
            debugAppCheck = BuildConfig.DEBUG,
        )
        val model = HybridRuntime.model(instrumentation.targetContext, cloud = true)
        val app = FirebaseApp.getInstance(io.kombify.speechkit.net.FirebaseAiServices.appName(config))
        DebugAppCheckTestHelper.fromInstrumentationArgs().withDebugProvider<Exception>(app) {
            runBlocking {
                withTimeout(90_000) {
                    val result = model.generateFirebaseCloud("Summarize in one short English sentence: SpeechKit keeps local notes and allows explicitly selected cloud assistance.")
                    assertEquals(InferenceLocation.FIREBASE_CLOUD, result.source)
                    assertTrue(result.text.isNotBlank())
                    instrumentation.sendStatus(0, Bundle().apply {
                        putString("speechkit_cloud_source", result.source.name)
                        putString("speechkit_cloud_model", BuildConfig.FIREBASE_CLOUD_MODEL)
                        putInt("speechkit_cloud_result_characters", result.text.length)
                    })
                }
            }
        }
    }

}
