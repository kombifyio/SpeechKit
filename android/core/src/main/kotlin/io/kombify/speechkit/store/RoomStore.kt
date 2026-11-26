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
import androidx.room.OnConflictStrategy
import androidx.room.migration.Migration
import androidx.room.withTransaction
import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.room.Update
import java.time.Instant

/**
 * Room/SQLite store implementation.
 *
 * Mirrors: app/internal/store/sqlite.go SQLiteStore.
 */
class RoomStore(context: Context) : Store {

    private val db = Room.databaseBuilder(
        context.applicationContext,
        SpeechKitDatabase::class.java,
        "speechkit.db",
    ).addMigrations(MIGRATION_1_2, MIGRATION_2_3).build()

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

    /** Idempotently persist a note and its receipt in the same database transaction. */
    suspend fun saveQuickNoteOnce(scope: String, operationId: String, text: String, language: String): QuickNote =
        db.withTransaction {
            require(scope.isNotBlank() && operationId.isNotBlank() && text.isNotBlank())
            val previous = db.operationDao().get(scope, operationId)
            if (previous != null) {
                check(previous.source == "local_note" && previous.state == "completed") { "Operation outcome is not replayable" }
                val id = previous.content.toLongOrNull() ?: error("Invalid note receipt")
                val note = quickNoteDao.getById(id)?.toModel() ?: error("Saved note no longer exists")
                check(note.text == text && note.language == language) { "Operation ID belongs to a different note" }
                return@withTransaction note
            }
            val now = Instant.now().toEpochMilli()
            val id = quickNoteDao.insert(QuickNoteEntity(text = text, language = language, provider = "local_agent", durationMs = 0, latencyMs = 0, createdAt = now, updatedAt = now))
            db.operationDao().put(OperationReceipt(scope, operationId, "local_note", "completed", id.toString(), now))
            quickNoteDao.getById(id)?.toModel() ?: error("Saved note could not be read back")
        }

    /** Saves a durable turn/operation state. A remote DISPATCHED operation is never safe to replay. */
    suspend fun saveOperation(operation: OperationReceipt) = db.operationDao().put(operation)

    /** Account-bound writes and revocation serialize in the same database transaction. */
    suspend fun saveScopedOperation(operation: OperationReceipt) = db.withTransaction {
        check(db.scopeDao().isRevoked(operation.scope) == 0) { "Account scope has been revoked" }
        db.operationDao().put(operation)
    }

    /** Persist the tombstone and purge before any stale writer can commit again. */
    suspend fun revokeScope(scope: String) = db.withTransaction {
        require(scope.isNotBlank() && scope != "device")
        db.scopeDao().revoke(RevokedScope(scope))
        db.operationDao().deleteScope(scope)
    }

    /** A delayed query or event must never regress an acknowledged turn. */
    suspend fun saveRemoteTurnReceipt(scope: String, sessionId: String, turnId: String, status: String): OperationReceipt =
        db.withTransaction {
            check(db.scopeDao().isRevoked(scope) == 0) { "Account scope has been revoked" }
            val rank = mapOf("unknown" to 0, "dispatched" to 1, "completed" to 2)
            require(scope.isNotBlank() && sessionId.isNotBlank() && turnId.startsWith("$sessionId:") && status in rank)
            val previous = db.operationDao().get(scope, turnId)
            if (previous != null) {
                check(previous.source == "remote_agent" && previous.content == sessionId && previous.state in rank) { "Turn receipt binding differs" }
                if (rank.getValue(previous.state) >= rank.getValue(status)) return@withTransaction previous
            }
            val receipt = OperationReceipt(scope, turnId, "remote_agent", status, sessionId, Instant.now().toEpochMilli())
            db.operationDao().put(receipt)
            receipt
        }

    suspend fun getOperation(scope: String, operationId: String): OperationReceipt? =
        db.operationDao().get(scope, operationId)

    suspend fun listOperations(scope: String): List<OperationReceipt> = db.operationDao().list(scope)

    suspend fun deleteOperations(scope: String) = db.operationDao().deleteScope(scope)

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

/** Scope is a device profile or account namespace, never a bearer or secret. */
@Entity(tableName = "operation_receipts", primaryKeys = ["scope", "operationId"])
data class OperationReceipt(
    val scope: String,
    val operationId: String,
    val source: String,
    val state: String,
    val content: String,
    val updatedAt: Long,
)

@Entity(tableName = "revoked_scopes")
data class RevokedScope(@PrimaryKey val scope: String)

@Dao
interface ScopeDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun revoke(scope: RevokedScope)

    @Query("SELECT COUNT(*) FROM revoked_scopes WHERE scope = :scope")
    suspend fun isRevoked(scope: String): Int
}

@Dao
interface OperationDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun put(operation: OperationReceipt)

    @Query("SELECT * FROM operation_receipts WHERE scope = :scope AND operationId = :operationId")
    suspend fun get(scope: String, operationId: String): OperationReceipt?

    @Query("DELETE FROM operation_receipts WHERE scope = :scope")
    suspend fun deleteScope(scope: String)

    @Query("SELECT * FROM operation_receipts WHERE scope = :scope ORDER BY updatedAt ASC")
    suspend fun list(scope: String): List<OperationReceipt>
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
    entities = [TranscriptionEntity::class, QuickNoteEntity::class, OperationReceipt::class, RevokedScope::class],
    version = 3,
    exportSchema = true,
)
abstract class SpeechKitDatabase : RoomDatabase() {
    abstract fun transcriptionDao(): TranscriptionDao
    abstract fun quickNoteDao(): QuickNoteDao
    abstract fun operationDao(): OperationDao
    abstract fun scopeDao(): ScopeDao
}

private val MIGRATION_1_2 = object : Migration(1, 2) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("CREATE TABLE IF NOT EXISTS `operation_receipts` (`scope` TEXT NOT NULL, `operationId` TEXT NOT NULL, `source` TEXT NOT NULL, `state` TEXT NOT NULL, `content` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, PRIMARY KEY(`scope`, `operationId`))")
    }
}

private val MIGRATION_2_3 = object : Migration(2, 3) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("CREATE TABLE IF NOT EXISTS `revoked_scopes` (`scope` TEXT NOT NULL, PRIMARY KEY(`scope`))")
    }
}
