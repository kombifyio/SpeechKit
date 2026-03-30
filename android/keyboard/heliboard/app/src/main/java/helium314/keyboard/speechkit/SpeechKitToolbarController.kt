package helium314.keyboard.speechkit

import android.Manifest
import android.content.pm.PackageManager
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.ImageButton
import android.widget.TextView
import androidx.core.content.ContextCompat
import helium314.keyboard.latin.LatinIME
import helium314.keyboard.latin.R
import kotlinx.coroutines.*
import java.io.ByteArrayOutputStream

/**
 * Manages the SpeechKit voice toolbar within HeliBoard.
 *
 * Handles:
 * - Mic button tap -> start/stop recording
 * - Audio capture (16kHz S16 mono)
 * - Status text updates
 * - Permission checks
 *
 * This is intentionally a minimal integration layer. Full STT routing,
 * VAD, and AI features are handled by the speechkit-core module.
 * For the initial HeliBoard integration, we capture audio and commit
 * the transcription result directly to the InputConnection.
 */
class SpeechKitToolbarController(
    private val latinIME: LatinIME,
) {
    private var micButton: ImageButton? = null
    private var statusText: TextView? = null
    private var summarizeButton: ImageButton? = null
    private var spacer: View? = null

    private var isRecording = false
    private var recordingJob: Job? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private val handler = Handler(Looper.getMainLooper())

    fun attach(view: View) {
        micButton = view.findViewById(R.id.speechkit_mic_button)
        statusText = view.findViewById(R.id.speechkit_status_text)
        summarizeButton = view.findViewById(R.id.speechkit_summarize_button)
        spacer = view.findViewById(R.id.speechkit_spacer)

        micButton?.setOnClickListener { toggleRecording() }
        summarizeButton?.setOnClickListener { /* Phase 5: LLM summarize */ }
    }

    fun detach() {
        scope.cancel()
        micButton = null
        statusText = null
        summarizeButton = null
    }

    private fun toggleRecording() {
        if (isRecording) {
            stopRecording()
        } else {
            startRecording()
        }
    }

    private fun startRecording() {
        if (!hasMicPermission()) {
            showStatus(latinIME.getString(R.string.speechkit_error_mic))
            return
        }

        isRecording = true
        updateMicIcon()
        showStatus(latinIME.getString(R.string.speechkit_listening))

        recordingJob = scope.launch(Dispatchers.IO) {
            try {
                val sampleRate = 16000
                val bufferSize = AudioRecord.getMinBufferSize(
                    sampleRate,
                    AudioFormat.CHANNEL_IN_MONO,
                    AudioFormat.ENCODING_PCM_16BIT,
                ).coerceAtLeast(sampleRate * 2)

                val recorder = AudioRecord(
                    MediaRecorder.AudioSource.MIC,
                    sampleRate,
                    AudioFormat.CHANNEL_IN_MONO,
                    AudioFormat.ENCODING_PCM_16BIT,
                    bufferSize,
                )

                if (recorder.state != AudioRecord.STATE_INITIALIZED) {
                    withContext(Dispatchers.Main) {
                        showStatus("Audio init failed")
                        isRecording = false
                        updateMicIcon()
                    }
                    return@launch
                }

                val output = ByteArrayOutputStream()
                val buffer = ByteArray(1024)
                recorder.startRecording()

                while (isActive && isRecording) {
                    val read = recorder.read(buffer, 0, buffer.size)
                    if (read > 0) output.write(buffer, 0, read)
                }

                recorder.stop()
                recorder.release()

                val audioData = output.toByteArray()
                withContext(Dispatchers.Main) {
                    onRecordingComplete(audioData)
                }
            } catch (e: SecurityException) {
                withContext(Dispatchers.Main) {
                    showStatus(latinIME.getString(R.string.speechkit_error_mic))
                    isRecording = false
                    updateMicIcon()
                }
            } catch (e: Exception) {
                withContext(Dispatchers.Main) {
                    showStatus("Error: ${e.message}")
                    isRecording = false
                    updateMicIcon()
                }
            }
        }
    }

    private fun stopRecording() {
        isRecording = false
        updateMicIcon()
        showStatus(latinIME.getString(R.string.speechkit_processing))
        // recordingJob will exit its loop and call onRecordingComplete
    }

    private fun onRecordingComplete(audioData: ByteArray) {
        val durationMs = audioData.size.toLong() * 1000 / (16000 * 2)

        if (durationMs < 200) {
            // Too short -- probably a mis-tap
            hideStatus()
            return
        }

        // TODO: Route through SttRouter (Phase 5 integration)
        // For now, show a placeholder message
        showStatus("${durationMs}ms audio captured")

        // Auto-hide status after 3 seconds
        handler.postDelayed({ hideStatus() }, 3000)
    }

    private fun hasMicPermission(): Boolean {
        return ContextCompat.checkSelfPermission(
            latinIME, Manifest.permission.RECORD_AUDIO
        ) == PackageManager.PERMISSION_GRANTED
    }

    private fun updateMicIcon() {
        micButton?.let { btn ->
            if (isRecording) {
                btn.setColorFilter(0xFFE53935.toInt()) // Red when recording
                btn.alpha = 1.0f
            } else {
                btn.clearColorFilter()
                btn.alpha = 0.7f
            }
        }
    }

    private fun showStatus(text: String) {
        statusText?.text = text
        statusText?.visibility = View.VISIBLE
        spacer?.visibility = View.GONE
    }

    private fun hideStatus() {
        statusText?.visibility = View.GONE
        spacer?.visibility = View.VISIBLE
    }
}
