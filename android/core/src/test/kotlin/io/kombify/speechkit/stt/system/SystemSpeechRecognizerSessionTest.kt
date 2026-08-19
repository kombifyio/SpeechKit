package io.kombify.speechkit.stt.system

import android.speech.SpeechRecognizer
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * FSM tests for the system STT tier against a fake [SpeechRecognizerHandle]
 * (the seam exists exactly because Robolectric is deliberately not part of
 * this repo's test stack). `SpeechRecognizer.ERROR_*`/`RESULTS_*` are
 * compile-time constants, so referencing them here needs no Android runtime.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SystemSpeechRecognizerSessionTest {

    private class FakeHandle : SpeechRecognizerHandle {
        val requests = mutableListOf<RecognitionRequest>()
        var callbacks: SpeechRecognizerHandle.Callbacks? = null
        var stopCalls = 0
        var cancelCalls = 0
        var destroyCalls = 0

        override fun startListening(
            request: RecognitionRequest,
            callbacks: SpeechRecognizerHandle.Callbacks,
        ) {
            requests += request
            this.callbacks = callbacks
        }

        override fun stopListening() {
            stopCalls += 1
        }

        override fun cancel() {
            cancelCalls += 1
        }

        override fun destroy() {
            destroyCalls += 1
        }
    }

    private class Harness(scope: TestScope, preferOffline: Boolean = true) {
        val handle = FakeHandle()
        var factoryCalls = 0
        var factoryError: Throwable? = null
        val session = SystemSpeechRecognizerSession(
            handleFactory = {
                factoryCalls += 1
                factoryError?.let { throw it }
                handle
            },
            preferOffline = preferOffline,
            mainDispatcher = StandardTestDispatcher(scope.testScheduler),
        )
        val events = mutableListOf<TranscriptEvent>()

        init {
            scope.backgroundScope.launch { session.events.collect { events += it } }
        }

        fun callbacks(): SpeechRecognizerHandle.Callbacks = checkNotNull(handle.callbacks)
    }

    @Test
    fun `captures its own audio and treats sendAudio as a no-op`() = runTest {
        val h = Harness(this)
        assertTrue(h.session.capturesOwnAudio)

        h.session.startSegment(DictationSegmentOptions())
        runCurrent()
        h.session.sendAudio(ByteArray(3200))
        runCurrent()

        // Only the startListening interaction; no audio crossed the seam.
        assertEquals(1, h.handle.requests.size)
        assertTrue(h.session.keepAlive(), "sessionless tier has no watchdog to fail")
    }

    @Test
    fun `startSegment maps bare language codes to BCP-47 request tags`() = runTest {
        val h = Harness(this)

        h.session.startSegment(DictationSegmentOptions(language = "de"))
        runCurrent()
        h.callbacks().onResult("")
        runCurrent()
        h.session.startSegment(DictationSegmentOptions(language = "en"))
        runCurrent()
        h.callbacks().onResult("")
        runCurrent()
        h.session.startSegment(DictationSegmentOptions(language = "de-CH"))
        runCurrent()

        assertEquals(
            listOf("de-DE", "en-US", "de-CH"),
            h.handle.requests.map { it.languageTag },
        )
        assertTrue(h.handle.requests.all { it.partialResults && it.preferOffline })
    }

    @Test
    fun `partials become drafts and results become final plus segment done`() = runTest {
        val h = Harness(this)
        h.session.startSegment(DictationSegmentOptions(language = "de"))
        runCurrent()

        h.callbacks().onReady()
        h.callbacks().onPartial("hallo")
        h.callbacks().onPartial("hallo welt")
        h.callbacks().onResult("Hallo Welt.")
        runCurrent()

        assertEquals(
            listOf(
                TranscriptEvent.SegmentReady(1),
                TranscriptEvent.Draft(1, "hallo", 0),
                TranscriptEvent.Draft(1, "hallo welt", 1),
                TranscriptEvent.Final(1, "Hallo Welt.", 2),
                TranscriptEvent.SegmentDone(1),
            ),
            h.events,
        )
    }

    @Test
    fun `no match and speech timeout map to an empty final, not a failure`() = runTest {
        val h = Harness(this)

        for (code in listOf(
            SpeechRecognizer.ERROR_NO_MATCH,
            SpeechRecognizer.ERROR_SPEECH_TIMEOUT,
        )) {
            h.events.clear()
            h.session.startSegment(DictationSegmentOptions())
            runCurrent()
            h.callbacks().onError(code)
            runCurrent()

            assertEquals(2, h.events.size, "error $code")
            val final = assertInstanceOf(TranscriptEvent.Final::class.java, h.events[0])
            assertEquals("", final.text)
            assertInstanceOf(TranscriptEvent.SegmentDone::class.java, h.events[1])
        }
    }

    @Test
    fun `recognizer errors map to stable failure codes`() = runTest {
        val h = Harness(this)
        val cases = mapOf(
            SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS to "mic_permission_denied",
            SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED to "language_not_supported",
            SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE to "language_not_supported",
            SpeechRecognizer.ERROR_NETWORK to "network",
            SpeechRecognizer.ERROR_NETWORK_TIMEOUT to "network",
            SpeechRecognizer.ERROR_SERVER to "network",
            SpeechRecognizer.ERROR_SERVER_DISCONNECTED to "network",
            SpeechRecognizer.ERROR_RECOGNIZER_BUSY to "busy",
            SpeechRecognizer.ERROR_AUDIO to "audio_capture_failed",
            SpeechRecognizer.ERROR_CLIENT to "recognizer_error",
        )

        for ((errorCode, expected) in cases) {
            h.events.clear()
            h.session.startSegment(DictationSegmentOptions())
            runCurrent()
            h.callbacks().onError(errorCode)
            runCurrent()

            val failure = assertInstanceOf(
                TranscriptEvent.Failure::class.java,
                h.events.single(),
                "error $errorCode",
            )
            assertEquals(expected, failure.code, "error $errorCode")
        }
    }

    @Test
    fun `a failure frees the session for the next segment`() = runTest {
        val h = Harness(this)
        h.session.startSegment(DictationSegmentOptions())
        runCurrent()
        h.callbacks().onError(SpeechRecognizer.ERROR_RECOGNIZER_BUSY)
        runCurrent()

        // Same session, next mic press: a fresh segment id, recognizer reused.
        h.session.startSegment(DictationSegmentOptions())
        runCurrent()
        h.callbacks().onResult("again")
        runCurrent()

        assertEquals(1, h.factoryCalls, "recognizer must be reused across segments")
        assertTrue(h.events.contains(TranscriptEvent.Final(2, "again", 0)))
    }

    @Test
    fun `starting a segment while one is active throws`() = runTest {
        val h = Harness(this)
        h.session.startSegment(DictationSegmentOptions())
        runCurrent()

        val error = runCatching { h.session.startSegment(DictationSegmentOptions()) }
            .exceptionOrNull()

        assertInstanceOf(IllegalStateException::class.java, error)
    }

    @Test
    fun `finishSegment stops listening and late results still land`() = runTest {
        val h = Harness(this)
        h.session.startSegment(DictationSegmentOptions())
        runCurrent()

        h.session.finishSegment()
        runCurrent()
        assertEquals(1, h.handle.stopCalls)

        // The flushed final arrives after stopListening.
        h.callbacks().onResult("flushed")
        runCurrent()
        assertEquals(
            listOf(
                TranscriptEvent.Final(1, "flushed", 0),
                TranscriptEvent.SegmentDone(1),
            ),
            h.events,
        )

        // Idle finish is a no-op.
        h.session.finishSegment()
        runCurrent()
        assertEquals(1, h.handle.stopCalls)
    }

    @Test
    fun `late callbacks from a completed segment are dropped`() = runTest {
        val h = Harness(this)
        h.session.startSegment(DictationSegmentOptions())
        runCurrent()
        val firstSegment = h.callbacks()
        firstSegment.onResult("done")
        runCurrent()

        // Late echoes from the platform after the segment terminated.
        firstSegment.onPartial("late")
        firstSegment.onResult("late final")
        firstSegment.onError(SpeechRecognizer.ERROR_CLIENT)
        runCurrent()

        assertEquals(
            listOf(
                TranscriptEvent.Final(1, "done", 0),
                TranscriptEvent.SegmentDone(1),
            ),
            h.events,
        )
    }

    @Test
    fun `close cancels and destroys the recognizer and emits Closed`() = runTest {
        val h = Harness(this)
        h.session.startSegment(DictationSegmentOptions())
        runCurrent()

        h.session.close()
        runCurrent()

        assertEquals(1, h.handle.cancelCalls)
        assertEquals(1, h.handle.destroyCalls)
        assertEquals(TranscriptEvent.Closed("client"), h.events.last())

        val error = runCatching { h.session.startSegment(DictationSegmentOptions()) }
            .exceptionOrNull()
        assertInstanceOf(IllegalStateException::class.java, error)
    }

    @Test
    fun `a failing recognizer factory surfaces as a failure event, not a crash`() = runTest {
        val h = Harness(this)
        h.factoryError = IllegalStateException("no speech recognition service")

        h.session.startSegment(DictationSegmentOptions())
        runCurrent()

        val failure = assertInstanceOf(TranscriptEvent.Failure::class.java, h.events.single())
        assertEquals(SystemSpeechRecognizerSession.ERROR_RECOGNIZER_UNAVAILABLE, failure.code)

        // The session recovers once a recognizer can be created (e.g. the
        // user installed a recognition service): same session, next press.
        h.factoryError = null
        h.session.startSegment(DictationSegmentOptions())
        runCurrent()
        assertEquals(1, h.handle.requests.size)
    }
}
