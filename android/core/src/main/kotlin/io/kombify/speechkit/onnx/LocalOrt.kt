package io.kombify.speechkit.onnx

import ai.onnxruntime.OrtEnvironment
import ai.onnxruntime.OrtException
import io.kombify.speechkit.log.VoiceLog

/**
 * The process's ONNX Runtime environment for SpeechKit's on-device models, with ORT's telemetry events turned off.
 * The guard is the manifest, which removes the provider that starts ORT's telemetry client; this is defence in
 * depth for a host that merges the provider back: there the client still starts at process launch, and only the
 * events ORT emits after the environment exists are turned off. Callers borrow the environment and never close it.
 */
internal object LocalOrt {
    private val environment: OrtEnvironment by lazy {
        OrtEnvironment.getEnvironment().also { env ->
            try {
                env.setTelemetry(false)
            } catch (e: OrtException) {
                VoiceLog.w(VoiceLog.DICTATION, "onnxruntime telemetry could not be turned off", e)
            }
        }
    }

    fun environment(): OrtEnvironment = environment
}
