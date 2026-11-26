package io.kombify.speechkit.app.context

import android.content.Intent
import android.os.SystemClock
import android.view.accessibility.AccessibilityNodeInfo
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.test.platform.app.InstrumentationRegistry
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.app.context.capture.ScreenCaptureActivity
import io.kombify.speechkit.domain.ExecutionMode
import io.kombify.speechkit.net.StoredConnectionSelection
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test

/** The test source is a fictional foreground chat, and consent is the real system dialog. */
class ScreenCaptureSuccessJourney {
    @get:Rule val compose = createEmptyComposeRule()

    @Test fun captureVisibleSyntheticChatAfterSystemConsent() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue("Projection requires an explicit interactive run", args.getString("requireContextCapture") == "true")
        assumeTrue("Context and hybrid actions must be enabled", BuildConfig.CONTEXT_ACTIONS_ENABLED && BuildConfig.HYBRID_ENABLED)
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val target = instrumentation.targetContext
        val sender = instrumentation.context
        val route = StoredConnectionSelection(target).snapshot()
        assertEquals(ExecutionMode.DEVICE_ONLY, route.policy.mode)
        assertFalse(route.policy.allowNetwork)
        val intent = Intent(sender, SyntheticChatActivity::class.java)
            .putExtra("target_package", target.packageName).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        sender.startActivity(intent)
        val previewMonitor = instrumentation.addMonitor(ContentAnalysisActivity::class.java.name, null, false)
        try {
            fun screenshot(name: String) {
                val bitmap = requireNotNull(instrumentation.uiAutomation.takeScreenshot())
                try {
                    val file = java.io.File(requireNotNull(target.getExternalFilesDir("acceptance")), name)
                    file.outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
                    instrumentation.sendStatus(0, android.os.Bundle().apply { putString("speechkit_context_screenshot", file.absolutePath) })
                } finally { bitmap.recycle() }
            }
            fun root(): AccessibilityNodeInfo? {
                val node = instrumentation.uiAutomation.rootInActiveWindow ?: return null
                val owner = node.packageName?.toString().orEmpty()
                return node.takeIf { owner.contains("systemui") || owner.contains("permissioncontroller") }
            }
            fun findExact(node: AccessibilityNodeInfo?, values: Set<String>): AccessibilityNodeInfo? {
                node ?: return null
                if (node.text?.toString() in values) return node
                for (i in 0 until node.childCount) findExact(node.getChild(i), values)?.let { return it }
                return null
            }
            fun click(node: AccessibilityNodeInfo): Boolean {
                var current: AccessibilityNodeInfo? = node
                while (current != null) {
                    if (current.isClickable && current.performAction(AccessibilityNodeInfo.ACTION_CLICK)) return true
                    current = current.parent
                }
                return false
            }
            fun waitNode(timeout: Long, lookup: () -> AccessibilityNodeInfo?): AccessibilityNodeInfo? {
                val deadline = SystemClock.uptimeMillis() + timeout
                while (SystemClock.uptimeMillis() < deadline) {
                    lookup()?.let { return it }
                    SystemClock.sleep(100)
                }
                return null
            }
            val chatDeadline = SystemClock.uptimeMillis() + 10_000
            var chatVisible = false
            while (SystemClock.uptimeMillis() < chatDeadline) {
                val node = instrumentation.uiAutomation.rootInActiveWindow
                if (node?.packageName?.toString() == sender.packageName &&
                    node.findAccessibilityNodeInfosByText("Synthetic chat").isNotEmpty()) { chatVisible = true; break }
                SystemClock.sleep(100)
            }
            assertTrue("The fictional chat must be visible before capture", chatVisible)
            screenshot("context-synthetic-chat.png")
            instrumentation.runOnMainSync { ContextActions.capture(target, route.policy, route.revision) }
            val consent = waitNode(10_000) {
                root()?.takeIf { it.findAccessibilityNodeInfosByText("SpeechKit").isNotEmpty() }
            }
            assertNotNull("SpeechKit system projection consent did not appear", consent)
            instrumentation.waitForIdleSync()
            SystemClock.sleep(700)
            screenshot("context-capture-consent.png")
            // Android 14+ may default to one app. Select the entire screen only within this verified dialog.
            val choice = findExact(consent, setOf("A single app", "Eine App", "Einzelne App"))
            if (choice != null) {
                assertTrue("Could not open capture scope", click(choice))
                val entire = waitNode(5_000) { findExact(root(), setOf("Entire screen", "Gesamter Bildschirm", "Gesamten Bildschirm")) }
                assertNotNull("Full-screen capture option was unavailable", entire)
                assertTrue("Could not select full-screen capture", click(requireNotNull(entire)))
            }
            val start = waitNode(5_000) {
                val dialog = root()
                if (dialog?.findAccessibilityNodeInfosByText("SpeechKit")?.isNotEmpty() == true)
                    findExact(dialog, setOf("Start now", "Jetzt starten", "Bildschirm teilen")) else null
            }
            assertNotNull("Verified SpeechKit consent did not offer its capture confirmation", start)
            assertTrue("Could not approve SpeechKit projection", click(requireNotNull(start)))
            val preview = instrumentation.waitForMonitorWithTimeout(previewMonitor, 30_000)
            assertNotNull("Capture did not reach the analysis preview", preview)
            try {
                fun label(name: String): String {
                    val id = target.resources.getIdentifier(name, "string", target.packageName)
                    check(id != 0) { "Missing context string: $name" }
                    return target.getString(id)
                }
                compose.waitUntil(15_000) { compose.onAllNodes(hasText(label("context_visible_screen"))).fetchSemanticsNodes().isNotEmpty() }
                compose.onNodeWithText(label("context_visible_screen")).assertIsDisplayed()
                compose.waitUntil(15_000) { compose.onAllNodes(hasContentDescription(label("context_image_preview"))).fetchSemanticsNodes().isNotEmpty() }
                compose.onNodeWithContentDescription(label("context_image_preview")).assertIsDisplayed()
                instrumentation.waitForIdleSync()
                SystemClock.sleep(700)
                screenshot("context-screen-preview.png")
            } finally { instrumentation.runOnMainSync { preview?.finish() } }
        } finally {
            instrumentation.removeMonitor(previewMonitor)
            instrumentation.runOnMainSync { ScreenCaptureActivity.cancel(target) }
            if (instrumentation.uiAutomation.rootInActiveWindow?.packageName?.toString() == sender.packageName) {
                instrumentation.sendKeyDownUpSync(android.view.KeyEvent.KEYCODE_BACK)
            }
        }
    }
}
