package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.stt.system.SystemSpeechRecognizerSession
import io.mockk.mockk
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test

/**
 * Tier-branch tests for the controller. The system tier is constructed
 * lazily (the platform recognizer is only touched at the first
 * `startSegment`), so a mocked Context is enough to assert the branch shape
 * without an Android runtime.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class DictationControllerTest {

    // Since coroutines 1.11 the absent platform Main dispatcher is an error
    // rather than something runTest papers over, and constructing the system
    // tier's session reaches it.
    @BeforeEach
    fun installMainDispatcher() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @AfterEach
    fun removeMainDispatcher() = Dispatchers.resetMain()

    @Test
    fun `system on-device profile yields the raw system session, unwrapped`() = runTest {
        val controller = DictationController(
            profile = ConnectionProfile.SystemOnDevice(),
            context = mockk<Context>(relaxed = true),
        )

        val session = controller.openSession()

        // Deliberately NOT KeepAliveSession/RetryingDictationSession: the
        // tier is sessionless (no idle watchdog) and has no transport whose
        // ws_failure a batch POST could rescue.
        assertInstanceOf(SystemSpeechRecognizerSession::class.java, session)
        assertTrue(session.capturesOwnAudio, "callers must skip their own capture")
    }

    @Test
    fun `system profile without a context fails fast`() = runTest {
        val controller = DictationController(ConnectionProfile.SystemOnDevice())

        val error = runCatching { controller.openSession() }.exceptionOrNull()

        assertInstanceOf(IllegalStateException::class.java, error)
    }

    @Test
    fun `serverHealthy stays a server-tier probe`() = runTest {
        val controller = DictationController(
            profile = ConnectionProfile.SystemOnDevice(),
            context = mockk<Context>(relaxed = true),
        )

        assertFalse(controller.serverHealthy())
    }
}
