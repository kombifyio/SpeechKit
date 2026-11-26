package io.kombify.speechkit.app.hybrid

import android.content.Context
import android.graphics.Bitmap
import android.net.Uri
import io.kombify.speechkit.BuildConfig
import io.kombify.speechkit.domain.*
import io.kombify.speechkit.hybrid.*
import io.kombify.speechkit.net.*
import java.util.Locale
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.cancelAndJoin

/** Used only by foreground activities. Explicit Firebase cloud selection is separate from endpoint permission. */
enum class HybridTextSource { FIREBASE_DEVICE, FIREBASE_CLOUD, SPEECHKIT_ENDPOINT, GEMINI }
data class HybridTextResult(val text: String, val source: HybridTextSource)

object HybridRuntime {
    suspend fun localVoiceReady(context: Context): Boolean {
        if (!BuildConfig.HYBRID_ENABLED || android.os.Build.VERSION.SDK_INT < 36) return false
        if (LocalSpeechAdapters.router(context)?.available(io.kombify.speechkit.stt.streaming.DictationSegmentOptions(language = Locale.getDefault().toLanguageTag())) != true && !android.speech.SpeechRecognizer.isOnDeviceRecognitionAvailable(context)) return false
        val store = io.kombify.speechkit.store.RoomStore(context)
        return try { LocalNoteAgent(context, store).readiness() == DeviceReadiness.AVAILABLE }
        catch (cancelled: kotlinx.coroutines.CancellationException) { throw cancelled }
        catch (_: Exception) { false }
        finally { store.close() }
    }

    fun model(context: Context, cloud: Boolean = false): HybridAi {
        if (!BuildConfig.HYBRID_ENABLED || BuildConfig.FIREBASE_APPLICATION_ID.isBlank() || BuildConfig.FIREBASE_PROJECT_ID.isBlank() || BuildConfig.FIREBASE_API_KEY.isBlank())
            throw ExecutionFailure(ExecutionFailureKind.UNAVAILABLE, "Firebase project is not configured")
        check(!cloud || (BuildConfig.FIREBASE_CLOUD_ENABLED && !StoredServerProfile.cloudSelected(context))) { "Firebase cloud is not available for this profile" }
        return HybridAi(context, FirebaseProject(
            BuildConfig.FIREBASE_APPLICATION_ID, BuildConfig.FIREBASE_PROJECT_ID,
            BuildConfig.FIREBASE_API_KEY, BuildConfig.FIREBASE_CLOUD_MODEL.ifBlank { "gemini-3.5-flash-lite" }, cloud,
            if (BuildConfig.DEBUG) AppCheckMode.DEBUG else AppCheckMode.PLAY_INTEGRITY,
        ))
    }

    suspend fun generate(context: Context, profile: ConnectionProfile, prompt: String, firebaseCloud: Boolean, policyOverride: ExecutionPolicy? = null, image: Bitmap? = null): ExecutionResult<HybridTextResult> {
        image?.let(GenerationImage::validate)
        if (profile is ConnectionProfile.Server || profile is ConnectionProfile.Byok || profile is ConnectionProfile.Firebase) StoredConnectionSelection(context).requireCurrent(profile)
        val policy = StoredExecutionPolicy(context).read(profile).restrictTo(policyOverride)
        if (profile is ConnectionProfile.Firebase || policy.mode == ExecutionMode.DEVICE_ONLY ||
            (!BuildConfig.CONNECTION_SETUP_ENABLED && firebaseCloud && BuildConfig.FIREBASE_CLOUD_ENABLED)) {
            val cloud = policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY
            if (!cloud && policy.mode == ExecutionMode.ENDPOINT_ONLY) throw ExecutionFailure(ExecutionFailureKind.POLICY_DENIED, "Cloud access is disabled")
            val request: suspend () -> HybridGeneration = {
                val firebase = if (profile is ConnectionProfile.Firebase) HybridAi(context, FirebaseProject(
                    profile.applicationId, profile.projectId, profile.apiKey, profile.cloudModel, cloud,
                    if (BuildConfig.DEBUG && profile.debugAppCheck) AppCheckMode.DEBUG else AppCheckMode.PLAY_INTEGRITY,
                )) else model(context, cloud)
                firebase.generate(prompt, if (!cloud) HybridInferenceMode.ONLY_ON_DEVICE else when (policy.mode) {
                    ExecutionMode.DEVICE_ONLY -> HybridInferenceMode.ONLY_ON_DEVICE
                    ExecutionMode.PREFER_DEVICE -> HybridInferenceMode.PREFER_ON_DEVICE
                    ExecutionMode.PREFER_ENDPOINT -> HybridInferenceMode.PREFER_IN_CLOUD
                    ExecutionMode.ENDPOINT_ONLY -> HybridInferenceMode.ONLY_IN_CLOUD
                }, image)
            }
            val answer = if (cloud) firebaseCloudCall(context, profile, request) else request()
            val local = answer.source == InferenceLocation.DEVICE
            return ExecutionResult(HybridTextResult(answer.text, if (local) HybridTextSource.FIREBASE_DEVICE else HybridTextSource.FIREBASE_CLOUD),
                if (local) ExecutionLocation.DEVICE else ExecutionLocation.ENDPOINT)
        }
        val device: suspend () -> HybridTextResult = {
            val answer = model(context).generateOnDevice(prompt, image)
            check(answer.source == InferenceLocation.DEVICE)
            HybridTextResult(answer.text, HybridTextSource.FIREBASE_DEVICE)
        }
        val remote: (suspend () -> HybridTextResult)? = if (profile is ConnectionProfile.Byok) {
            suspend { HybridTextResult(GeminiTextClient(profile, context).generate(prompt, image), HybridTextSource.GEMINI) }
        } else if (profile is ConnectionProfile.Server) {
            suspend {
                if (image != null) throw ExecutionFailure(ExecutionFailureKind.UNAVAILABLE, "SpeechKit endpoint does not support image input")
                HybridTextResult(endpointCall { SpeechKitServerApi(profile, context = context).processAssist(prompt).text }, HybridTextSource.SPEECHKIT_ENDPOINT)
            }
        } else if (!BuildConfig.CONNECTION_SETUP_ENABLED && firebaseCloud && BuildConfig.FIREBASE_CLOUD_ENABLED && !StoredServerProfile.cloudSelected(context)) {
            suspend {
                val answer = model(context, true).generateFirebaseCloud(prompt, image)
                check(answer.source == InferenceLocation.FIREBASE_CLOUD)
                HybridTextResult(answer.text, HybridTextSource.FIREBASE_CLOUD)
            }
        } else null
        return executeGeneration(policy, device, remote)
    }

    private suspend fun <T> firebaseCloudCall(context: Context, profile: ConnectionProfile, call: suspend () -> T): T = kotlinx.coroutines.coroutineScope {
        val job = checkNotNull(kotlin.coroutines.coroutineContext[kotlinx.coroutines.Job])
        val register = { NetworkUseGuard.listenIfAllowed { job.cancel(); listOf(job) }
            ?: throw ExecutionFailure(ExecutionFailureKind.POLICY_DENIED, "Cloud access is disabled") }
        val guard = if (profile is ConnectionProfile.Firebase) StoredConnectionSelection(context).withCurrent(profile, register) else register()
        try {
            val answer = call()
            kotlinx.coroutines.currentCoroutineContext().ensureActive()
            if (profile is ConnectionProfile.Firebase) StoredConnectionSelection(context).requireCurrent(profile)
            answer
        } finally { guard.close() }
    }

    /** Every host passes its selected language; unspecified input retains SpeechKit's multi-language semantics. */
    suspend fun transcribe(context: Context, profile: ConnectionProfile, uri: Uri, policyOverride: ExecutionPolicy? = null,
        options: io.kombify.speechkit.stt.streaming.DictationSegmentOptions = io.kombify.speechkit.stt.streaming.DictationSegmentOptions(),
        mediaType: String? = null,
    ): ExecutionResult<String> {
        if (profile is ConnectionProfile.Server || profile is ConnectionProfile.Byok || profile is ConnectionProfile.Firebase) StoredConnectionSelection(context).requireCurrent(profile)
        val selection = StoredConnectionSelection(context)
        val revision = selection.revision()
        val policy = StoredExecutionPolicy(context).read(profile).restrictTo(policyOverride)
        val audioType = (mediaType ?: context.contentResolver.getType(uri) ?: "audio/ogg")
            .let { if (it == "application/ogg") "audio/ogg" else it }
        require(audioType.startsWith("audio/"))
        val bytes = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
            context.contentResolver.openInputStream(uri)?.use { stream ->
                val output = java.io.ByteArrayOutputStream()
                val chunk = ByteArray(8192)
                var emptyReads = 0
                while (true) {
                    kotlinx.coroutines.currentCoroutineContext().ensureActive()
                    val count = stream.read(chunk)
                    if (count < 0) break
                    if (count == 0) {
                        require(++emptyReads < 100) { "Audio stream did not progress" }
                        continue
                    }
                    emptyReads = 0
                    require(output.size() + count <= MAX_AUDIO_BYTES) { "Audio file is too large" }
                    output.write(chunk, 0, count)
                }
                require(output.size() > 0) { "Audio file is empty" }
                output.toByteArray()
            } ?: error("Cannot read audio")
        }
        try {
            if (audioType == "audio/ogg") {
                require(bytes.size >= 35 && bytes.copyOfRange(0, 4).contentEquals("OggS".toByteArray())) { "Invalid Ogg audio" }
                val packetStart = 27 + (bytes[26].toInt() and 0xff)
                require(packetStart + 8 <= bytes.size) { "Invalid Ogg audio" }
                val header = bytes.copyOfRange(packetStart, packetStart + 8)
                require(header.contentEquals("OpusHead".toByteArray()) ||
                    header.copyOfRange(0, 7).contentEquals(byteArrayOf(1) + "vorbis".toByteArray()) ||
                    header.contentEquals("Speex   ".toByteArray()) ||
                    header.copyOfRange(0, 5).contentEquals(byteArrayOf(0x7f) + "FLAC".toByteArray())) { "Unsupported Ogg audio" }
            }
            val router = LocalSpeechAdapters.router(context) ?: LocalSpeechRouter(context, listOf(
                io.kombify.speechkit.stt.NativeLocalSpeechAdapter(context), MlKitLocalSpeechAdapter(context),
            ))
            var localResult: io.kombify.speechkit.stt.LocalSpeechResult? = null
            val local: suspend () -> String = {
                router.transcribe(io.kombify.speechkit.stt.LocalSpeechAudio.Encoded(bytes), options)
                    .also { localResult = it }.text
            }
            val remote: (suspend () -> String)? = if (profile is ConnectionProfile.Server || profile is ConnectionProfile.Firebase) ({
                if (profile is ConnectionProfile.Firebase) FirebaseAudioTranscriber(profile, context).transcribe(bytes,
                    audioType, options.language)
                else endpointCall { SpeechKitServerApi(profile as ConnectionProfile.Server, context = context).transcribe(bytes,
                    filename = "audio", mediaType = audioType, language = options.language).text }
            }) else null
            val result = executeGeneration(policy, local, remote)
            kotlinx.coroutines.currentCoroutineContext().ensureActive()
            if (selection.revision() != revision) throw kotlinx.coroutines.CancellationException("Connection changed")
            return result.copy(adapterId = when (result.location) {
                ExecutionLocation.DEVICE -> localResult?.engineId
                ExecutionLocation.ENDPOINT -> if (profile is ConnectionProfile.Firebase) "firebase_cloud" else "speechkit_endpoint"
            })
        } finally { bytes.fill(0) }
    }
    private suspend fun <T> endpointCall(block: suspend () -> T): T = try {
        block()
    } catch (failure: SpeechKitApiException) {
        if (failure.denial == null && failure.httpStatus >= 500) throw ExecutionFailure(ExecutionFailureKind.TRANSIENT, "Endpoint unavailable", failure)
        throw failure
    } catch (cancelled: kotlinx.coroutines.CancellationException) {
        throw cancelled
    } catch (failure: java.io.IOException) {
        throw ExecutionFailure(ExecutionFailureKind.TRANSIENT, "Endpoint unavailable", failure)
    }

    fun scope(profile: ConnectionProfile): String = when (profile) {
        is ConnectionProfile.Server -> profile.storageScope()
        is ConnectionProfile.Byok -> "gemini:${profile.credentialId}"
        is ConnectionProfile.Firebase -> "firebase:${profile.projectId}"
        else -> "device"
    }

    private val activeLock = Any()
    private val activeJobs = mutableSetOf<kotlinx.coroutines.Job>()
    private var clearing = false
    fun track(context: Context, profile: ConnectionProfile, job: kotlinx.coroutines.Job) {
        if ((profile is ConnectionProfile.Server || profile is ConnectionProfile.Byok || profile is ConnectionProfile.Firebase) && !StoredConnectionSelection(context).isCurrent(profile)) { job.cancel(); return }
        val policy = StoredExecutionPolicy(context).read(profile)
        if (policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY) {
            val listener = NetworkUseGuard.listenIfAllowed { job.cancel(); listOf(job) }
            if (listener == null) { job.cancel(); return }
            job.invokeOnCompletion { listener.close() }
        }
        if (profile is ConnectionProfile.Server && !StoredServerProfile.registerActiveJob(context, profile, job)) { job.cancel(); return }
        synchronized(activeLock) {
            if (clearing) { job.cancel(); return }
            activeJobs.add(job)
        }
        job.invokeOnCompletion { synchronized(activeLock) { activeJobs.remove(job) } }
    }
    suspend fun cancelActive() {
        val jobs = synchronized(activeLock) { clearing = true; activeJobs.toList() }
        try { jobs.forEach { it.cancel() }; jobs.forEach { it.join() } }
        finally { synchronized(activeLock) { clearing = false } }
    }

    const val MAX_AUDIO_BYTES = 25 * 1024 * 1024
}
