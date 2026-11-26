package io.kombify.speechkit.net

import android.content.Context
import com.google.firebase.ai.type.ActivityDetectionConfig
import com.google.firebase.ai.type.AudioTranscriptionConfig
import com.google.firebase.ai.type.InlineData
import com.google.firebase.ai.type.InlineDataPart
import com.google.firebase.ai.type.LiveGenerationConfig
import com.google.firebase.ai.type.LiveServerContent
import com.google.firebase.ai.type.LiveSession
import com.google.firebase.ai.type.RealtimeInputConfig
import com.google.firebase.ai.type.ResponseModality
import com.google.firebase.ai.type.InvalidAPIKeyException
import com.google.firebase.ai.type.PermissionMissingException
import com.google.firebase.ai.type.QuotaExceededException
import com.google.firebase.ai.type.PromptBlockedException
import com.google.firebase.ai.type.ContentBlockedException
import com.google.firebase.ai.type.RequestTimeoutException
import com.google.firebase.ai.type.SpeechConfig
import com.google.firebase.ai.type.Voice
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.ExecutionPolicy
import io.kombify.speechkit.domain.ExecutionMode
import io.kombify.speechkit.domain.ExecutionFailure
import io.kombify.speechkit.domain.ExecutionFailureKind
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.Closeable

/** Firebase AI Logic transport; media capture and playback remain owned by the host. */
@OptIn(com.google.firebase.ai.type.PublicPreviewAPI::class)
class FirebaseVoiceAgentDriver(
    private val profile: ConnectionProfile.Firebase,
    private val context: Context,
    private val localHandoff: (() -> Unit)? = null,
    private val localReady: suspend () -> Boolean = { false },
    private val policyOverride: ExecutionPolicy? = null,
) : VoiceAgentSessionDriver {
    private val stateValue = MutableStateFlow(VoiceAgentUiState())
    override val state: StateFlow<VoiceAgentUiState> = stateValue.asStateFlow()
    private val queue = VoiceAgentEventQueue()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var listener: Closeable? = null
    private var job: Job? = null
    @Volatile private var session: LiveSession? = null
    private var holding = false
    private var input = ""
    private var output = ""

    private fun effectivePolicy(): ExecutionPolicy {
        val saved = StoredExecutionPolicy(context).read(profile)
        val requested = policyOverride ?: return saved
        require(saved.mode != ExecutionMode.DEVICE_ONLY || requested.mode == ExecutionMode.DEVICE_ONLY) { "A gesture cannot enable a remote destination" }
        require(saved.mode != ExecutionMode.ENDPOINT_ONLY || requested.mode == ExecutionMode.ENDPOINT_ONLY) { "A gesture cannot enable a local destination" }
        return requested.copy(allowNetwork = saved.allowNetwork && requested.allowNetwork)
    }

    private fun requireCurrent() {
        StoredConnectionSelection(context).requireCurrent(profile)
        val policy = effectivePolicy()
        check(policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY) { "Network use is disabled" }
        NetworkUseGuard.checkAllowed()
    }

    override suspend fun start(options: VoiceAgentStartFrame): Flow<VoiceAgentEvent> {
        val policy = effectivePolicy()
        if (policy.mode == ExecutionMode.DEVICE_ONLY || !policy.allowNetwork) {
            localHandoff?.invoke(); throw localVoiceHandoffFailure()
        }
        if (policy.mode == ExecutionMode.PREFER_DEVICE && localHandoff != null && localReady()) {
            localHandoff.invoke(); throw localVoiceHandoffFailure()
        }
        requireCurrent()
        check(options.sequenceId == null && options.personaId == null && options.roleId == null) { "Managed agent options are unavailable for Firebase Live" }
        check(job == null) { "Voice session already started" }
        stateValue.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Connecting)
        val ready = CompletableDeferred<Unit>()
        val work = scope.launch(start = CoroutineStart.LAZY) {
            try {
                val config = LiveGenerationConfig.Builder()
                    .setResponseModality(ResponseModality.AUDIO)
                    .setSpeechConfig(SpeechConfig(Voice(profile.voice)))
                    .setInputAudioTranscription(AudioTranscriptionConfig())
                    .setOutputAudioTranscription(AudioTranscriptionConfig())
                    .setRealtimeInputConfig(RealtimeInputConfig.builder()
                        .setAutomaticActivityDetection(ActivityDetectionConfig.disabled())
                        .setActivityHandling(RealtimeInputConfig.ActivityHandling.INTERRUPT).build()).build()
                val ai = FirebaseAiServices.get(context, FirebaseAiProject(profile.applicationId, profile.projectId, profile.apiKey, true, profile.debugAppCheck))
                val live = ai.liveModel(profile.liveModel, config).connect()
                session = live
                requireCurrent()
                ready.complete(Unit)
                queue.offer(VoiceAgentEvent.State(VoiceAgentStates.LISTENING, "session_ready"))
                live.receive().collect { message ->
                    if (message is LiveServerContent) {
                        if (message.interrupted) {
                            output = ""
                            queue.offer(VoiceAgentEvent.Interrupted)
                            queue.offer(VoiceAgentEvent.State(VoiceAgentStates.LISTENING))
                        }
                        message.inputTranscription?.text?.let { input += it; queue.offer(VoiceAgentEvent.Transcript(true, input, false)) }
                        message.outputTranscription?.text?.let { output += it; queue.offer(VoiceAgentEvent.Transcript(false, output, false)) }
                        message.content?.parts?.filterIsInstance<InlineDataPart>()?.forEach { part ->
                            require(part.mimeType.startsWith("audio/pcm", ignoreCase = true)) { "Unsupported Firebase audio format" }
                            queue.offer(VoiceAgentEvent.State(VoiceAgentStates.SPEAKING))
                            if (!queue.offer(VoiceAgentEvent.Audio(part.inlineData))) throw IllegalStateException("Voice stream capacity exceeded")
                        }
                        if (message.turnComplete) {
                            if (input.isNotEmpty()) queue.offer(VoiceAgentEvent.Transcript(true, input, true))
                            if (output.isNotEmpty()) queue.offer(VoiceAgentEvent.Transcript(false, output, true))
                            input = ""; output = ""
                            queue.offer(VoiceAgentEvent.State(VoiceAgentStates.LISTENING))
                        }
                    }
                }
                if (!ready.isCompleted) ready.completeExceptionally(VoiceAgentSetupException("firebase_live_closed"))
                queue.finish(VoiceAgentEndReasons.CLIENT)
            } catch (cancelled: CancellationException) {
                ready.cancel(cancelled); throw cancelled
            } catch (failure: Throwable) {
                val causes = generateSequence(failure as Throwable?) { it.cause }.take(12).toList()
                val kind = when {
                    causes.any { it is InvalidAPIKeyException || it is PermissionMissingException || it is SecurityException } -> ExecutionFailureKind.AUTHORIZATION
                    causes.any { it is PromptBlockedException || it is ContentBlockedException } -> ExecutionFailureKind.SAFETY
                    causes.any { it is QuotaExceededException } -> ExecutionFailureKind.POLICY_DENIED
                    causes.any { it is RequestTimeoutException || it is java.io.IOException } -> ExecutionFailureKind.TRANSIENT
                    else -> ExecutionFailureKind.OTHER
                }
                ready.completeExceptionally(ExecutionFailure(kind, "Firebase Live setup failed (${kind.name.lowercase()})", failure))
                queue.finish(VoiceAgentEndReasons.ERROR, VoiceAgentEvent.Failure("firebase_live_failed", "Firebase voice connection failed", fatal = true))
            } finally {
                withContext(NonCancellable) { runCatching { session?.close() }; session = null }
            }
        }
        val registration = StoredConnectionSelection(context).withCurrent(profile) {
            NetworkUseGuard.listenIfAllowed { ready.cancel(); work.cancel(); listOf(work) }
        } ?: throw IllegalStateException("Network destination is not permitted")
        listener = registration; job = work; work.start()
        try {
            if (withTimeoutOrNull(8_000) { ready.await(); true } == null)
                throw ExecutionFailure(ExecutionFailureKind.TRANSIENT, "Firebase Live setup timed out")
        } catch (failure: Throwable) {
            withContext(NonCancellable) { work.cancelAndJoin(); registration.close(); listener = null; job = null }
            if (failure is ExecutionFailure && failure.kind in setOf(ExecutionFailureKind.TRANSIENT, ExecutionFailureKind.UNAVAILABLE) &&
                policy.mode == ExecutionMode.PREFER_ENDPOINT && localHandoff != null && localReady()) {
                localHandoff.invoke(); throw localVoiceHandoffFailure()
            }
            throw failure
        }
        return queue.events
    }
    override fun accept(event: VoiceAgentEvent) { stateValue.value = reduceVoiceAgentState(stateValue.value, event) }
    override suspend fun sendAudio(pcm: ByteArray) {
        requireCurrent(); require(pcm.size <= 64_000 && pcm.size % 2 == 0)
        val live = checkNotNull(session)
        if (!holding) { live.sendStartActivityRealtime(); holding = true }
        live.sendAudioRealtime(InlineData(pcm, "audio/pcm;rate=16000"))
    }
    override suspend fun endTurn() {
        requireCurrent(); if (holding) { holding = false; checkNotNull(session).sendStopActivityRealtime(); queue.offer(VoiceAgentEvent.State(VoiceAgentStates.PROCESSING)) }
    }
    override suspend fun sendText(text: String) { requireCurrent(); require(text.length <= 16_384); checkNotNull(session).send(text) }
    override suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>) { throw UnsupportedOperationException("Tools are unavailable for Firebase Live") }
    override suspend fun advanceStep(reason: String?) { throw UnsupportedOperationException("Sequences are unavailable for Firebase Live") }
    override suspend fun cancelReply() { requireCurrent(); if (!holding && queue.interrupt()) { checkNotNull(session).sendStartActivityRealtime(); holding = true } }
    override suspend fun stop() {
        withContext(NonCancellable) { job?.cancelAndJoin(); job = null; listener?.close(); listener = null; scope.cancel() }
        queue.finish(VoiceAgentEndReasons.CLIENT)
        stateValue.value = stateValue.value.copy(phase = VoiceAgentUiState.Phase.Ended)
    }
}

/** A Firebase Live handshake only; no microphone capture or generated turn. */
suspend fun testFirebaseLiveConnection(profile: ConnectionProfile.Firebase, context: Context) {
    val driver = FirebaseVoiceAgentDriver(profile, context)
    try { driver.start() } finally { driver.stop() }
}
