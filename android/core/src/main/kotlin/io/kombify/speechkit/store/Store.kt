package io.kombify.speechkit.store

import java.time.Instant

/**
 * Central storage interface.
 *
 * Mirrors: internal/store/types.go Store interface.
 * Each backend (Room/SQLite, PostgreSQL, kombify Cloud) implements this.
 */
interface Store {
    // Transcriptions
    suspend fun saveTranscription(
        text: String,
        language: String,
        provider: String,
        model: String,
        durationMs: Long,
        latencyMs: Long,
        audioData: ByteArray? = null,
    ): Long

    suspend fun getTranscription(id: Long): Transcription?
    suspend fun listTranscriptions(opts: ListOpts = ListOpts()): List<Transcription>
    suspend fun transcriptionCount(): Int

    // Quick Notes
    suspend fun saveQuickNote(
        text: String,
        language: String,
        provider: String,
        durationMs: Long,
        latencyMs: Long,
        audioData: ByteArray? = null,
    ): Long

    suspend fun getQuickNote(id: Long): QuickNote?
    suspend fun listQuickNotes(opts: ListOpts = ListOpts()): List<QuickNote>
    suspend fun updateQuickNote(id: Long, text: String)
    suspend fun pinQuickNote(id: Long, pinned: Boolean)
    suspend fun deleteQuickNote(id: Long)
    suspend fun quickNoteCount(): Int

    // Stats
    suspend fun stats(): Stats

    // Lifecycle
    fun close()
}

/** Mirrors: internal/store/types.go ListOpts. */
data class ListOpts(
    val limit: Int = 50,
    val offset: Int = 0,
    val language: String? = null,
    val after: Instant? = null,
)

/** Mirrors: internal/store/types.go Transcription. */
data class Transcription(
    val id: Long,
    val text: String,
    val language: String,
    val provider: String,
    val model: String,
    val durationMs: Long,
    val latencyMs: Long,
    val audioPath: String? = null,
    val createdAt: Instant,
)

/** Mirrors: internal/store/types.go QuickNote. */
data class QuickNote(
    val id: Long,
    val text: String,
    val language: String,
    val provider: String,
    val durationMs: Long,
    val latencyMs: Long,
    val audioPath: String? = null,
    val pinned: Boolean = false,
    val createdAt: Instant,
    val updatedAt: Instant,
)

/** Mirrors: internal/store/types.go Stats. */
data class Stats(
    val transcriptions: Int = 0,
    val quickNotes: Int = 0,
    val totalWords: Int = 0,
    val totalAudioDurationMs: Long = 0,
    val averageWordsPerMinute: Double = 0.0,
    val averageLatencyMs: Long = 0,
)
