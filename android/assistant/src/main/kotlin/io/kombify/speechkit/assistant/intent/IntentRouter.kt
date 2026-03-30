package io.kombify.speechkit.assistant.intent

import android.content.Context
import timber.log.Timber

/**
 * Intent classification and routing for the voice assistant.
 *
 * Classifies transcribed text into actionable intents using
 * pattern matching (Phase 4 MVP) with LLM fallback (Phase 5).
 *
 * The router maintains conversation context (foreground app, recent intents)
 * to improve classification accuracy.
 */
class IntentRouter {

    private var foregroundApp: String? = null
    private var webUri: String? = null
    private val actionExecutors = mutableMapOf<IntentType, ActionExecutor>()

    init {
        // Register built-in action executors
        registerDefaults()
    }

    fun setContext(foregroundApp: String? = null, webUri: String? = null) {
        this.foregroundApp = foregroundApp
        this.webUri = webUri
    }

    /**
     * Classify transcribed text into an intent.
     *
     * Uses pattern matching with keyword extraction.
     * Falls back to GENERAL_QUERY for unrecognized patterns.
     */
    fun classify(text: String): AssistantIntent {
        val normalized = text.trim().lowercase()

        // Check each pattern in priority order
        for ((type, patterns) in INTENT_PATTERNS) {
            for (pattern in patterns) {
                if (pattern.matches(normalized)) {
                    val params = pattern.extract(normalized)
                    return AssistantIntent(
                        type = type,
                        rawText = text,
                        parameters = params,
                        confidence = 0.85f,
                        foregroundApp = foregroundApp,
                    )
                }
            }
        }

        // Fallback: general query
        return AssistantIntent(
            type = IntentType.GENERAL_QUERY,
            rawText = text,
            parameters = mapOf("query" to text),
            confidence = 0.5f,
            foregroundApp = foregroundApp,
        )
    }

    /** Execute a classified intent. */
    suspend fun execute(context: Context, intent: AssistantIntent): ActionResult {
        val executor = actionExecutors[intent.type]
        if (executor == null) {
            Timber.w("No executor for intent type: ${intent.type}")
            return ActionResult(
                success = false,
                errorMessage = "Aktion '${intent.type.displayName}' nicht unterstuetzt",
            )
        }

        return try {
            executor.execute(context, intent)
        } catch (e: Exception) {
            Timber.e(e, "Intent execution failed: ${intent.type}")
            ActionResult(
                success = false,
                errorMessage = e.message ?: "Unbekannter Fehler",
            )
        }
    }

    fun registerExecutor(type: IntentType, executor: ActionExecutor) {
        actionExecutors[type] = executor
    }

    private fun registerDefaults() {
        registerExecutor(IntentType.OPEN_APP, OpenAppExecutor())
        registerExecutor(IntentType.SET_TIMER, SetTimerExecutor())
        registerExecutor(IntentType.SET_ALARM, SetAlarmExecutor())
        registerExecutor(IntentType.QUICK_NOTE, QuickNoteExecutor())
        registerExecutor(IntentType.SEARCH_WEB, SearchWebExecutor())
        registerExecutor(IntentType.SEND_MESSAGE, SendMessageExecutor())
        registerExecutor(IntentType.MAKE_CALL, MakeCallExecutor())
        registerExecutor(IntentType.GENERAL_QUERY, GeneralQueryExecutor())
    }

    companion object {
        private val INTENT_PATTERNS: Map<IntentType, List<IntentPattern>> = mapOf(
            IntentType.OPEN_APP to listOf(
                IntentPattern.prefix("oeffne", "app"),
                IntentPattern.prefix("open", "app"),
                IntentPattern.prefix("starte", "app"),
                IntentPattern.prefix("start", "app"),
            ),
            IntentType.SET_TIMER to listOf(
                IntentPattern.contains("timer", extractNumber = true),
                IntentPattern.contains("wecker auf", extractNumber = true),
                IntentPattern.contains("set timer", extractNumber = true),
                IntentPattern.contains("set a timer", extractNumber = true),
            ),
            IntentType.SET_ALARM to listOf(
                IntentPattern.contains("wecker", extractNumber = true),
                IntentPattern.contains("alarm", extractNumber = true),
                IntentPattern.contains("weck mich", extractNumber = true),
            ),
            IntentType.QUICK_NOTE to listOf(
                IntentPattern.prefix("notiz", "content"),
                IntentPattern.prefix("merke", "content"),
                IntentPattern.prefix("note", "content"),
                IntentPattern.prefix("schnelle notiz", "content"),
                IntentPattern.prefix("quick note", "content"),
            ),
            IntentType.SEARCH_WEB to listOf(
                IntentPattern.prefix("suche nach", "query"),
                IntentPattern.prefix("such nach", "query"),
                IntentPattern.prefix("google", "query"),
                IntentPattern.prefix("search for", "query"),
                IntentPattern.prefix("was ist", "query"),
                IntentPattern.prefix("what is", "query"),
            ),
            IntentType.SEND_MESSAGE to listOf(
                IntentPattern.contains("nachricht an", extractTarget = true),
                IntentPattern.contains("schreibe an", extractTarget = true),
                IntentPattern.contains("message to", extractTarget = true),
                IntentPattern.contains("send message", extractTarget = true),
            ),
            IntentType.MAKE_CALL to listOf(
                IntentPattern.prefix("ruf an", "contact"),
                IntentPattern.prefix("anrufen", "contact"),
                IntentPattern.prefix("call", "contact"),
            ),
        )
    }
}

/** A classified voice assistant intent. */
data class AssistantIntent(
    val type: IntentType,
    val rawText: String,
    val parameters: Map<String, String> = emptyMap(),
    val confidence: Float = 0f,
    val foregroundApp: String? = null,
)

/** Available intent types. */
enum class IntentType(val displayName: String) {
    OPEN_APP("App oeffnen"),
    SET_TIMER("Timer stellen"),
    SET_ALARM("Wecker stellen"),
    QUICK_NOTE("Notiz erstellen"),
    SEARCH_WEB("Websuche"),
    SEND_MESSAGE("Nachricht senden"),
    MAKE_CALL("Anruf starten"),
    GENERAL_QUERY("Allgemeine Frage"),
}

/** Result of executing an intent action. */
data class ActionResult(
    val success: Boolean,
    val responseText: String = "",
    val errorMessage: String? = null,
    val keepOpen: Boolean = false,
)

/** Executes a specific intent type. */
interface ActionExecutor {
    suspend fun execute(context: Context, intent: AssistantIntent): ActionResult
}

/**
 * Simple pattern matcher for intent classification.
 * MVP approach -- will be enhanced with LLM classification in Phase 5.
 */
data class IntentPattern(
    private val keyword: String,
    private val matchMode: MatchMode,
    private val paramKey: String,
    private val extractNumber: Boolean = false,
    private val extractTarget: Boolean = false,
) {
    enum class MatchMode { PREFIX, CONTAINS }

    fun matches(text: String): Boolean = when (matchMode) {
        MatchMode.PREFIX -> text.startsWith(keyword)
        MatchMode.CONTAINS -> text.contains(keyword)
    }

    fun extract(text: String): Map<String, String> {
        val params = mutableMapOf<String, String>()

        when (matchMode) {
            MatchMode.PREFIX -> {
                val remaining = text.removePrefix(keyword).trim()
                if (remaining.isNotBlank()) params[paramKey] = remaining
            }
            MatchMode.CONTAINS -> {
                val idx = text.indexOf(keyword)
                if (idx >= 0) {
                    val remaining = text.substring(idx + keyword.length).trim()
                    if (remaining.isNotBlank()) params[paramKey] = remaining
                }
            }
        }

        if (extractNumber) {
            val numbers = Regex("\\d+").findAll(text)
            numbers.firstOrNull()?.let { params["number"] = it.value }

            // Extract time units
            when {
                text.contains("minute") || text.contains("min") -> params["unit"] = "minutes"
                text.contains("stunde") || text.contains("hour") -> params["unit"] = "hours"
                text.contains("sekunde") || text.contains("second") -> params["unit"] = "seconds"
            }
        }

        if (extractTarget) {
            val idx = text.indexOf(keyword)
            if (idx >= 0) {
                val afterKeyword = text.substring(idx + keyword.length).trim()
                // Split "contact message" pattern
                val parts = afterKeyword.split(" ", limit = 2)
                if (parts.isNotEmpty()) params["target"] = parts[0]
                if (parts.size > 1) params["message"] = parts[1]
            }
        }

        return params
    }

    companion object {
        fun prefix(keyword: String, paramKey: String) =
            IntentPattern(keyword, MatchMode.PREFIX, paramKey)

        fun contains(keyword: String, paramKey: String = "value", extractNumber: Boolean = false, extractTarget: Boolean = false) =
            IntentPattern(keyword, MatchMode.CONTAINS, paramKey, extractNumber, extractTarget)
    }
}
