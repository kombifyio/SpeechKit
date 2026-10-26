package io.kombify.speechkit.stt.parakeet

/** One file of a [ParakeetModel], pinned by its SHA-256. */
data class ParakeetModelFile(
    val name: String,
    val url: String,
    val sha256: String,
    val sizeBytes: Long,
)

/**
 * A compressed archive among a model's files that the store unpacks into the model directory: [fileName] names the
 * downloaded archive, [members] the files taken from it, each checked against its SHA-256 before the model is
 * installed. The archive itself is deleted afterwards.
 */
data class ModelArchive(val fileName: String, val members: List<ParakeetModelFile>)

/** Which engine runs a model. */
enum class OnDeviceEngineKind {
    /** Parakeet's sherpa-onnx int8 export on ONNX Runtime ([ParakeetRecognizer]). */
    ONNX,

    /** Phonon-2.5's five-value container on SpeechKit's own engine (`io.kombify.speechkit.stt.phonon`). */
    FIVE_VALUE,
}

/**
 * A downloadable on-device model: a directory of [files], all pinned to an
 * exact Hugging Face revision and SHA-256. The same weights the desktop
 * download catalog offers (`app/host/downloads/catalog_artifacts.go`), so a
 * phone and a desktop transcribe identically.
 */
data class ParakeetModel(
    /** Catalog variant id, shared with the desktop catalog. */
    val id: String,
    val name: String,
    /** Directory name the model installs into. */
    val dirName: String,
    val files: List<ParakeetModelFile>,
    val recommended: Boolean = false,
    val engine: OnDeviceEngineKind = OnDeviceEngineKind.ONNX,
    /** Set when [files] carry an archive to unpack. */
    val archive: ModelArchive? = null,
    /** Total device memory below which the model is not offered. */
    val minTotalMemoryBytes: Long = ParakeetOnDevice.MIN_TOTAL_MEMORY_BYTES,
    /** True for a model that transcribes English only. */
    val englishOnly: Boolean = false,
) {
    /** Bytes downloaded. */
    val sizeBytes: Long get() = files.sumOf { it.sizeBytes }

    /** The files an installed model directory holds: [files] without the archive, plus what it unpacks. */
    val installedFiles: List<ParakeetModelFile>
        get() = archive?.let { a -> files.filter { it.name != a.fileName } + a.members } ?: files
}

/** The models SpeechKit offers on device. */
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

    private const val PHONON_REVISION =
        "https://huggingface.co/FermionResearch/Phonon-2.5/resolve/2673618ad8636c8dfd20fe57e94d1f67f75bdeaa"
    private const val PHONON_EN_REVISION =
        "https://huggingface.co/FermionResearch/Phonon-2.5-English/resolve/cea043a0a125d6535d473645ad5fa90847a6b001"
    private const val CC_BY_SHA256 = "9ba9550ad48438d0836ddab3da480b3b69ffa0aac7b7878b5a0039e7ab429411"

    /** Total memory a Phonon model needs: it runs in about 350 MB, so phones sold with 3 GB qualify. */
    const val PHONON_MIN_TOTAL_MEMORY_BYTES: Long = 2L * 1024 * 1024 * 1024 + 512L * 1024 * 1024

    /**
     * Fermion Research's Phonon-2.5 (CC-BY-4.0), a five-value Parakeet TDT 0.6B v3 student for the same 25
     * languages. Downloaded as Fermion's own archive (163 MB) and unpacked on the phone; the files are the ones the
     * desktop catalog serves (`parakeet.phonon-2.5`). Recommended: a quarter of Parakeet Ultra's download, a third
     * of its memory, faster, and the same accuracy within a word per thousand.
     */
    val PHONON = ParakeetModel(
        id = "parakeet.phonon-2.5",
        name = "Phonon-2.5",
        dirName = "phonon-2.5",
        recommended = true,
        engine = OnDeviceEngineKind.FIVE_VALUE,
        minTotalMemoryBytes = PHONON_MIN_TOTAL_MEMORY_BYTES,
        files = listOf(
            ParakeetModelFile("phonon-2.5.bps.tar.zst", "$PHONON_REVISION/phonon-2.5.bps.tar.zst", "1faa04053a472cabab3bc31d07f30c23f22906f71044ca10eb1f0de36d1f19a9", 162_587_031),
            ParakeetModelFile("NOTICE", "$PHONON_REVISION/NOTICE", "dd4993badd45eb336280a0ab5b4ecb3ed06260d189a30fcae8b9e89a587c4de8", 2_664),
            ParakeetModelFile("LICENSE", "$PHONON_REVISION/LICENSE-WEIGHTS-CC-BY-4.0.txt", CC_BY_SHA256, 18_657),
        ),
        archive = ModelArchive(
            fileName = "phonon-2.5.bps.tar.zst",
            members = listOf(
                ParakeetModelFile("model.fermion", "", "17cb62d08e751a41819eb779db089aa7ac4eb2bab433ba5c2991d8cbd3cfefb9", 177_593_644),
                ParakeetModelFile("config.json", "", "b8ee2310784b2ffb62200bc9d2301cdc0576141ee9c53f7a41a5a2d181910cc9", 277_495),
                ParakeetModelFile("packed_manifest.json", "", "2c98e5d0e1349fa146f21cdc168d681b186711c78abcfac47af16c25649f5c89", 503),
            ),
        ),
    )

    /** Fermion Research's Phonon-2.5 English (CC-BY-4.0), English only. */
    val PHONON_EN = ParakeetModel(
        id = "parakeet.phonon-2.5-en",
        name = "Phonon-2.5 English",
        dirName = PhononModelFiles.ENGLISH_DIR,
        engine = OnDeviceEngineKind.FIVE_VALUE,
        minTotalMemoryBytes = PHONON_MIN_TOTAL_MEMORY_BYTES,
        englishOnly = true,
        files = listOf(
            ParakeetModelFile("phonon-2.5-en.bps.tar.zst", "$PHONON_EN_REVISION/phonon-2.5-en.bps.tar.zst", "6bd8119ac5a637e5ece0666f5c1e43dbcd99e1c36b3d0ef3ec5f9b58260a9dd4", 163_693_770),
            ParakeetModelFile("NOTICE", "$PHONON_EN_REVISION/NOTICE", "76cdb0a2b0493d1d551d164314361839605fda714dd96e3a83735d6975673c45", 2_657),
            ParakeetModelFile("LICENSE", "$PHONON_EN_REVISION/LICENSE-WEIGHTS-CC-BY-4.0.txt", CC_BY_SHA256, 18_657),
        ),
        archive = ModelArchive(
            fileName = "phonon-2.5-en.bps.tar.zst",
            members = listOf(
                ParakeetModelFile("model.fermion", "", "c57119c0f04756932acc1d60d90506346e3708d69f00e654084acc7fe507fe27", 177_479_389),
                ParakeetModelFile("config.json", "", "9e5dfcc4e49a6c169d53540f07f4115e591007759b835132c849166b235dac95", 277_503),
                ParakeetModelFile("packed_manifest.json", "", "9584d9ad34be1fd5fa1347310fc8c37cd7ee185e10036b2b3615c9f989759ae0", 507),
            ),
        ),
    )

    val all: List<ParakeetModel> = listOf(PHONON, ULTRA, PHONON_EN, V3)

    fun byId(id: String): ParakeetModel? = all.firstOrNull { it.id == id }
}
