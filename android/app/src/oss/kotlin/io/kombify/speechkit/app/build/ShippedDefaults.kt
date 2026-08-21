package io.kombify.speechkit.app.build

/**
 * The oss flavor ships no connection at all: it is the zero-config build, and
 * a default server would contradict that.
 */
object ShippedDefaults {

    val serverUrl: String? get() = null
}
