package io.kombify.speechkit.audio

import kotlinx.coroutines.flow.Flow

/**
 * Cold PCM source: recording starts when the flow is collected and stops when
 * collection is cancelled.
 *
 * Shared by the keyboard panel, the Voice Agent surfaces, the in-app test
 * screens, and the system assistant so each host does not own its own
 * AudioRecord loop. [AudioSession] is the start/stop buffer adapter over
 * the same capture, used by [io.kombify.speechkit.dictation.DefaultDictationSession].
 */
fun interface AudioCapture {
    fun frames(): Flow<ByteArray>
}
