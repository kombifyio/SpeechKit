package io.kombify.speechkit.net

import android.graphics.Bitmap

/** Bounds an in-memory image before it reaches any inference adapter. */
object GenerationImage {
    const val MAX_EDGE = 4096
    const val MAX_BYTES = 16 * 1024 * 1024

    fun validate(image: Bitmap) {
        require(!image.isRecycled) { "Image is no longer available" }
        require(image.width in 1..MAX_EDGE && image.height in 1..MAX_EDGE && image.allocationByteCount <= MAX_BYTES) {
            "Image exceeds the supported dimensions or memory size"
        }
    }
}
