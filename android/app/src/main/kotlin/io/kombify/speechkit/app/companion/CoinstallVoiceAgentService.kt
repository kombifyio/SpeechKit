package io.kombify.speechkit.app.companion

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.os.Binder
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.ParcelFileDescriptor
import io.kombify.speechkit.R
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.audio.PcmStreamPlayer
import io.kombify.speechkit.coinstall.voiceagent.v1.IVoiceAgentCallback
import io.kombify.speechkit.coinstall.voiceagent.v1.IVoiceAgentService
import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentCapability
import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentContract
import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentSessionRequest
import io.kombify.speechkit.net.CreateVoiceAgentSessionResponse
import io.kombify.speechkit.net.VoiceAgentAudio
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentSession
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentWsClient
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.turn.TurnEngine
import io.kombify.speechkit.turn.TurnEvent
import java.net.URI
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** One attested, ticket-only media handoff. The Companion continues to own account and chat. */
class CoinstallVoiceAgentService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private lateinit var identity: VoiceAgentCallerIdentity
    private lateinit var audioManager: AudioManager
    private var active: Running? = null

    private class Running(val uid: Int, val id: String, val callback: IVoiceAgentCallback, val source: ParcelFileDescriptor?) {
        val player = PcmStreamPlayer(VoiceAgentAudio.SERVER_SAMPLE_RATE)
        val engine = TurnEngine()
        var live: VoiceAgentSession? = null
        var work: Job? = null
        var capture: Job? = null
        var focus: AudioFocusRequest? = null
        var engineEndedTurn = false
        var uplinkBytes = 0L
        var playbackBytes = 0L
        var ended = false
        lateinit var death: IBinder.DeathRecipient
    }

    override fun onCreate() {
        super.onCreate()
        identity = VoiceAgentCallerIdentity(packageManager)
        audioManager = getSystemService(AudioManager::class.java)
    }
    private val binder = object : IVoiceAgentService.Stub() {
        override fun getContractVersion(): Int = VoiceAgentContract.VERSION
        override fun getCapability(): VoiceAgentCapability {
            val trusted = attested(Binder.getCallingUid())
            val hosted = hostedOrigin() != null
            val mic = checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
            return VoiceAgentCapability().apply {
                available = trusted && hosted
                unavailableReason = when {
                    !trusted -> VoiceAgentContract.ERROR_CALLER_NOT_ATTESTED
                    !hosted -> VoiceAgentContract.ERROR_HOSTED_ORIGIN_UNAVAILABLE
                    else -> ""
                }
                microphonePermission = mic
            }
        }
        override fun startSession(request: VoiceAgentSessionRequest, callback: IVoiceAgentCallback) {
            val uid = Binder.getCallingUid()
            if (!attested(uid)) {
                reject(callback, request.sessionId, VoiceAgentContract.ERROR_CALLER_NOT_ATTESTED)
                request.audioSource?.close()
                return
            }
            if (!validTicket(request)) {
                reject(callback, request.sessionId, VoiceAgentContract.ERROR_TICKET_INVALID)
                request.audioSource?.close()
                return
            }
            if (request.audioSource == null && checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
                reject(callback, request.sessionId, VoiceAgentContract.ERROR_MICROPHONE_PERMISSION)
                return
            }
            scope.launch {
                if (active != null) {
                    reject(callback, request.sessionId, VoiceAgentContract.ERROR_SESSION_BUSY)
                    request.audioSource?.close()
                } else begin(uid, request, callback)
            }
        }
        override fun stopSession(sessionId: String) {
            val uid = Binder.getCallingUid()
            if (!attested(uid)) return
            scope.launch { active?.takeIf { it.uid == uid && it.id == sessionId }?.let { finish(it, "client") } }
        }
        override fun interruptSession(sessionId: String) {
            val uid = Binder.getCallingUid()
            if (!attested(uid)) return
            scope.launch {
                active?.takeIf { it.uid == uid && it.id == sessionId }?.let {
                    it.player.flush(); it.engine.notePlaybackStopped(); it.live?.cancelReply()
                }
            }
        }
    }
    override fun onBind(intent: Intent): IBinder? = binder.takeIf { intent.action == VoiceAgentContract.BIND_ACTION }
    override fun onUnbind(intent: Intent): Boolean { active?.let { finish(it, "client") }; return false }
    override fun onDestroy() { active?.let { finish(it, "client") }; scope.cancel(); super.onDestroy() }
    private fun attested(uid: Int): Boolean = Build.VERSION.SDK_INT >= 28 && identity.attested(uid)

    private fun begin(uid: Int, request: VoiceAgentSessionRequest, callback: IVoiceAgentCallback) {
        val run = Running(uid, request.sessionId, callback, request.audioSource)
        run.death = IBinder.DeathRecipient { scope.launch { finish(run, "client") } }
        try { callback.asBinder().linkToDeath(run.death, 0) } catch (_: Throwable) { request.audioSource?.close(); return }
        if (run.ended) return
        active = run
        run.work = scope.launch(start = CoroutineStart.LAZY) {
            if (run.ended) return@launch
            try {
                startAudioNotification(microphone = request.audioSource == null)
                val focus = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT)
                    .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_ASSISTANT)
                        .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
                    .setOnAudioFocusChangeListener({ change ->
                        if (change == AudioManager.AUDIOFOCUS_LOSS || change == AudioManager.AUDIOFOCUS_LOSS_TRANSIENT) {
                            scope.launch { finish(run, "client") }
                        }
                    }, Handler(Looper.getMainLooper())).build()
                check(audioManager.requestAudioFocus(focus) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED)
                run.focus = focus
                val live = VoiceAgentWsClient().connect(CreateVoiceAgentSessionResponse(
                    sessionId = request.sessionId, wsUrl = request.wsUrl, ticket = request.ticket))
                run.live = live
                live.start(VoiceAgentStartFrame(locale = request.locale.takeIf { LOCALE.matches(it) }))
                notify(run) { it.onState(run.id, VoiceAgentContract.STATE_CONNECTING) }
                if (run.ended) return@launch
                run.capture = launch {
                    try {
                    val file = request.audioSource
                    if (file != null) {
                        val pcm = readPcm(file)
                        for (offset in pcm.indices step 3200) {
                            if (run.ended) break
                            val chunk = pcm.copyOfRange(offset, minOf(offset + 3200, pcm.size))
                            live.sendAudio(chunk)
                            run.uplinkBytes += chunk.size
                            delay(100)
                        }
                        if (!run.ended) live.endTurn()
                    } else {
                        val mic = MicAudioCapture(duplex = true)
                        mic.frames().collect { frame ->
                            run.engine.noteEchoControl(mic.echoControl)
                            run.engine.offer(frame).forEach { event -> when (event) {
                                is TurnEvent.TurnAudio -> { live.sendAudio(event.pcm); run.uplinkBytes += event.pcm.size }
                                is TurnEvent.TurnEnded -> { run.engineEndedTurn = true; live.endTurn() }
                                else -> Unit
                            } }
                        }
                    }
                    } catch (cancelled: CancellationException) { throw cancelled
                    } catch (_: Throwable) {
                        notify(run) { it.onError(run.id, VoiceAgentContract.ERROR_TRANSPORT, "", true) }
                        finish(run, "server_error")
                    }
                }
                live.events.collect { event ->
                    if (!run.ended) when (event) {
                        is VoiceAgentEvent.State -> notify(run) { it.onState(run.id, event.state) }
                        is VoiceAgentEvent.Transcript -> {
                            if (event.input && event.done) {
                                if (!run.engineEndedTurn) run.engine.noteProviderTurnEnd()
                                run.engineEndedTurn = false
                            }
                            notify(run) { it.onTranscript(run.id, if (event.input) VoiceAgentContract.ROLE_USER else VoiceAgentContract.ROLE_AGENT, event.text.takeLast(4000), event.done) }
                        }
                        is VoiceAgentEvent.Audio -> {
                            run.engine.notePlaybackFrame(event.pcm, VoiceAgentAudio.SERVER_SAMPLE_RATE)
                            run.player.play(event.pcm)
                            run.playbackBytes += event.pcm.size
                        }
                        VoiceAgentEvent.Interrupted -> { run.player.flush(); run.engine.notePlaybackStopped() }
                        is VoiceAgentEvent.Failure -> notify(run) { it.onError(run.id, VoiceAgentContract.ERROR_SERVER, "", false) }
                        is VoiceAgentEvent.Closed -> finish(run, event.reason)
                        is VoiceAgentEvent.ToolCall -> Unit // The registered agent runtime owns tool execution.
                    }
                }
                finish(run, "closed")
            } catch (cancelled: CancellationException) { throw cancelled
            } catch (_: Throwable) {
                notify(run) { it.onError(run.id, VoiceAgentContract.ERROR_TRANSPORT, "", true) }
                finish(run, "server_error")
            } finally { runCatching { request.audioSource?.close() } }
        }
        run.work?.start()
    }
    private fun finish(run: Running, reason: String) {
        if (run.ended) return
        run.ended = true
        VoiceLog.i(VoiceLog.AGENT, "coinstall_end reason=${reason.take(64)} uplink_bytes=${run.uplinkBytes} playback_bytes=${run.playbackBytes}")
        run.capture?.cancel()
        runCatching { run.source?.close() }
        run.player.release()
        run.focus?.let { audioManager.abandonAudioFocusRequest(it) }
        run.live?.let { live -> scope.launch { runCatching { live.close() } } }
        run.work?.cancel()
        runCatching { run.callback.onEnded(run.id, reason.take(64)) }
        runCatching { run.callback.asBinder().unlinkToDeath(run.death, 0) }
        if (active === run) active = null
        stopForeground(STOP_FOREGROUND_REMOVE)
    }
    private fun notify(run: Running, send: (IVoiceAgentCallback) -> Unit) {
        if (!run.ended) try { send(run.callback) } catch (_: Throwable) { finish(run, "client") }
    }
    private fun reject(callback: IVoiceAgentCallback, id: String, code: String) {
        runCatching { callback.onError(id.take(128), code, "", true); callback.onEnded(id.take(128), "server_error") }
    }
    private fun startAudioNotification(microphone: Boolean) {
        val notifications = getSystemService(NotificationManager::class.java)
        notifications.createNotificationChannel(NotificationChannel(CHANNEL, getString(R.string.dev_va_title), NotificationManager.IMPORTANCE_LOW))
        val notification = Notification.Builder(this, CHANNEL).setSmallIcon(android.R.drawable.ic_btn_speak_now)
            .setContentTitle(getString(R.string.dev_va_title)).setOngoing(true).build()
        if (Build.VERSION.SDK_INT >= 30) {
            startForeground(VOICE_NOTIFICATION, notification, if (microphone) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
                else ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK)
        } else startForeground(VOICE_NOTIFICATION, notification)
    }
    private fun validTicket(request: VoiceAgentSessionRequest): Boolean = runCatching {
        val url = URI(request.wsUrl)
        val origin = hostedOrigin() ?: return@runCatching false
        ID.matches(request.sessionId) && ID.matches(request.aiSessionId) && TICKET.matches(request.ticket) &&
            url.scheme == "wss" && url.host == origin.host && url.port == -1 && url.rawQuery == null &&
            url.rawFragment == null && url.rawUserInfo == null && url.rawPath == "/v1/speechkit/voiceagent/sessions/${request.sessionId}/ws"
    }.getOrDefault(false)
    private fun hostedOrigin(): URI? = runCatching { URI(BuildConfig.HOSTED_VOICE_ORIGIN) }.getOrNull()?.takeIf {
        it.scheme == "https" && !it.host.isNullOrEmpty() && it.port == -1 &&
            it.rawPath.orEmpty() in listOf("", "/") && it.rawQuery == null && it.rawFragment == null && it.rawUserInfo == null
    }
    private suspend fun readPcm(source: ParcelFileDescriptor): ByteArray = withContext(Dispatchers.IO) {
        val bytes = ParcelFileDescriptor.AutoCloseInputStream(source).use { input ->
            val output = ByteArrayOutputStream()
            val buffer = ByteArray(8192)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                require(output.size() + count <= MAX_SOURCE_BYTES)
                output.write(buffer, 0, count)
            }
            output.toByteArray()
        }
        require(bytes.size in 2..MAX_SOURCE_BYTES)
        if (bytes.size >= 44 && bytes.copyOfRange(0, 4).contentEquals("RIFF".toByteArray())) {
            val header = ByteBuffer.wrap(bytes).order(ByteOrder.LITTLE_ENDIAN)
            require(String(bytes, 8, 4) == "WAVE" && String(bytes, 12, 4) == "fmt " && header.getInt(16) == 16 &&
                header.getShort(20).toInt() == 1 && header.getShort(22).toInt() == 1 && header.getInt(24) == 16000 &&
                header.getShort(34).toInt() == 16 && String(bytes, 36, 4) == "data" && header.getInt(40) == bytes.size - 44)
            bytes.copyOfRange(44, bytes.size).also { require(it.size % 2 == 0) }
        } else bytes.also { require(it.size % 2 == 0) }
    }
    companion object {
        private val ID = Regex("^[A-Za-z0-9_-]{1,128}$")
        private val TICKET = Regex("^[A-Za-z0-9_-]{16,1024}$")
        private val LOCALE = Regex("^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$")
        private const val CHANNEL = "companion-voice"
        private const val VOICE_NOTIFICATION = 8101
        private const val MAX_SOURCE_BYTES = 16000 * 2 * 120 + 44
    }
}
