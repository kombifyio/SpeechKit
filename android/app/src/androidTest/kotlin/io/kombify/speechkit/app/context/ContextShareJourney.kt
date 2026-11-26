package io.kombify.speechkit.app.context

import android.app.Activity
import android.content.ClipData
import android.content.ComponentName
import android.content.Intent
import android.net.Uri
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasContentDescription
import androidx.compose.ui.test.hasText

import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.test.platform.app.InstrumentationRegistry
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.domain.ExecutionMode
import io.kombify.speechkit.net.StoredConnectionSelection
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test

/** Installed share target journeys. Run on a dedicated, fresh local-only test installation. */
class ContextShareJourney {
    @get:Rule val compose = createEmptyComposeRule()
    private val instrumentation get() = InstrumentationRegistry.getInstrumentation()
    private val target get() = instrumentation.targetContext
    private val sender get() = instrumentation.context
    private val chat = "Alex: Meet at the library at three.\nSam: I'll bring the blue book."

    private fun label(name: String): String {
        val id = target.resources.getIdentifier(name, "string", target.packageName)
        check(id != 0) { "Missing context string: $name" }
        return target.getString(id)
    }

    private fun enabledLocalInstall() {
        val enabled = BuildConfig::class.java.getField("CONTEXT_ACTIONS_ENABLED").getBoolean(null)
        assumeTrue("Build with -PspeechkitContextActionsEnabled=true", enabled)
        val policy = StoredConnectionSelection(target).snapshot().policy
        assertEquals("Use a dedicated local-only installation", ExecutionMode.DEVICE_ONLY, policy.mode)
        assertFalse("The journey must not permit a remote destination", policy.allowNetwork)
    }

    private fun fixture(name: String): Uri = Uri.parse("content://${sender.packageName}.contextfixtures/$name")

    private fun launch(type: String, text: String? = null, uri: Uri? = null): Activity {
        val activityClass = "io.kombify.speechkit.app.context.ContentAnalysisActivity"
        val resolution = Intent(Intent.ACTION_SEND).setType(type).setPackage(target.packageName)
        val resolved = target.packageManager.queryIntentActivities(resolution, android.content.pm.PackageManager.MATCH_DEFAULT_ONLY)
        assertTrue("The system share resolver must find SpeechKit", resolved.any { it.activityInfo.name == activityClass })
        val monitor = instrumentation.addMonitor(activityClass, null, false)
        // The test APK, which owns the private provider, issues the real one-shot URI grant.
        val fixtureName = uri?.lastPathSegment ?: "text"
        require(text == null || text == chat)
        sender.startActivity(Intent(sender, SyntheticChatActivity::class.java).apply {
            putExtra("target_package", target.packageName)
            putExtra("auto_share", fixtureName)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK)
        })
        val activity = instrumentation.waitForMonitorWithTimeout(monitor, 10_000)
        instrumentation.removeMonitor(monitor)
        assertNotNull("The share target did not open", activity)
        instrumentation.runOnMainSync {
            activity?.window?.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
        return requireNotNull(activity)
    }

    private fun waitForLabel(name: String) {
        val expected = label(name)
        compose.waitUntil(15_000) { compose.onAllNodes(hasText(expected)).fetchSemanticsNodes(atLeastOneRootRequired = false).isNotEmpty() }
        compose.onNodeWithText(expected).assertIsDisplayed()
    }

    private fun screenshot(name: String) {
        instrumentation.waitForIdleSync()
        android.os.SystemClock.sleep(700) // Let the Android window transition finish.
        val bitmap = requireNotNull(instrumentation.uiAutomation.takeScreenshot())
        try {
            val file = File(requireNotNull(target.getExternalFilesDir("acceptance")), name)
            file.outputStream().use { bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
            instrumentation.sendStatus(0, android.os.Bundle().apply { putString("speechkit_context_screenshot", file.absolutePath) })
        } finally { bitmap.recycle() }
    }

    @Test fun sharedTextCanBeEditedAndCanceledWithoutProcessing() {
        enabledLocalInstall()
        val activity = launch("text/plain", text = chat)
        try {
            waitForLabel("context_title")
            waitForLabel("context_shared_text")
            compose.onNodeWithText(chat).assertIsDisplayed().performTextReplacement("Synthetic edited message")
            compose.onNodeWithText(label("context_result")).assertDoesNotExist()
            screenshot("context-text-preview.png")
            compose.onNodeWithText(label("context_cancel")).performClick()
            compose.waitUntil(5_000) { activity.isFinishing || activity.isDestroyed }
        } finally { instrumentation.runOnMainSync { activity.finish() } }
        val reopened = launch("text/plain", text = chat)
        try {
            waitForLabel("context_title")
            compose.onNodeWithText(chat).assertIsDisplayed()
            compose.onNodeWithText("Synthetic edited message").assertDoesNotExist()
            compose.onNodeWithText(label("context_result")).assertDoesNotExist()
        } finally { instrumentation.runOnMainSync { reopened.finish() } }
    }

    @Test fun sharedImagePreviewsAndLocalOnlyFailureDoesNotInventResult() {
        enabledLocalInstall()
        val activity = launch("image/png", uri = fixture("chat.png"))
        try {
            waitForLabel("context_shared_image")
            compose.waitUntil(15_000) { compose.onAllNodes(hasContentDescription(label("context_image_preview"))).fetchSemanticsNodes(atLeastOneRootRequired = false).isNotEmpty() }
            compose.onNodeWithContentDescription(label("context_image_preview")).assertIsDisplayed()
            compose.onNodeWithText(label("context_result")).assertDoesNotExist()
            screenshot("context-image-preview.png")
            compose.onNodeWithText(label("context_summarize")).performClick()
            waitForLabel("context_failed")
            compose.onNodeWithText(label("context_result")).assertDoesNotExist()
            val policy = StoredConnectionSelection(target).snapshot().policy
            assertEquals(ExecutionMode.DEVICE_ONLY, policy.mode)
            assertFalse(policy.allowNetwork)
        } finally { instrumentation.runOnMainSync { activity.finish() } }
    }

    @Test fun sharedSyntheticSilenceOffersTranscriptionWithoutRunningIt() {
        enabledLocalInstall()
        val activity = launch("audio/wav", uri = fixture("silence.wav"))
        try {
            waitForLabel("context_shared_audio")
            waitForLabel("context_audio_ready")
            compose.onNodeWithText(label("context_transcribe")).assertIsDisplayed()
            compose.onNodeWithText(label("context_result")).assertDoesNotExist()
            compose.onNodeWithText(label("context_cancel")).performClick()
            compose.waitUntil(5_000) { activity.isFinishing || activity.isDestroyed }
        } finally { instrumentation.runOnMainSync { activity.finish() } }
    }

    @Test fun missingAndOversizedSharesFailClosed() {
        enabledLocalInstall()
        for (name in listOf("missing.png", "oversized.png")) {
            val activity = launch("image/png", uri = fixture(name))
            try {
                waitForLabel("context_invalid_input")
                compose.onNodeWithText(label("context_result")).assertDoesNotExist()
            } finally { instrumentation.runOnMainSync { activity.finish() } }
        }
    }

    /** Opt-in acceptance with synthetic inputs and the actual selected processing route. */
    @Test fun contextProcessingJourney() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue("Context processing is opt-in", args.getString("requireContextProcessing") == "true")
        assertTrue("Context actions must be enabled", BuildConfig.CONTEXT_ACTIONS_ENABLED && BuildConfig.HYBRID_ENABLED)
        val destination = args.getString("contextProcessingDestination") ?: "selected"
        require(destination == "local" || destination == "selected")
        val selection = StoredConnectionSelection(target)
        if (destination == "local") {
            kotlinx.coroutines.runBlocking { selection.selectLocal() }
            val policy = selection.snapshot().policy
            assertEquals(ExecutionMode.DEVICE_ONLY, policy.mode)
            assertFalse(policy.allowNetwork)
        }

        fun runCases() {
            val rows = org.json.JSONArray()
            val receipt = org.json.JSONObject().put("destination", destination).put("cases", rows)
            val file = File(requireNotNull(target.getExternalFilesDir("acceptance")), "context-processing-$destination.json")
            fun save() {
                file.writeText(receipt.toString(2))
                instrumentation.sendStatus(0, android.os.Bundle().apply { putString("speechkit_context_processing_receipt", file.absolutePath) })
            }
            fun probeLocalReadiness() {
                val readiness = org.json.JSONObject()
                receipt.put("localReadiness", readiness)
                kotlinx.coroutines.runBlocking {
                    suspend fun probe(name: String, timeoutMs: Long = 15_000, block: suspend () -> String) {
                        val value = try { kotlinx.coroutines.withTimeout(timeoutMs) { block() } }
                        catch (failure: io.kombify.speechkit.domain.ExecutionFailure) { "ERROR_${failure.kind.name}" }
                        catch (failure: io.kombify.speechkit.stt.LocalSpeechFailure) { "ERROR_${failure.kind.name}" }
                        catch (failure: Exception) { "ERROR_${failure.javaClass.simpleName}" }
                        catch (failure: LinkageError) { "ERROR_${failure.javaClass.simpleName}" }
                        readiness.put(name, value)
                        save()
                    }
                    val locale = java.util.Locale.getDefault()
                    readiness.put("locale", locale.toLanguageTag())
                    probe("generation") { io.kombify.speechkit.app.hybrid.HybridRuntime.model(target).readiness().name }
                    probe("nativeAudio") {
                        io.kombify.speechkit.stt.NativeLocalSpeechAdapter(target).availability(
                            io.kombify.speechkit.stt.streaming.DictationSegmentOptions(language = locale.toLanguageTag())).name
                    }
                    for (advanced in listOf(true, false)) {
                        val key = if (advanced) "importedAudioAdvanced" else "importedAudioBasic"
                        probe(key, 120_000) {
                            io.kombify.speechkit.hybrid.ImportedAudio(locale, advanced).use { audio ->
                                val initial = audio.readiness()
                                readiness.put("${key}Initial", initial.name)
                                if (initial == io.kombify.speechkit.hybrid.DeviceReadiness.DOWNLOADABLE) {
                                    readiness.put("${key}DownloadAttempted", true)
                                    save()
                                    audio.download { }
                                }
                                audio.readiness().name
                            }
                        }
                    }
                }
                save()
            }
            var readinessProbed = false
            val cases = listOf(
                Triple("text-summary", "text", "context_summarize"),
                Triple("image-summary", "chat.png", "context_summarize"),
                Triple("audio-transcription", "context-followup.wav", "context_transcribe"),
                Triple("audio-summary", "context-followup.wav", "context_summarize"),
            )
            val failures = mutableListOf<String>()
            val requireAudioPermission = args.getString("requireContextAudioPermission") == "true"
            require(!requireAudioPermission || destination == "local")
            var permissionClicked = false
            for ((name, input, action) in cases) {
                val row = org.json.JSONObject().put("action", action.removePrefix("context_"))
                    .put("input", input).put("status", "started")
                rows.put(row); save()
                var activity: Activity? = null
                try {
                    val type = when (input) { "text" -> "text/plain"; "chat.png" -> "image/png"; else -> "audio/wav" }
                    activity = launch(type, text = if (input == "text") chat else null, uri = if (input == "text") null else fixture(input))
                    waitForLabel(when (input) { "text" -> "context_shared_text"; "chat.png" -> "context_shared_image"; else -> "context_shared_audio" })
                    if (destination == "local" && !readinessProbed) {
                        readinessProbed = true
                        probeLocalReadiness()
                    }
                    if (name == "audio-transcription" && requireAudioPermission) {
                        check(target.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO) != android.content.pm.PackageManager.PERMISSION_GRANTED) {
                            "Audio permission must be absent before the permission journey"
                        }
                        row.put("permissionInitiallyGranted", false)
                    }
                    compose.onNodeWithText(label(action)).performScrollTo().performClick()
                    compose.waitUntil(120_000) {
                        if (args.getString("requireContextAudioPermission") == "true") {
                            val root = instrumentation.uiAutomation.rootInActiveWindow
                            val owner = root?.packageName?.toString().orEmpty()
                            if (owner.endsWith("permissioncontroller") && root?.findAccessibilityNodeInfosByText("SpeechKit")?.isNotEmpty() == true) {
                                val allow = root.findAccessibilityNodeInfosByViewId("com.android.permissioncontroller:id/permission_allow_foreground_only_button").firstOrNull()
                                    ?: root.findAccessibilityNodeInfosByViewId("$owner:id/permission_allow_foreground_only_button").firstOrNull()
                                if (allow != null) {
                                    screenshot("context-audio-permission.png")
                                    check(allow.performAction(android.view.accessibility.AccessibilityNodeInfo.ACTION_CLICK))
                                    permissionClicked = true
                                    row.put("permissionAllowClicked", true)
                                    save()
                                }
                                return@waitUntil false
                            }
                        }
                        val window = instrumentation.uiAutomation.rootInActiveWindow
                        if (window?.packageName?.toString()?.endsWith("permissioncontroller") == true) return@waitUntil false
                        compose.onAllNodes(hasText(label("context_result"))).fetchSemanticsNodes(atLeastOneRootRequired = false).isNotEmpty() ||
                            compose.onAllNodes(hasText(label("context_failed"))).fetchSemanticsNodes(atLeastOneRootRequired = false).isNotEmpty()
                    }
                    val failed = compose.onAllNodes(hasText(label("context_failed"))).fetchSemanticsNodes(atLeastOneRootRequired = false).isNotEmpty()
                    if (failed) {
                        row.put("status", "error").put("source", org.json.JSONObject.NULL)
                            .put("result", label("context_failed"))
                        compose.onNodeWithText(label("context_failed")).performScrollTo()
                        if (action == "context_transcribe" && args.getString("requireContextAudioPermission") == "true") failures.add("$name: local transcription failed after permission flow")
                        if (destination == "selected") failures.add("$name: analysis failed")
                    } else {
                        // Collect visible semantic text after the Result heading: the screen contains only synthetic input.
                        val all = compose.onAllNodes(androidx.compose.ui.test.hasText("", substring = true))
                            .fetchSemanticsNodes().flatMap { node ->
                                node.config.getOrNull(SemanticsProperties.Text)?.map { it.text } ?: emptyList()
                            }
                        val heading = all.indexOf(label("context_result"))
                        check(heading >= 0) { "Missing result heading" }
                        val sourceNames = listOf("context_audio_local", "context_audio_cloud", "keyboard_text_source_device",
                            "keyboard_text_source_firebase", "keyboard_text_source_endpoint", "keyboard_text_source_gemini")
                        val sources = sourceNames.filter { all.contains(label(it)) }
                        val answer = all.drop(heading + 1).firstOrNull { value ->
                            value.isNotBlank() && sourceNames.none { label(it) == value } &&
                                value != label("hybrid_copy") && value != label("context_cancel")
                        }
                        check(!answer.isNullOrBlank() && sources.isNotEmpty()) { "Result or attribution missing" }
                        if (action == "context_transcribe") {
                            check(answer.contains("Bibliothek", ignoreCase = true) && answer.contains("Buch", ignoreCase = true)) {
                                "The spoken library/book facts were not preserved"
                            }
                        }
                        val cloud = sources.any { it in listOf("context_audio_cloud", "keyboard_text_source_firebase", "keyboard_text_source_endpoint", "keyboard_text_source_gemini") }
                        if (destination == "selected") {
                            if (input == "context-followup.wav") check("context_audio_cloud" in sources) { "Cloud audio source missing" }
                            if (action == "context_summarize") check("keyboard_text_source_firebase" in sources) { "Firebase source missing" }
                        }
                        if (name == "audio-transcription" && requireAudioPermission) {
                            check(permissionClicked && target.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO) == android.content.pm.PackageManager.PERMISSION_GRANTED) { "System audio permission was not granted by the test" }
                            check("context_audio_local" in sources) { "Local transcription source missing" }
                            row.put("permissionGranted", true)
                        }
                        row.put("status", "success").put("source", if (cloud) "cloud" else "device")
                            .put("sourceLabels", org.json.JSONArray(sources.map { label(it) })).put("result", answer)
                        compose.onNodeWithText(label("context_result")).performScrollTo()
                    }
                    screenshot("context-processing-$destination-$name.png")
                } catch (failure: Exception) {
                    row.put("status", "error").put("result", failure.javaClass.simpleName)
                    failures.add("$name: ${failure.javaClass.simpleName}")
                } finally {
                    activity?.let { current -> instrumentation.runOnMainSync { current.finish() } }
                    save()
                }
            }
            assertTrue("Processing journeys failed: ${failures.joinToString()}", failures.isEmpty())
        }

        // A separately authorized test run may supply private setup in the app's private files.
        // The input is removed before initialization; it never enters a receipt or runner command.
        val privateInput = File(target.filesDir, "context-test-input.json")
        if (destination == "selected" && privateInput.exists()) {
            var bytes = byteArrayOf()
            var appCheck: com.google.firebase.appcheck.FirebaseAppCheck? = null
            try {
                bytes = privateInput.readBytes()
                check(privateInput.delete()) { "Could not remove private test input" }
                val config = org.json.JSONObject(bytes.toString(Charsets.UTF_8))
                bytes.fill(0)
                val project = io.kombify.speechkit.net.FirebaseAiProject(config.getString("applicationId"), config.getString("projectId"),
                    config.getString("apiKey"), allowCloud = true, debugAppCheck = true)
                val profile = io.kombify.speechkit.domain.ConnectionProfile.Firebase(
                    applicationId = config.getString("applicationId"), projectId = config.getString("projectId"),
                    apiKey = config.getString("apiKey"), cloudModel = config.getString("cloudModel"), debugAppCheck = true)
                val token = config.getString("appCheckToken")
                val expiry = config.getLong("appCheckExpireTimeMillis")
                check(token.isNotBlank() && expiry > System.currentTimeMillis()) { "App Check token missing or expired" }
                StoredConnectionSelection.configureFirebase(BuildConfig.CONNECTION_SETUP_ENABLED && BuildConfig.HYBRID_ENABLED && BuildConfig.FIREBASE_CLOUD_ENABLED)
                kotlinx.coroutines.runBlocking { selection.selectFirebase(profile) }
                io.kombify.speechkit.net.FirebaseAiServices.get(target, project)
                val app = com.google.firebase.FirebaseApp.getInstance(io.kombify.speechkit.net.FirebaseAiServices.appName(project))
                val check = com.google.firebase.appcheck.FirebaseAppCheck.getInstance(app)
                appCheck = check
                check.installAppCheckProviderFactory(com.google.firebase.appcheck.AppCheckProviderFactory {
                    com.google.firebase.appcheck.AppCheckProvider {
                        com.google.android.gms.tasks.Tasks.forResult<com.google.firebase.appcheck.AppCheckToken>(object : com.google.firebase.appcheck.AppCheckToken() {
                            override fun getToken(): String = token
                            override fun getExpireTimeMillis(): Long = expiry
                        })
                    }
                }, false)
                runCases()
            } finally {
                bytes.fill(0)
                privateInput.delete()
                appCheck?.installAppCheckProviderFactory(com.google.firebase.appcheck.AppCheckProviderFactory {
                    com.google.firebase.appcheck.AppCheckProvider {
                        com.google.android.gms.tasks.Tasks.forException(IllegalStateException("Context acceptance credential is no longer active"))
                    }
                }, false)
                kotlinx.coroutines.runBlocking(kotlinx.coroutines.NonCancellable) { selection.selectLocal() }
            }
        } else runCases()
    }

}
