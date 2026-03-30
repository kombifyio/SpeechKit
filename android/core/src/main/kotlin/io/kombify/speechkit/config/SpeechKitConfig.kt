package io.kombify.speechkit.config

/**
 * SpeechKit configuration model.
 *
 * Mirrors: internal/config/config.go Config struct.
 * Android uses DataStore instead of TOML files.
 */
data class SpeechKitConfig(
    val general: GeneralConfig = GeneralConfig(),
    val local: LocalConfig = LocalConfig(),
    val huggingface: HuggingFaceConfig = HuggingFaceConfig(),
    val routing: RoutingConfig = RoutingConfig(),
)

data class GeneralConfig(
    val language: String = "de",
    val activeMode: String = "dictate",
)

data class LocalConfig(
    val enabled: Boolean = false,
    val model: String = "ggml-small.bin",
)

data class HuggingFaceConfig(
    val enabled: Boolean = true,
    val model: String = "openai/whisper-large-v3",
    val token: String? = null,
)

data class RoutingConfig(
    val strategy: String = "cloud-only",
    val preferLocalUnderSeconds: Double = 10.0,
    val parallelCloud: Boolean = false,
)
