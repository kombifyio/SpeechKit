package helium314.keyboard.speechkit

import android.Manifest
import android.content.pm.PackageManager
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.widget.Toast
import androidx.core.content.ContextCompat
import helium314.keyboard.latin.LatinIME
import kotlinx.coroutines.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.util.concurrent.TimeUnit

/**
 * Voice input with real STT transcription via HuggingFace Inference API.
 *
 * Flow:
 * 1. Tap mic -> start recording (16kHz PCM mono)
 * 2. Tap mic again -> stop recording
 * 3. Convert PCM to WAV
 * 4. Send to HuggingFace whisper-large-v3 API
 * 5. Commit transcribed text to InputConnection
 */
class SpeechKitVoiceInput(private val ime: LatinIME) {

    enum class Mode { DICTATE, ASSIST }

    private var isRecording = false
    private var currentMode = Mode.DICTATE
    private var recordingJob: Job? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private var audioOutput: ByteArrayOutputStream? = null
    private var recorder: AudioRecord? = null

    private val httpClient = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .build()

    fun toggle() {
        currentMode = Mode.DICTATE
        if (isRecording) stopRecording() else startRecording()
    }

    fun toggleAssist() {
        currentMode = Mode.ASSIST
        if (isRecording) stopRecording() else startRecording()
    }

    private fun startRecording() {
        if (!hasMicPermission()) {
            Toast.makeText(ime, "Mikrofon-Berechtigung fehlt. Bitte in App-Einstellungen aktivieren.", Toast.LENGTH_LONG).show()
            return
        }

        isRecording = true
        Toast.makeText(ime, "Aufnahme...", Toast.LENGTH_SHORT).show()

        recordingJob = scope.launch(Dispatchers.IO) {
            try {
                val sampleRate = 16000
                val minBuf = AudioRecord.getMinBufferSize(
                    sampleRate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
                )

                val rec = AudioRecord(
                    MediaRecorder.AudioSource.MIC, sampleRate,
                    AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
                    maxOf(minBuf, sampleRate * 2),
                )

                if (rec.state != AudioRecord.STATE_INITIALIZED) {
                    withContext(Dispatchers.Main) {
                        Toast.makeText(ime, "Mikrofon konnte nicht initialisiert werden", Toast.LENGTH_SHORT).show()
                        isRecording = false
                    }
                    return@launch
                }

                recorder = rec
                audioOutput = ByteArrayOutputStream()
                val buffer = ByteArray(1024)
                rec.startRecording()

                while (isActive && isRecording) {
                    val read = rec.read(buffer, 0, buffer.size)
                    if (read > 0) audioOutput?.write(buffer, 0, read)
                }

                rec.stop()
                rec.release()
                recorder = null

                val pcmData = audioOutput?.toByteArray() ?: ByteArray(0)
                audioOutput = null

                withContext(Dispatchers.Main) {
                    onRecordingDone(pcmData)
                }
            } catch (e: SecurityException) {
                withContext(Dispatchers.Main) {
                    Toast.makeText(ime, "Mikrofon-Zugriff verweigert", Toast.LENGTH_SHORT).show()
                    isRecording = false
                }
            } catch (e: Exception) {
                withContext(Dispatchers.Main) {
                    Toast.makeText(ime, "Aufnahme-Fehler: ${e.message}", Toast.LENGTH_SHORT).show()
                    isRecording = false
                }
            }
        }
    }

    private fun stopRecording() {
        isRecording = false
    }

    private fun onRecordingDone(pcmData: ByteArray) {
        val durationMs = pcmData.size.toLong() * 1000 / (16000 * 2)

        if (durationMs < 300) {
            Toast.makeText(ime, "Zu kurz, nochmal versuchen", Toast.LENGTH_SHORT).show()
            return
        }

        when (currentMode) {
            Mode.DICTATE -> doDictation(pcmData)
            Mode.ASSIST -> doAssist(pcmData)
        }
    }

    /** Dictation: transcribe and insert text directly */
    private fun doDictation(pcmData: ByteArray) {
        Toast.makeText(ime, "Transkribiere...", Toast.LENGTH_SHORT).show()
        scope.launch(Dispatchers.IO) {
            try {
                val wavData = pcmToWav(pcmData)
                val text = transcribeViaHuggingFace(wavData)
                withContext(Dispatchers.Main) {
                    if (text.isNotBlank()) {
                        ime.currentInputConnection?.commitText("$text ", 1)
                    } else {
                        Toast.makeText(ime, "Keine Sprache erkannt", Toast.LENGTH_SHORT).show()
                    }
                }
            } catch (e: Exception) {
                withContext(Dispatchers.Main) {
                    Toast.makeText(ime, "Transkription fehlgeschlagen: ${e.message?.take(80)}", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    /** Assist: transcribe, then send to LLM, then show answer in keyboard panel */
    private fun doAssist(pcmData: ByteArray) {
        Toast.makeText(ime, "Verarbeite Frage...", Toast.LENGTH_SHORT).show()
        scope.launch(Dispatchers.IO) {
            try {
                // Step 1: Transcribe speech to text
                val wavData = pcmToWav(pcmData)
                val question = transcribeViaHuggingFace(wavData)

                if (question.isBlank()) {
                    withContext(Dispatchers.Main) {
                        Toast.makeText(ime, "Keine Frage erkannt", Toast.LENGTH_SHORT).show()
                    }
                    return@launch
                }

                withContext(Dispatchers.Main) {
                    Toast.makeText(ime, "Frage: $question", Toast.LENGTH_SHORT).show()
                }

                // Step 2: Send to LLM for answer
                val answer = askLlm(question)

                // Step 3: Show answer in keyboard panel
                withContext(Dispatchers.Main) {
                    ime.showAssistResult(question, answer)
                }
            } catch (e: Exception) {
                withContext(Dispatchers.Main) {
                    Toast.makeText(ime, "Assist fehlgeschlagen: ${e.message?.take(80)}", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    /** Send question to HuggingFace Chat Completions API */
    private fun askLlm(question: String): String {
        val token = resolveHfToken()
        val model = "meta-llama/Llama-3.2-3B-Instruct"

        val jsonBody = """
            {
                "model": "$model",
                "messages": [
                    {"role": "system", "content": "Du bist ein hilfreicher Assistent. Antworte kurz und praezise auf Deutsch."},
                    {"role": "user", "content": ${org.json.JSONObject.quote(question)}}
                ],
                "max_tokens": 256,
                "temperature": 0.7,
                "stream": false
            }
        """.trimIndent()

        val request = Request.Builder()
            .url("https://router.huggingface.co/v1/chat/completions")
            .header("Authorization", "Bearer $token")
            .header("Content-Type", "application/json")
            .post(jsonBody.toRequestBody("application/json".toMediaType()))
            .build()

        val response = httpClient.newCall(request).execute()
        if (!response.isSuccessful) {
            val err = response.body?.string()?.take(200) ?: "unknown"
            throw RuntimeException("LLM API ${response.code}: $err")
        }

        val json = response.body?.string() ?: throw RuntimeException("Empty response")
        val parsed = org.json.JSONObject(json)
        return parsed.getJSONArray("choices")
            .getJSONObject(0)
            .getJSONObject("message")
            .getString("content")
            .trim()
    }

    /**
     * Send audio to HuggingFace whisper-large-v3 via OpenAI-compatible API.
     */
    private fun transcribeViaHuggingFace(wavData: ByteArray): String {
        val token = resolveHfToken()

        val body = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart(
                "file", "audio.wav",
                wavData.toRequestBody("audio/wav".toMediaType()),
            )
            .addFormDataPart("model", "openai/whisper-large-v3")
            .addFormDataPart("language", "de")
            .addFormDataPart("response_format", "json")
            .build()

        val request = Request.Builder()
            .url("https://router.huggingface.co/v1/audio/transcriptions")
            .header("Authorization", "Bearer $token")
            .post(body)
            .build()

        val response = httpClient.newCall(request).execute()

        if (!response.isSuccessful) {
            val errorBody = response.body?.string()?.take(200) ?: "unknown"
            throw RuntimeException("HF API ${response.code}: $errorBody")
        }

        val json = response.body?.string() ?: throw RuntimeException("Empty response")
        val parsed = JSONObject(json)
        return parsed.optString("text", "").trim()
    }

    /**
     * Resolve HuggingFace token.
     * Priority: SharedPreferences > BuildConfig > environment.
     */
    private fun resolveHfToken(): String {
        // 1. Check SharedPreferences (set via SpeechKit settings UI)
        val prefs = ime.getSharedPreferences("speechkit_config", 0)
        val prefToken = prefs.getString("hf_token", null)
        if (!prefToken.isNullOrBlank()) return prefToken

        // 2. Hardcoded fallback for development (remove before release)
        // Users must set their own token in settings
        throw RuntimeException("HuggingFace Token nicht konfiguriert. Bitte in SpeechKit Settings eintragen.")
    }

    /**
     * Convert raw PCM 16-bit signed mono 16kHz to WAV format.
     */
    private fun pcmToWav(pcm: ByteArray): ByteArray {
        val sampleRate = 16000
        val channels = 1
        val bitsPerSample = 16
        val byteRate = sampleRate * channels * bitsPerSample / 8
        val blockAlign = channels * bitsPerSample / 8
        val dataSize = pcm.size
        val fileSize = 36 + dataSize

        val header = ByteArray(44)
        // RIFF
        header[0] = 'R'.code.toByte(); header[1] = 'I'.code.toByte()
        header[2] = 'F'.code.toByte(); header[3] = 'F'.code.toByte()
        writeLE32(header, 4, fileSize)
        header[8] = 'W'.code.toByte(); header[9] = 'A'.code.toByte()
        header[10] = 'V'.code.toByte(); header[11] = 'E'.code.toByte()
        // fmt
        header[12] = 'f'.code.toByte(); header[13] = 'm'.code.toByte()
        header[14] = 't'.code.toByte(); header[15] = ' '.code.toByte()
        writeLE32(header, 16, 16)
        writeLE16(header, 20, 1) // PCM
        writeLE16(header, 22, channels)
        writeLE32(header, 24, sampleRate)
        writeLE32(header, 28, byteRate)
        writeLE16(header, 32, blockAlign)
        writeLE16(header, 34, bitsPerSample)
        // data
        header[36] = 'd'.code.toByte(); header[37] = 'a'.code.toByte()
        header[38] = 't'.code.toByte(); header[39] = 'a'.code.toByte()
        writeLE32(header, 40, dataSize)

        return header + pcm
    }

    private fun writeLE32(buf: ByteArray, off: Int, v: Int) {
        buf[off] = (v and 0xFF).toByte()
        buf[off + 1] = (v shr 8 and 0xFF).toByte()
        buf[off + 2] = (v shr 16 and 0xFF).toByte()
        buf[off + 3] = (v shr 24 and 0xFF).toByte()
    }

    private fun writeLE16(buf: ByteArray, off: Int, v: Int) {
        buf[off] = (v and 0xFF).toByte()
        buf[off + 1] = (v shr 8 and 0xFF).toByte()
    }

    private fun hasMicPermission(): Boolean =
        ContextCompat.checkSelfPermission(ime, Manifest.permission.RECORD_AUDIO) ==
            PackageManager.PERMISSION_GRANTED

    fun release() {
        scope.cancel()
        recorder?.stop()
        recorder?.release()
        recorder = null
    }
}
