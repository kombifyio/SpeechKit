package io.kombify.speechkit.engine

import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import java.time.Instant

/**
 * Core SpeechKit engine interface.
 *
 * Mirrors: pkg/speechkit/runtime.go Engine interface.
 * Go channels map to Kotlin SharedFlow/StateFlow.
 * Go context.Context maps to coroutine cancellation.
 */
interface Engine {
    suspend fun start()
    suspend fun stop()
    val events: SharedFlow<Event>
    val commands: CommandBus
    val state: StateFlow<Snapshot>
}

/** Mirrors: pkg/speechkit/runtime.go EventType constants. */
enum class EventType(val value: String) {
    STATE_CHANGED("state.changed"),
    RECORDING_STARTED("recording.started"),
    PROCESSING_STARTED("processing.started"),
    TRANSCRIPTION_READY("transcription.ready"),
    TRANSCRIPT_COMMITTED("transcription.committed"),
    QUICKNOTE_MODE_ARMED("quicknote.mode_armed"),
    QUICKNOTE_UPDATED("quicknote.updated"),
    WARNING_RAISED("warning.raised"),
    ERROR_RAISED("error.raised"),
}

/** Mirrors: pkg/speechkit/runtime.go Event struct. */
data class Event(
    val type: EventType,
    val time: Instant = Instant.now(),
    val message: String = "",
    val text: String = "",
    val provider: String = "",
    val quickNote: Boolean = false,
    val error: Throwable? = null,
)

/** Mirrors: pkg/speechkit/runtime.go CommandType constants. */
enum class CommandType(val value: String) {
    SHOW_DASHBOARD("dashboard.show"),
    START_DICTATION("dictation.start"),
    STOP_DICTATION("dictation.stop"),
    SET_ACTIVE_MODE("mode.set_active"),
    OPEN_QUICKNOTE("quicknote.open"),
    OPEN_QUICK_CAPTURE("quicknote.capture.open"),
    CLOSE_QUICK_CAPTURE("quicknote.capture.close"),
    ARM_QUICKNOTE_RECORDING("quicknote.record.arm"),
    COPY_LAST_TRANSCRIPTION("transcription.copy_last"),
    INSERT_LAST_TRANSCRIPTION("transcription.insert_last"),
    SUMMARIZE_SELECTION("selection.summarize"),
}

/** Mirrors: pkg/speechkit/runtime.go Command struct. */
data class Command(
    val type: CommandType,
    val text: String = "",
    val noteId: Long = 0,
    val target: String = "",
    val metadata: Map<String, String> = emptyMap(),
)

/** Mirrors: pkg/speechkit/runtime.go Snapshot struct. */
data class Snapshot(
    val status: String = "idle",
    val text: String = "",
    val level: Double = 0.0,
    val activeMode: String = "dictate",
    val providers: List<String> = emptyList(),
    val activeProfiles: Map<String, String> = emptyMap(),
    val transcriptions: Int = 0,
    val quickNoteMode: Boolean = false,
    val quickCaptureMode: Boolean = false,
    val lastTranscriptionText: String = "",
)

/** Mirrors: pkg/speechkit/runtime.go CommandBus interface. */
interface CommandBus {
    suspend fun dispatch(command: Command)
}
