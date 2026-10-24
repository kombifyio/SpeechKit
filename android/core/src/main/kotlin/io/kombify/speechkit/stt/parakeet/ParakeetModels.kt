package io.kombify.speechkit.stt.parakeet

/** One file of a [ParakeetModel], pinned by its SHA-256. */
data class ParakeetModelFile(
    val name: String,
    val url: String,
    val sha256: String,
    val sizeBytes: Long,
)

/**
 * A downloadable Parakeet model: a directory of [files], all pinned to an
 * exact Hugging Face revision and SHA-256. The same artifacts the desktop
 * download catalog offers (`app/host/downloads/catalog_artifacts.go`), so a
 * phone and a desktop transcribe with identical weights.
 */
data class ParakeetModel(
    /** Catalog variant id, shared with the desktop catalog. */
    val id: String,
    val name: String,
    /** Directory name the model installs into. */
    val dirName: String,
    val files: List<ParakeetModelFile>,
    val recommended: Boolean = false,
) {
    val sizeBytes: Long get() = files.sumOf { it.sizeBytes }
}

/** The Parakeet models SpeechKit offers on device. */
object ParakeetModels {
    private const val ULTRA_REVISION =
        "https://huggingface.co/tpmaerk/parakeet-ultra-sherpa-onnx-int8/resolve/b86784a3a5356cbd126f710b61cd90c5fe5898f1"
    private const val V3_REVISION =
        "https://huggingface.co/csukuangfj/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8/resolve/2bda32ec70b097a55adaa07d9a7173915b43cc78"
    private const val TOKENS_SHA256 = "d58544679ea4bc6ac563d1f545eb7d474bd6cfa467f0a6e2c1dc1c7d37e3c35d"

    /** Moondream's post-trained Parakeet TDT 0.6B v3 (CC-BY-4.0); the recommended model. */
    val ULTRA = ParakeetModel(
        id = "parakeet.ultra-int8",
        name = "Parakeet Ultra",
        dirName = "parakeet-ultra-int8",
        recommended = true,
        files = listOf(
            ParakeetModelFile("encoder.int8.onnx", "$ULTRA_REVISION/encoder.int8.onnx", "f4d06712d32431b1718eceba2153c314ac4425e8fe708a3534f5e56c5ad53697", 652_282_347),
            ParakeetModelFile("decoder.int8.onnx", "$ULTRA_REVISION/decoder.int8.onnx", "94373c6405c8149d1c9d1e9862514e0e3e88dab1c7de18fac9d5df9c6ba5efdd", 11_845_275),
            ParakeetModelFile("joiner.int8.onnx", "$ULTRA_REVISION/joiner.int8.onnx", "8a71aaccfdba3d451775507a889afd2257d2e754463fd81095a8c8f290f924c2", 6_355_277),
            ParakeetModelFile("tokens.txt", "$ULTRA_REVISION/tokens.txt", TOKENS_SHA256, 93_939),
        ),
    )

    /** NVIDIA's original Parakeet TDT 0.6B v3 (CC-BY-4.0) as exported by sherpa-onnx. */
    val V3 = ParakeetModel(
        id = "parakeet.tdt-0.6b-v3-int8",
        name = "Parakeet TDT 0.6B v3",
        dirName = "parakeet-tdt-0.6b-v3-int8",
        files = listOf(
            ParakeetModelFile("encoder.int8.onnx", "$V3_REVISION/encoder.int8.onnx", "acfc2b4456377e15d04f0243af540b7fe7c992f8d898d751cf134c3a55fd2247", 652_184_281),
            ParakeetModelFile("decoder.int8.onnx", "$V3_REVISION/decoder.int8.onnx", "179e50c43d1a9de79c8a24149a2f9bac6eb5981823f2a2ed88d655b24248db4e", 11_845_275),
            ParakeetModelFile("joiner.int8.onnx", "$V3_REVISION/joiner.int8.onnx", "3164c13fc2821009440d20fcb5fdc78bff28b4db2f8d0f0b329101719c0948b3", 6_355_277),
            ParakeetModelFile("tokens.txt", "$V3_REVISION/tokens.txt", TOKENS_SHA256, 93_939),
        ),
    )

    val all: List<ParakeetModel> = listOf(ULTRA, V3)

    fun byId(id: String): ParakeetModel? = all.firstOrNull { it.id == id }
}
