package io.kombify.speechkit.ime

import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.util.concurrent.atomic.AtomicInteger
import kotlin.coroutines.CoroutineContext
import timber.log.Timber

/**
 * Plays the agent's spoken answer inside the keyboard window.
 *
 * The track is created lazily and released when the conversation ends: an
 * input method is started and stopped constantly as the user moves between
 * fields, and holding an open audio track across that would keep the audio
 * focus of a keyboard that is not even visible.
 *
 * ## Threading
 *
 * [play] is `suspend` and moves onto [playback] itself rather than trusting
 * every caller to remember. `AudioTrack.write` in `MODE_STREAM` blocks until
 * the whole chunk has been queued against a [MIN_BUFFER_BYTES] buffer, and
 * both hosts drive this from a Compose effect — that is `AndroidUiDispatcher`,
 * the main thread of an IME window, where a 400 ms block is a frozen keyboard.
 *
 * Every touch of the track then goes through one [Mutex], and [flush] and
 * [release] hand their work to the same context instead of reaching into the
 * track from the caller's thread. Releasing a track while another thread sits
 * inside `write` is a native crash, and that race did not exist while playback
 * was on the main thread. The cost is that a barge-in flush waits for the
 * write in flight, so up to one buffer of the abandoned answer is still heard;
 * dropping the frames that have not been handed over yet is the caller's job
 * (`ImeVoiceAgentController.discardPendingAudio`) and it is the larger half.
 *
 * Moving [release] onto that context also made it lose its ordering against a
 * [play] that was already in flight, and [play] creates the track it does not
 * find. A frame that lost the mutex to the release behind it would therefore
 * build and start a *new* track — one the host can no longer reach, because
 * `hidePanel` drops its reference to this player in the next statement, so it
 * would read out the abandoned answer to the end and never be released.
 * [releases] is the ordering the dispatcher no longer provides: a frame
 * remembers how many releases had happened when it was handed over, and one
 * that arrives across a release boundary is dropped instead of resurrecting
 * the track. A [play] issued *after* a release still creates one, which is
 * what keeps a released player reusable.
 *
 * Public rather than internal because the keyboard adapter that mounts this
 * panel inside HeliBoard's input view lives in `:app`, on the other side of
 * the licence boundary, and must not build a second player of its own.
 */
class ImeAgentAudioPlayer(
    private val sampleRateHz: Int = 16_000,
    private val playback: CoroutineContext = Dispatchers.IO,
) {
    // Not cancelled by release(): a released player is reusable by design, and
    // the scope holds no thread of its own while nothing is queued on it.
    private val scope = CoroutineScope(SupervisorJob() + playback)
    private val trackLock = Mutex()
    private var track: AudioTrack? = null

    // How many times this player has been released. Read at the call site,
    // before anything can suspend, so a frame carries the state of the world
    // it was produced in rather than the one it wakes up into.
    private val releases = AtomicInteger()

    suspend fun play(pcm: ByteArray) {
        if (pcm.isEmpty()) return
        val issued = releases.get()
        withContext(playback) {
            trackLock.withLock {
                // Released after this frame was handed over: the conversation
                // is over and the host has already let go of this player.
                // Creating a track here would leak it and read out an answer
                // nobody is listening to any more.
                if (releases.get() != issued) return@withLock
                val active = track ?: create().also { track = it }
                runCatching { active.write(pcm, 0, pcm.size) }
                    .onFailure { Timber.w(it, "agent audio playback failed") }
            }
        }
    }

    /**
     * Drops speech that is buffered but not yet heard. Barge-in cuts the
     * agent mid-sentence; without this the track would keep reading out an
     * answer the server has already abandoned. The track is resumed straight
     * away because `flush()` leaves it paused and the next answer writes into
     * the same track.
     */
    fun flush() {
        scope.launch {
            trackLock.withLock {
                val active = track ?: return@withLock
                runCatching {
                    active.pause()
                    active.flush()
                    active.play()
                }.onFailure { Timber.w(it, "agent audio flush failed") }
            }
        }
    }

    fun release() {
        // Bumped here rather than inside the coroutine: this is the point the
        // caller stops owning the player, and every frame handed over before
        // it must be refused even if the coroutine runs much later.
        releases.incrementAndGet()
        scope.launch {
            trackLock.withLock {
                runCatching {
                    track?.stop()
                    track?.release()
                }
                track = null
            }
        }
    }

    private fun create(): AudioTrack {
        val minBuffer = AudioTrack.getMinBufferSize(
            sampleRateHz,
            AudioFormat.CHANNEL_OUT_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
        )
        return AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANT)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setSampleRate(sampleRateHz)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(minBuffer, MIN_BUFFER_BYTES))
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
            .also { it.play() }
    }

    private companion object {
        const val MIN_BUFFER_BYTES = 12_800 // 400 ms of 16 kHz S16 mono
    }
}
