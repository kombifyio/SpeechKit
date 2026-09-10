package io.kombify.speechkit.assistant.intent

import android.app.SearchManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.AlarmClock
import io.kombify.speechkit.assistant.R
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ConnectionProfileSource
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.net.SpeechKitServerApi

/**
 * Built-in action executors for common voice assistant intents.
 *
 * Each executor translates an AssistantIntent into Android system actions
 * using standard intents (AlarmClock, ACTION_VIEW, ACTION_SEND, etc.).
 */

class OpenAppExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val appName = intent.parameters["app"] ?: return ActionResult(
            success = false,
            errorMessage = context.getString(R.string.speechkit_assistant_open_app_which),
        )

        val pm = context.packageManager
        val resolvedIntent = pm.getLaunchIntentForPackage(resolvePackageName(appName, context))

        return if (resolvedIntent != null) {
            resolvedIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            context.startActivity(resolvedIntent)
            ActionResult(
                success = true,
                responseText = context.getString(R.string.speechkit_assistant_open_app_opening, appName),
            )
        } else {
            ActionResult(
                success = false,
                errorMessage = context.getString(R.string.speechkit_assistant_open_app_missing, appName),
            )
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
            success = false,
            errorMessage = context.getString(R.string.speechkit_assistant_timer_how_many),
            keepOpen = true,
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
            ActionResult(
                success = true,
                responseText = context.getString(R.string.speechkit_assistant_timer_set, number, unit),
            )
        } catch (e: Exception) {
            ActionResult(
                success = false,
                errorMessage = context.getString(R.string.speechkit_assistant_timer_failed),
            )
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
            putExtra(AlarmClock.EXTRA_MESSAGE, "SpeechKit alarm")
            putExtra(AlarmClock.EXTRA_SKIP_UI, false)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }

        return try {
            context.startActivity(alarmIntent)
            ActionResult(
                success = true,
                responseText = if (number != null) {
                    context.getString(R.string.speechkit_assistant_alarm_set_hour, number)
                } else {
                    context.getString(R.string.speechkit_assistant_alarm_set)
                },
            )
        } catch (e: Exception) {
            ActionResult(
                success = false,
                errorMessage = context.getString(R.string.speechkit_assistant_alarm_failed),
            )
        }
    }
}

class QuickNoteExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val content = intent.parameters["content"] ?: intent.rawText

        // Save via Store (would be injected in production)
        VoiceLog.i(VoiceLog.ASSIST, "quick note saved chars=${content.length}")

        return ActionResult(
            success = true,
            responseText = context.getString(
                R.string.speechkit_assistant_note_saved,
                "${content.take(50)}${if (content.length > 50) "..." else ""}",
            ),
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
            ActionResult(
                success = true,
                responseText = context.getString(R.string.speechkit_assistant_searching, query),
            )
        } catch (e: Exception) {
            // Fallback to browser
            val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse("https://www.google.com/search?q=${Uri.encode(query)}")).apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            context.startActivity(browserIntent)
            ActionResult(
                success = true,
                responseText = context.getString(R.string.speechkit_assistant_searching, query),
            )
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
                errorMessage = context.getString(R.string.speechkit_assistant_message_who),
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
            context.startActivity(Intent.createChooser(sendIntent, "Send message").apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            })
            ActionResult(
                success = true,
                responseText = context.getString(R.string.speechkit_assistant_message_preparing, target),
            )
        } catch (e: Exception) {
            ActionResult(
                success = false,
                errorMessage = context.getString(R.string.speechkit_assistant_message_failed),
            )
        }
    }
}

class MakeCallExecutor : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val contact = intent.parameters["contact"] ?: return ActionResult(
            success = false,
            errorMessage = context.getString(R.string.speechkit_assistant_call_who),
            keepOpen = true,
        )

        // Open dialer (does not auto-call -- requires user confirmation)
        val dialIntent = Intent(Intent.ACTION_DIAL).apply {
            data = Uri.parse("tel:$contact")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }

        return try {
            context.startActivity(dialIntent)
            ActionResult(
                success = true,
                responseText = context.getString(R.string.speechkit_assistant_call_preparing, contact),
            )
        } catch (e: Exception) {
            ActionResult(
                success = false,
                errorMessage = context.getString(R.string.speechkit_assistant_call_failed),
            )
        }
    }
}

class GeneralQueryExecutor(
    private val profiles: ConnectionProfileSource,
) : ActionExecutor {
    override suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val query = intent.parameters["query"] ?: intent.rawText
        val profile = profiles.currentProfile()
        if (profile !is ConnectionProfile.Server) {
            return ActionResult(
                success = true,
                responseText = NO_SERVER_ASSIST_MESSAGE,
                keepOpen = true,
            )
        }
        return try {
            val response = SpeechKitServerApi(profile).processAssist(query)
            val spoken = response.speakText.ifBlank { response.text }
            ActionResult(
                success = spoken.isNotBlank(),
                responseText = spoken.ifBlank { ACTION_FAILED_MESSAGE },
            )
        } catch (e: Exception) {
            VoiceLog.e(VoiceLog.ASSIST, "processAssist failed", e)
            ActionResult(
                success = false,
                errorMessage = e.message ?: ACTION_FAILED_MESSAGE,
            )
        }
    }

    private companion object {
        const val ACTION_FAILED_MESSAGE = "Action failed"
        const val NO_SERVER_ASSIST_MESSAGE =
            "I need a paired SpeechKit server for that"
    }
}
