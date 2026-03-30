package io.kombify.speechkit.assistant.intent

import android.app.SearchManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.AlarmClock
import timber.log.Timber

/**
 * Built-in action executors for common voice assistant intents.
 *
 * Each executor translates an AssistantIntent into Android system actions
 * using standard intents (AlarmClock, ACTION_VIEW, ACTION_SEND, etc.).
 */

class OpenAppExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val appName = intent.parameters["app"] ?: return ActionResult(
            success = false, errorMessage = "Welche App soll ich oeffnen?",
        )

        val pm = context.packageManager
        val resolvedIntent = pm.getLaunchIntentForPackage(resolvePackageName(appName, context))

        return if (resolvedIntent != null) {
            resolvedIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            context.startActivity(resolvedIntent)
            ActionResult(success = true, responseText = "$appName wird geoeffnet")
        } else {
            ActionResult(success = false, errorMessage = "App '$appName' nicht gefunden")
        }
    }

    private fun resolvePackageName(name: String, context: Context): String {
        val normalized = name.lowercase().trim()
        // Common app name mappings
        return COMMON_APPS[normalized] ?: run {
            // Try to find by label
            val pm = context.packageManager
            val apps = pm.getInstalledApplications(0)
            apps.firstOrNull { app ->
                pm.getApplicationLabel(app).toString().lowercase().contains(normalized)
            }?.packageName ?: normalized
        }
    }

    companion object {
        private val COMMON_APPS = mapOf(
            "whatsapp" to "com.whatsapp",
            "instagram" to "com.instagram.android",
            "youtube" to "com.google.android.youtube",
            "spotify" to "com.spotify.music",
            "chrome" to "com.android.chrome",
            "gmail" to "com.google.android.gm",
            "kamera" to "com.android.camera2",
            "camera" to "com.android.camera2",
            "kalender" to "com.google.android.calendar",
            "calendar" to "com.google.android.calendar",
            "einstellungen" to "com.android.settings",
            "settings" to "com.android.settings",
            "maps" to "com.google.android.apps.maps",
            "telegram" to "org.telegram.messenger",
            "signal" to "org.thoughtcrime.securesms",
            "twitter" to "com.twitter.android",
            "x" to "com.twitter.android",
        )
    }
}

class SetTimerExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val number = intent.parameters["number"]?.toIntOrNull() ?: return ActionResult(
            success = false, errorMessage = "Wie viele Minuten?", keepOpen = true,
        )
        val unit = intent.parameters["unit"] ?: "minutes"

        val seconds = when (unit) {
            "hours" -> number * 3600
            "minutes" -> number * 60
            "seconds" -> number
            else -> number * 60
        }

        val timerIntent = Intent(AlarmClock.ACTION_SET_TIMER).apply {
            putExtra(AlarmClock.EXTRA_LENGTH, seconds)
            putExtra(AlarmClock.EXTRA_MESSAGE, "SpeechKit Timer")
            putExtra(AlarmClock.EXTRA_SKIP_UI, true)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }

        return try {
            context.startActivity(timerIntent)
            ActionResult(success = true, responseText = "Timer auf $number $unit gestellt")
        } catch (e: Exception) {
            ActionResult(success = false, errorMessage = "Timer konnte nicht gestellt werden")
        }
    }
}

class SetAlarmExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val number = intent.parameters["number"]?.toIntOrNull()

        val alarmIntent = Intent(AlarmClock.ACTION_SET_ALARM).apply {
            if (number != null) {
                putExtra(AlarmClock.EXTRA_HOUR, number)
                putExtra(AlarmClock.EXTRA_MINUTES, 0)
            }
            putExtra(AlarmClock.EXTRA_MESSAGE, "SpeechKit Wecker")
            putExtra(AlarmClock.EXTRA_SKIP_UI, false)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }

        return try {
            context.startActivity(alarmIntent)
            val timeStr = if (number != null) "auf $number Uhr " else ""
            ActionResult(success = true, responseText = "Wecker ${timeStr}wird gestellt")
        } catch (e: Exception) {
            ActionResult(success = false, errorMessage = "Wecker konnte nicht gestellt werden")
        }
    }
}

class QuickNoteExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val content = intent.parameters["content"] ?: intent.rawText

        // Save via Store (would be injected in production)
        Timber.d("Quick note saved: '$content'")

        return ActionResult(
            success = true,
            responseText = "Notiz gespeichert: \"${content.take(50)}${if (content.length > 50) "..." else ""}\"",
        )
    }
}

class SearchWebExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val query = intent.parameters["query"] ?: intent.rawText

        val searchIntent = Intent(Intent.ACTION_WEB_SEARCH).apply {
            putExtra(SearchManager.QUERY, query)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }

        return try {
            context.startActivity(searchIntent)
            ActionResult(success = true, responseText = "Suche nach: $query")
        } catch (e: Exception) {
            // Fallback to browser
            val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse("https://www.google.com/search?q=${Uri.encode(query)}")).apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            context.startActivity(browserIntent)
            ActionResult(success = true, responseText = "Suche nach: $query")
        }
    }
}

class SendMessageExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val target = intent.parameters["target"]
        val message = intent.parameters["message"]

        if (target == null) {
            return ActionResult(
                success = false,
                errorMessage = "An wen soll die Nachricht gehen?",
                keepOpen = true,
            )
        }

        // Open messaging app with pre-filled text
        val sendIntent = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_TEXT, message ?: "")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }

        return try {
            context.startActivity(Intent.createChooser(sendIntent, "Nachricht senden").apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            })
            ActionResult(success = true, responseText = "Nachricht an $target wird vorbereitet")
        } catch (e: Exception) {
            ActionResult(success = false, errorMessage = "Nachricht konnte nicht gesendet werden")
        }
    }
}

class MakeCallExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val contact = intent.parameters["contact"] ?: return ActionResult(
            success = false, errorMessage = "Wen soll ich anrufen?", keepOpen = true,
        )

        // Open dialer (does not auto-call -- requires user confirmation)
        val dialIntent = Intent(Intent.ACTION_DIAL).apply {
            data = Uri.parse("tel:$contact")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }

        return try {
            context.startActivity(dialIntent)
            ActionResult(success = true, responseText = "Anruf an $contact wird vorbereitet")
        } catch (e: Exception) {
            ActionResult(success = false, errorMessage = "Anruf konnte nicht gestartet werden")
        }
    }
}

class GeneralQueryExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val query = intent.parameters["query"] ?: intent.rawText

        // Phase 5: Route to LLM for answer generation
        // For now, fall back to web search
        return ActionResult(
            success = true,
            responseText = "Ich kann diese Frage noch nicht direkt beantworten. Soll ich im Web danach suchen?",
            keepOpen = true,
        )
    }
}
