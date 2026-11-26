package io.kombify.speechkit.app.hybrid

import android.content.Context
import android.content.ContextWrapper
import androidx.test.platform.app.InstrumentationRegistry
import io.kombify.speechkit.store.RoomStore
import java.io.File
import java.util.UUID
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test

/** Real device SQLite upgrade coverage for the shipped Room schema. */
class HybridStoreMigrationTest {
    @Test fun existingNotesSurviveDatabaseUpgrade() = runBlocking {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val directory = File(base.cacheDir, "hybrid-upgrade-${UUID.randomUUID()}").apply { mkdirs() }
        val isolated = object : ContextWrapper(base) {
            override fun getApplicationContext(): Context = this
            override fun getDatabasePath(name: String): File = File(directory, name)
        }
        // The shipped v1 schema, on disk. Room must upgrade it without losing data.
        android.database.sqlite.SQLiteDatabase.openOrCreateDatabase(File(directory, "speechkit.db"), null).use { old ->
            old.execSQL("CREATE TABLE transcriptions (id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, text TEXT NOT NULL, language TEXT NOT NULL, provider TEXT NOT NULL, model TEXT NOT NULL, durationMs INTEGER NOT NULL, latencyMs INTEGER NOT NULL, audioPath TEXT, createdAt INTEGER NOT NULL)")
            old.execSQL("CREATE TABLE quick_notes (id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, text TEXT NOT NULL, language TEXT NOT NULL, provider TEXT NOT NULL, durationMs INTEGER NOT NULL, latencyMs INTEGER NOT NULL, audioPath TEXT, pinned INTEGER NOT NULL, createdAt INTEGER NOT NULL, updatedAt INTEGER NOT NULL)")
            old.execSQL("INSERT INTO quick_notes VALUES (7, 'Existing note', 'en', 'device', 0, 0, NULL, 1, 1, 1)")
            old.version = 1
        }
        val store = RoomStore(isolated)
        try {
            assertEquals("Existing note", store.getQuickNote(7)?.text)
            assertTrue(store.getQuickNote(7)?.pinned == true)
            val created = store.saveQuickNoteOnce("device", "migration-check", "New note", "en")
            assertEquals(created.id.toString(), store.getOperation("device", "migration-check")?.content)
            assertEquals(2, store.quickNoteCount())
        } finally {
            store.close()
            directory.listFiles()?.forEach { it.delete() }
            directory.delete()
        }
    }
}
