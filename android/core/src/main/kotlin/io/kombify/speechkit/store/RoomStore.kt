package io.kombify.speechkit.store

import android.content.Context
import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.Update
import java.time.Instant

/**
 * Room/SQLite store implementation.
 *
 * Mirrors: internal/store/sqlite.go SQLiteStore.
 */
class RoomStore(context: Context) : Store {

    private val db = Room.databaseBuilder(
        context.applicationContext,
        SpeechKitDatabase::class.java,
        "speechkit.db",
    ).build()

    private val transcriptionDao = db.transcriptionDao()
    private val quickNoteDao = db.quickNoteDao()

    override suspend fun saveTranscription(
        text: String, language: String, provider: String, model: String,
        durationMs: Long, latencyMs: Long, audioData: ByteArray?,
    ): Long {
        val entity = TranscriptionEntity(
            text = text, language = language, provider = provider, model = model,
            durationMs = durationMs, latencyMs = latencyMs,
            createdAt = Instant.now().toEpochMilli(),
        )
        return transcriptionDao.insert(entity)
    }

    override suspend fun getTranscription(id: Long): Transcription? =
        transcriptionDao.getById(id)?.toModel()

    override suspend fun listTranscriptions(opts: ListOpts): List<Transcription> =
        transcriptionDao.list(opts.limit, opts.offset).map { it.toModel() }

    override suspend fun transcriptionCount(): Int =
        transcriptionDao.count()

    override suspend fun saveQuickNote(
        text: String, language: String, provider: String,
        durationMs: Long, latencyMs: Long, audioData: ByteArray?,
    ): Long {
        val now = Instant.now().toEpochMilli()
        val entity = QuickNoteEntity(
            text = text, language = language, provider = provider,
            durationMs = durationMs, latencyMs = latencyMs,
            createdAt = now, updatedAt = now,
        )
        return quickNoteDao.insert(entity)
    }

    override suspend fun getQuickNote(id: Long): QuickNote? =
        quickNoteDao.getById(id)?.toModel()

    override suspend fun listQuickNotes(opts: ListOpts): List<QuickNote> =
        quickNoteDao.list(opts.limit, opts.offset).map { it.toModel() }

    override suspend fun updateQuickNote(id: Long, text: String) {
        quickNoteDao.updateText(id, text, Instant.now().toEpochMilli())
    }

    override suspend fun pinQuickNote(id: Long, pinned: Boolean) {
        quickNoteDao.updatePinned(id, pinned, Instant.now().toEpochMilli())
    }

    override suspend fun deleteQuickNote(id: Long) {
        quickNoteDao.delete(id)
    }

    override suspend fun quickNoteCount(): Int =
        quickNoteDao.count()

    override suspend fun stats(): Stats {
        val tCount = transcriptionDao.count()
        val qCount = quickNoteDao.count()
        val totalWords = transcriptionDao.totalWordCount() ?: 0
        val totalDuration = transcriptionDao.totalDurationMs() ?: 0
        val avgLatency = transcriptionDao.averageLatencyMs() ?: 0

        val wpm = if (totalDuration > 0) {
            totalWords.toDouble() / (totalDuration.toDouble() / 60000.0)
        } else 0.0

        return Stats(
            transcriptions = tCount,
            quickNotes = qCount,
            totalWords = totalWords,
            totalAudioDurationMs = totalDuration,
            averageWordsPerMinute = wpm,
            averageLatencyMs = avgLatency,
        )
    }

    override fun close() {
        db.close()
    }
}

// --- Room Entities ---

@Entity(tableName = "transcriptions")
data class TranscriptionEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val text: String,
    val language: String,
    val provider: String,
    val model: String,
    val durationMs: Long,
    val latencyMs: Long,
    val audioPath: String? = null,
    val createdAt: Long,
) {
    fun toModel() = Transcription(
        id = id, text = text, language = language, provider = provider,
        model = model, durationMs = durationMs, latencyMs = latencyMs,
        audioPath = audioPath, createdAt = Instant.ofEpochMilli(createdAt),
    )
}

@Entity(tableName = "quick_notes")
data class QuickNoteEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val text: String,
    val language: String,
    val provider: String,
    val durationMs: Long,
    val latencyMs: Long,
    val audioPath: String? = null,
    val pinned: Boolean = false,
    val createdAt: Long,
    val updatedAt: Long,
) {
    fun toModel() = QuickNote(
        id = id, text = text, language = language, provider = provider,
        durationMs = durationMs, latencyMs = latencyMs, audioPath = audioPath,
        pinned = pinned, createdAt = Instant.ofEpochMilli(createdAt),
        updatedAt = Instant.ofEpochMilli(updatedAt),
    )
}

// --- DAOs ---

@Dao
interface TranscriptionDao {
    @Insert
    suspend fun insert(entity: TranscriptionEntity): Long

    @Query("SELECT * FROM transcriptions WHERE id = :id")
    suspend fun getById(id: Long): TranscriptionEntity?

    @Query("SELECT * FROM transcriptions ORDER BY createdAt DESC LIMIT :limit OFFSET :offset")
    suspend fun list(limit: Int, offset: Int): List<TranscriptionEntity>

    @Query("SELECT COUNT(*) FROM transcriptions")
    suspend fun count(): Int

    @Query("SELECT SUM(LENGTH(text) - LENGTH(REPLACE(text, ' ', '')) + 1) FROM transcriptions WHERE text != ''")
    suspend fun totalWordCount(): Int?

    @Query("SELECT SUM(durationMs) FROM transcriptions")
    suspend fun totalDurationMs(): Long?

    @Query("SELECT AVG(latencyMs) FROM transcriptions")
    suspend fun averageLatencyMs(): Long?
}

@Dao
interface QuickNoteDao {
    @Insert
    suspend fun insert(entity: QuickNoteEntity): Long

    @Query("SELECT * FROM quick_notes WHERE id = :id")
    suspend fun getById(id: Long): QuickNoteEntity?

    @Query("SELECT * FROM quick_notes ORDER BY pinned DESC, updatedAt DESC LIMIT :limit OFFSET :offset")
    suspend fun list(limit: Int, offset: Int): List<QuickNoteEntity>

    @Query("UPDATE quick_notes SET text = :text, updatedAt = :updatedAt WHERE id = :id")
    suspend fun updateText(id: Long, text: String, updatedAt: Long)

    @Query("UPDATE quick_notes SET pinned = :pinned, updatedAt = :updatedAt WHERE id = :id")
    suspend fun updatePinned(id: Long, pinned: Boolean, updatedAt: Long)

    @Query("DELETE FROM quick_notes WHERE id = :id")
    suspend fun delete(id: Long)

    @Query("SELECT COUNT(*) FROM quick_notes")
    suspend fun count(): Int
}

// --- Database ---

@Database(
    entities = [TranscriptionEntity::class, QuickNoteEntity::class],
    version = 1,
    exportSchema = true,
)
abstract class SpeechKitDatabase : RoomDatabase() {
    abstract fun transcriptionDao(): TranscriptionDao
    abstract fun quickNoteDao(): QuickNoteDao
}
