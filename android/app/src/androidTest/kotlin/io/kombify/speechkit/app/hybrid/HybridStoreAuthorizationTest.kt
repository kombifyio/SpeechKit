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

/** Owner-bound operation authority and durable sign-out revocation on real device SQLite. */
class HybridStoreAuthorizationTest {
    @Test fun noteReceiptSurvivesReopenWithoutRepeatingWrite() = runBlocking {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val directory = File(base.cacheDir, "hybrid-acceptance-${UUID.randomUUID()}").apply { mkdirs() }
        val isolated = object : ContextWrapper(base) {
            override fun getApplicationContext(): Context = this
            override fun getDatabasePath(name: String): File = File(directory, name)
        }
        val operation = UUID.randomUUID().toString()
        var store = RoomStore(isolated)
        try {
            val saved = store.saveQuickNoteOnce("device", operation, "Acceptance note", "en")
            store.close()
            store = RoomStore(isolated)
            val again = store.saveQuickNoteOnce("device", operation, "Acceptance note", "en")
            assertEquals(saved.id, again.id)
            assertEquals(1, store.quickNoteCount())
            assertEquals(saved.id.toString(), store.getOperation("device", operation)?.content)
            assertNull(store.getOperation("another-owner", operation))
            var rejected = false
            try { store.saveQuickNoteOnce("device", operation, "Different operation payload", "en") }
            catch (_: IllegalStateException) { rejected = true }
            assertTrue("An operation ID must not authorize different contents", rejected)
            assertEquals("Acceptance note", store.getQuickNote(saved.id)?.text)
            store.saveRemoteTurnReceipt("endpoint:acceptance", "session1", "session1:1", "completed")
            store.saveRemoteTurnReceipt("endpoint:acceptance", "session1", "session1:1", "dispatched")
            store.close()
            store = RoomStore(isolated)
            assertEquals("completed", store.getOperation("endpoint:acceptance", "session1:1")?.state)
            assertNull(store.getOperation("endpoint:another-owner", "session1:1"))
            store.revokeScope("endpoint:acceptance")
            store.close()
            store = RoomStore(isolated)
            assertTrue(store.listOperations("endpoint:acceptance").isEmpty())
            rejected = false
            try { store.saveRemoteTurnReceipt("endpoint:acceptance", "session1", "session1:1", "completed") }
            catch (_: IllegalStateException) { rejected = true }
            assertTrue("A late reply must not recreate signed-out account data", rejected)
            assertEquals("Acceptance note", store.getQuickNote(saved.id)?.text)
        } finally {
            store.close()
            directory.listFiles()?.forEach { it.delete() }
            directory.delete()
        }
    }
}
