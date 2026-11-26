package io.kombify.speechkit.hybrid

import android.content.Context
import android.graphics.Bitmap
import com.google.firebase.ai.type.content
import io.kombify.speechkit.net.GenerationImage
import com.google.firebase.ai.DownloadStatus
import com.google.firebase.ai.InferenceMode
import com.google.firebase.ai.InferenceSource
import com.google.firebase.ai.OnDeviceConfig
import com.google.firebase.ai.OnDeviceModelOption
import com.google.firebase.ai.OnDeviceModelStatus
import com.google.firebase.ai.type.PublicPreviewAPI
import io.kombify.speechkit.net.FirebaseAiProject
import io.kombify.speechkit.net.FirebaseAiServices
import kotlinx.coroutines.flow.collect

/** Public Firebase project identifiers. Cloud use additionally requires explicit permission. */
data class FirebaseProject(
    val applicationId: String,
    val projectId: String,
    val apiKey: String,
    val cloudModel: String,
    val allowCloud: Boolean = false,
    val appCheck: AppCheckMode = AppCheckMode.PLAY_INTEGRITY,
)

enum class AppCheckMode { PLAY_INTEGRITY, DEBUG }
enum class ModelCategory { PREVIEW, PREVIEW_FAST }
enum class DeviceReadiness { UNAVAILABLE, DOWNLOADABLE, DOWNLOADING, AVAILABLE, UNKNOWN }
enum class InferenceLocation { DEVICE, FIREBASE_CLOUD }
enum class HybridInferenceMode { ONLY_ON_DEVICE, PREFER_ON_DEVICE, PREFER_IN_CLOUD, ONLY_IN_CLOUD }
data class HybridGeneration(val text: String, val source: InferenceLocation)
sealed interface DownloadProgress {
    data class Started(val bytes: Long) : DownloadProgress
    data class Progress(val bytes: Long) : DownloadProgress
    data object Completed : DownloadProgress
}

/** No automatic cloud fallback: the caller enforces destination policy. */
@OptIn(PublicPreviewAPI::class)
class HybridAi(context: Context, project: FirebaseProject, category: ModelCategory = ModelCategory.PREVIEW) {
    private val config = project
    private val ai = FirebaseAiServices.get(context, FirebaseAiProject(project.applicationId, project.projectId, project.apiKey, project.allowCloud, project.appCheck == AppCheckMode.DEBUG))
    private val modelOption = when (category) { ModelCategory.PREVIEW -> OnDeviceModelOption.PREVIEW; ModelCategory.PREVIEW_FAST -> OnDeviceModelOption.PREVIEW_FAST }
    private val deviceModel = ai.generativeModel(
        modelName = project.cloudModel,
        onDeviceConfig = OnDeviceConfig(
            mode = InferenceMode.ONLY_ON_DEVICE,
            modelOption = modelOption,
        ),
    )

    init {
        require(project.applicationId.isNotBlank() && project.projectId.isNotBlank() && project.apiKey.isNotBlank())
        require(project.cloudModel.isNotBlank())

    }

    suspend fun readiness(): DeviceReadiness = classifySdkFailure { when (deviceModel.onDeviceExtension?.checkStatus()) {
        OnDeviceModelStatus.UNAVAILABLE -> DeviceReadiness.UNAVAILABLE
        OnDeviceModelStatus.DOWNLOADABLE -> DeviceReadiness.DOWNLOADABLE
        OnDeviceModelStatus.DOWNLOADING -> DeviceReadiness.DOWNLOADING
        OnDeviceModelStatus.AVAILABLE -> DeviceReadiness.AVAILABLE
        else -> DeviceReadiness.UNKNOWN
    } }

    /** SDK-reported model identity, when the selected on-device model supports it. */
    suspend fun deviceModelName(): String = classifySdkFailure {
        checkNotNull(deviceModel.onDeviceExtension) { "On-device SDK unavailable" }.getModelName()
    }

    suspend fun download(onProgress: (DownloadProgress) -> Unit) = classifySdkFailure {
        val extension = checkNotNull(deviceModel.onDeviceExtension) { "On-device SDK unavailable" }
        extension.download().collect { status ->
            when (status) {
                is DownloadStatus.DownloadStarted -> onProgress(DownloadProgress.Started(status.bytesToDownload))
                is DownloadStatus.DownloadInProgress -> onProgress(DownloadProgress.Progress(status.totalBytesDownloaded))
                is DownloadStatus.DownloadCompleted -> onProgress(DownloadProgress.Completed)
                is DownloadStatus.DownloadFailed -> throw status.exception
                else -> error("Unknown on-device download state")
            }
        }
    }

    suspend fun generateOnDevice(prompt: String, image: Bitmap? = null): HybridGeneration = generate(prompt, HybridInferenceMode.ONLY_ON_DEVICE, image)

    suspend fun generateFirebaseCloud(prompt: String, image: Bitmap? = null): HybridGeneration = generate(prompt, HybridInferenceMode.ONLY_IN_CLOUD, image)

    suspend fun generate(prompt: String, mode: HybridInferenceMode, image: Bitmap? = null): HybridGeneration {
        image?.let(GenerationImage::validate)
        require(prompt.isNotBlank() && prompt.length <= MAX_PROMPT_CHARS)
        check(mode == HybridInferenceMode.ONLY_ON_DEVICE || config.allowCloud) { "Firebase cloud is not enabled for this project" }
        val sdkMode = when (mode) {
            HybridInferenceMode.ONLY_ON_DEVICE -> InferenceMode.ONLY_ON_DEVICE
            HybridInferenceMode.PREFER_ON_DEVICE -> InferenceMode.PREFER_ON_DEVICE
            HybridInferenceMode.PREFER_IN_CLOUD -> InferenceMode.PREFER_IN_CLOUD
            HybridInferenceMode.ONLY_IN_CLOUD -> InferenceMode.ONLY_IN_CLOUD
        }
        val model = if (mode == HybridInferenceMode.ONLY_ON_DEVICE) deviceModel else
            ai.generativeModel(config.cloudModel, onDeviceConfig = OnDeviceConfig(mode = sdkMode, modelOption = modelOption))
        val response = classifySdkFailure {
            if (image == null) model.generateContent(prompt)
            else model.generateContent(content { image(image); text(prompt) })
        }
        val location = when (response.inferenceSource) {
            InferenceSource.ON_DEVICE -> InferenceLocation.DEVICE
            InferenceSource.IN_CLOUD -> InferenceLocation.FIREBASE_CLOUD
            else -> error("Unknown Firebase inference source")
        }
        check(mode != HybridInferenceMode.ONLY_ON_DEVICE || location == InferenceLocation.DEVICE)
        check(mode != HybridInferenceMode.ONLY_IN_CLOUD || location == InferenceLocation.FIREBASE_CLOUD)
        return HybridGeneration(checkNotNull(response.text) { "Model returned no text" }, location)
    }

    private companion object { const val MAX_PROMPT_CHARS = 16000 }
}
