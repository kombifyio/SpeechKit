package io.kombify.speechkit.assistant.service

import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.ByteArrayOutputStream

/**
 * The assistant listen path must use the shared streaming session, not a
 * private AudioRecord+batch STT stack. A session that owns the mic must not
 * pull a second capture; a pushed-PCM session must receive the capture frames.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class UtteranceTranscriberTest {

    private class FakeSession(
        override val capturesOwnAudio: Boolean,
    ) : StreamingSttSession {
        val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
        override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()
        val sentAudio = ByteArrayOutputStream()
        var finishCalls = 0
        var started = 0

        override suspend fun startSegment(options: DictationSegmentOptions) {
            started += 1
        }

        override suspend fun sendAudio(pcm: ByteArray) {
            sentAudio.write(pcm)
        }

        override suspend fun finishSegment() {
            finishCalls += 1
        }

        override suspend fun close() {
            channel.trySend(TranscriptEvent.Closed("client"))
            channel.close()
        }
    }

    @Test
    fun `on-device sessions return the final without a second capture`() = runTest {
        val session = FakeSession(capturesOwnAudio = true)
        session.channel.trySend(TranscriptEvent.Final(segmentId = 1, text = "hello"))
        val outcome = UtteranceTranscriber(
            sessionFactory = { session },
            audioCapture = AudioCapture { flowOf(byteArrayOf(1, 2, 3, 4)) },
        ).transcribe()
        assertEquals("hello", outcome.text)
        assertEquals(UtteranceResult.Reason.HEARD, outcome.reason)
        assertEquals(0, session.sentAudio.size())
        assertEquals(0, session.finishCalls)
        assertEquals(1, session.started)
    }

    @Test
    fun `pushed-pcm sessions receive capture frames then finish`() = runTest {
        val session = FakeSession(capturesOwnAudio = false)
        session.channel.trySend(TranscriptEvent.Final(segmentId = 1, text = "hi"))
        val outcome = UtteranceTranscriber(
            sessionFactory = { session },
            audioCapture = AudioCapture { flowOf(ByteArray(3_200)) },
        ).transcribe()
        assertEquals("hi", outcome.text)
        assertEquals(UtteranceResult.Reason.HEARD, outcome.reason)
        assertTrue(session.sentAudio.size() > 0)
        assertEquals(1, session.finishCalls)
    }

    @Test
    fun `silent hanging capture returns no speech without waiting for stt`() = runTest {
        val session = FakeSession(capturesOwnAudio = false)
        val outcome = UtteranceTranscriber(
            sessionFactory = { session },
            audioCapture = AudioCapture {
                flow { awaitCancellation() }
            },
        ).transcribe()
        assertEquals(UtteranceResult.Reason.NO_SPEECH, outcome.reason)
        assertEquals("", outcome.text)
        assertEquals(0, session.finishCalls)
    }
}
