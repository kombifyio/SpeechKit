package io.kombify.speechkit.app.companion

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.net.Uri
import android.os.Binder
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import io.kombify.speechkit.R
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.audio.MicAudioCapture
import io.kombify.speechkit.audio.PcmPlaybackQueue
import io.kombify.speechkit.audio.PcmPlaybackException
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
import io.kombify.speechkit.net.VoiceAgentSetupException
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentWsClient
import io.kombify.speechkit.net.safeVoiceAgentCode
import io.kombify.speechkit.net.voiceAgentFatalEndReason
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.turn.TurnEngine
import io.kombify.speechkit.turn.TurnEvent
import java.net.URI
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext

/** One attested, ticket-only media handoff. The Companion continues to own account and chat. */
class CoinstallVoiceAgentService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private lateinit var identity: VoiceAgentCallerIdentity
    private lateinit var audioManager: AudioManager
    private var active: Running? = null
    private var pending: Prepared? = null
    private var lastStartId = 0

    private class Prepared(
        val run: Running, val request: VoiceAgentSessionRequest,
        val identity: Uri, val intent: PendingIntent, val deadline: Long,
    ) { var expiry: Job? = null }

    private class Running(val uid: Int, val id: String, val callback: IVoiceAgentCallback, val source: ParcelFileDescriptor?) {
        val player = PcmStreamPlayer(VoiceAgentAudio.SERVER_SAMPLE_RATE)
        val engine = TurnEngine()
        var live: VoiceAgentSession? = null
        var work: Job? = null
        var capture: Job? = null
        var playback: Job? = null
        val audio = PcmPlaybackQueue(VoiceAgentAudio.SERVER_SAMPLE_RATE)
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
            if (!admit(uid, request, callback)) return
            scope.launch {
                if (active != null || pending != null) {
                    reject(callback, request.sessionId, VoiceAgentContract.ERROR_SESSION_BUSY)
                    request.audioSource?.close()
                } else newRun(uid, request, callback)?.let { begin(it, request) }
            }
        }
        override fun prepareSession(request: VoiceAgentSessionRequest, callback: IVoiceAgentCallback): PendingIntent? {
            // Capture Binder identity before dispatching to the single lifecycle owner.
            val uid = Binder.getCallingUid()
            if (!admit(uid, request, callback)) return null
            return runBlocking(Dispatchers.Main.immediate) {
                if (active != null || pending != null) {
                    reject(callback, request.sessionId, VoiceAgentContract.ERROR_SESSION_BUSY)
                    request.audioSource?.close()
                    return@runBlocking null
                }
                val run = newRun(uid, request, callback) ?: return@runBlocking null
                try {
                    val nonce = Uri.Builder().scheme("speechkit-voice-start").authority(packageName)
                        .appendPath(UUID.randomUUID().toString()).build()
                    val intent = PendingIntent.getForegroundService(this@CoinstallVoiceAgentService, 0,
                        Intent(this@CoinstallVoiceAgentService, CoinstallVoiceAgentService::class.java)
                            .setAction(START_ACTION).setData(nonce),
                        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_ONE_SHOT)
                    val reservation = Prepared(run, request, nonce, intent, SystemClock.elapsedRealtime() + PREPARE_TIMEOUT_MS)
                    pending = reservation
                    reservation.expiry = scope.launch {
                        delay(PREPARE_TIMEOUT_MS)
                        if (pending === reservation) finish(run, "expired")
                    }
                    intent
                } catch (_: Throwable) {
                    notify(run) { it.onError(run.id, VoiceAgentContract.ERROR_TRANSPORT, "", true) }
                    finish(run, "server_error")
                    null
                }
            }
        }
        override fun stopSession(sessionId: String) {
            val uid = Binder.getCallingUid()
            if (!attested(uid)) return
            scope.launch {
                val run = active ?: pending?.run
                run?.takeIf { it.uid == uid && it.id == sessionId }?.let { finish(it, "client") }
            }
        }
        override fun interruptSession(sessionId: String) {
            val uid = Binder.getCallingUid()
            if (!attested(uid)) return
            scope.launch {
                active?.takeIf { it.uid == uid && it.id == sessionId }?.let {
                    stopPlayback(it); it.live?.cancelReply()
                }
            }
        }
    }
    override fun onBind(intent: Intent): IBinder? = binder.takeIf { intent.action == VoiceAgentContract.BIND_ACTION }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        val reservation = pending
        if (reservation == null || intent?.action != START_ACTION || intent.data != reservation.identity) {
            if (active == null) stopSelfResult(startId)
            return START_NOT_STICKY
        }
        pending = null
        reservation.expiry?.cancel()
        reservation.intent.cancel()
        val run = reservation.run
        active = run
        if (run.ended || SystemClock.elapsedRealtime() >= reservation.deadline || !validTicket(reservation.request)) {
            finish(run, "expired")
            return START_NOT_STICKY
        }
        try {
            // Android grants while-in-use eligibility to this visible-caller start.
            // Promote synchronously, before any socket or capture work is launched.
            startAudioNotification(microphone = reservation.request.audioSource == null)
            begin(run, reservation.request, foregroundStarted = true)
        } catch (_: Throwable) {
            notify(run) { it.onError(run.id, VoiceAgentContract.ERROR_TRANSPORT, "", true) }
            finish(run, "server_error")
        }
        return START_NOT_STICKY
    }
    override fun onUnbind(intent: Intent): Boolean {
        (active ?: pending?.run)?.let { finish(it, "client") }
        return false
    }
    override fun onDestroy() {
        (active ?: pending?.run)?.let { finish(it, "client") }
        scope.cancel(); super.onDestroy()
    }
    private fun attested(uid: Int): Boolean = Build.VERSION.SDK_INT >= 28 && identity.attested(uid)

    private fun admit(uid: Int, request: VoiceAgentSessionRequest, callback: IVoiceAgentCallback): Boolean {
        val error = when {
            !attested(uid) -> VoiceAgentContract.ERROR_CALLER_NOT_ATTESTED
            !validTicket(request) -> VoiceAgentContract.ERROR_TICKET_INVALID
            request.audioSource == null && checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED ->
                VoiceAgentContract.ERROR_MICROPHONE_PERMISSION
            else -> null
        }
        if (error == null) return true
        reject(callback, request.sessionId, error)
        request.audioSource?.close()
        return false
    }
    private fun newRun(uid: Int, request: VoiceAgentSessionRequest, callback: IVoiceAgentCallback): Running? {
        val run = Running(uid, request.sessionId, callback, request.audioSource)
        run.death = IBinder.DeathRecipient { scope.launch { finish(run, "client") } }
        try { callback.asBinder().linkToDeath(run.death, 0) } catch (_: Throwable) {
            request.audioSource?.close(); run.player.release(); return null
        }
        return run
    }
    private fun begin(run: Running, request: VoiceAgentSessionRequest, foregroundStarted: Boolean = false) {
        if (run.ended) return
        active = run
        run.work = scope.launch(start = CoroutineStart.LAZY) {
            if (run.ended) return@launch
            try {
                if (!foregroundStarted) startAudioNotification(microphone = request.audioSource == null)
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
                notify(run) { it.onState(run.id, VoiceAgentContract.STATE_CONNECTING) }
                if (run.ended) return@launch
                live.start(VoiceAgentStartFrame(locale = request.locale.takeIf { LOCALE.matches(it) }))
                if (run.ended) return@launch
                run.playback = launch {
                    try {
                        run.audio.consume { pcm ->
                            if (!run.ended) {
                                run.engine.notePlaybackFrame(pcm, VoiceAgentAudio.SERVER_SAMPLE_RATE)
                                run.player.play(pcm)
                                if (!run.ended) run.playbackBytes += pcm.size
                            }
                        }
                    } catch (cancelled: CancellationException) { throw cancelled
                    } catch (failure: PcmPlaybackException) {
                        notify(run) { it.onError(run.id, failure.code, "", true) }
                        finish(run, "playback_failed")
                    } catch (_: Exception) {
                        notify(run) { it.onError(run.id, "playback_device_failed", "", true) }
                        finish(run, "playback_failed")
                    }
                }
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
                            if (!run.audio.offer(event.pcm)) {
                                notify(run) { it.onError(run.id, VoiceAgentWsClient.OVERFLOW_CODE, "", true) }
                                finish(run, "playback_overflow")
                            }
                        }
                        VoiceAgentEvent.Interrupted -> stopPlayback(run)
                        is VoiceAgentEvent.Failure -> {
                            val code = safeVoiceAgentCode(event.code, VoiceAgentContract.ERROR_SERVER)
                            val terminal = event.fatal || code in TRANSPORT_FAILURES
                            notify(run) { it.onError(run.id, code, "", terminal) }
                            if (terminal) finish(run, if (event.fatal) voiceAgentFatalEndReason(code) else "transport_failed")
                        }
                        is VoiceAgentEvent.Closed -> finish(run, event.reason)
                        is VoiceAgentEvent.ToolCall -> Unit // The registered agent runtime owns tool execution.
                    }
                }
                finish(run, "closed")
            } catch (cancelled: CancellationException) { throw cancelled
            } catch (failure: VoiceAgentSetupException) {
                notify(run) { it.onError(run.id, failure.code, "", true) }
                finish(run, voiceAgentFatalEndReason(failure.code))
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
        pending?.takeIf { it.run === run }?.let {
            pending = null
            it.expiry?.cancel()
            it.intent.cancel()
        }
        val safeReason = safeVoiceAgentCode(reason, "closed")
        VoiceLog.i(VoiceLog.AGENT, "coinstall_end uplink_bytes=${run.uplinkBytes} playback_bytes=${run.playbackBytes}")
        run.capture?.cancel()
        runCatching { run.source?.close() }
        stopPlayback(run)
        run.audio.close()
        run.playback?.cancel()
        run.player.release()
        run.focus?.let { audioManager.abandonAudioFocusRequest(it) }
        run.live?.let { live -> scope.launch { runCatching { live.close() } } }
        run.work?.cancel()
        runCatching { run.callback.onEnded(run.id, safeReason) }
        runCatching { run.callback.asBinder().unlinkToDeath(run.death, 0) }
        if (active === run) {
            active = null
            stopForeground(STOP_FOREGROUND_REMOVE)
            if (lastStartId != 0) stopSelfResult(lastStartId)
        }
    }
    private fun stopPlayback(run: Running) {
        run.audio.clear()
        run.player.flush()
        run.engine.notePlaybackStopped()
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
        private val TRANSPORT_FAILURES = setOf(VoiceAgentWsClient.FAILURE_CODE,
            VoiceAgentWsClient.SEND_FAILURE_CODE, VoiceAgentWsClient.OVERFLOW_CODE)
        private const val MAX_SOURCE_BYTES = 16000 * 2 * 120 + 44
        private const val START_ACTION = "io.kombify.speechkit.voiceagent.v1.START"
        private const val PREPARE_TIMEOUT_MS = 15000L
    }
}
