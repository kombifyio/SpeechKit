package io.kombify.speechkit.log

import timber.log.Timber

/**
 * The one logcat surface for Android voice paths.
 *
 * Filter: `adb logcat -s sk.voice`
 *
 * Never log PCM, bearer tokens, or transcript text. Length, HTTP status,
 * error codes, and host URLs are enough to diagnose an on-device failure.
 */
object VoiceLog {
    const val TAG = "sk.voice"

    const val ASSIST = "assist"
    const val DICTATION = "dictation"
    const val AGENT = "voiceagent"
    const val AUDIO = "audio"
    const val NET = "net"

    fun i(area: String, message: String) {
        Timber.tag(TAG).i("%s", "$area $message")
    }

    fun w(area: String, message: String, t: Throwable? = null) {
        val line = "$area $message"
        if (t != null) {
            Timber.tag(TAG).w(t, "%s", line)
        } else {
            Timber.tag(TAG).w("%s", line)
        }
    }

    fun e(area: String, message: String, t: Throwable? = null) {
        val line = "$area $message"
        if (t != null) {
            Timber.tag(TAG).e(t, "%s", line)
        } else {
            Timber.tag(TAG).e("%s", line)
        }
    }
}
