package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.stt.parakeet.ParakeetOnDevice

/**
 * Which recognizer on-device dictation uses: the platform's system
 * recognizer (the zero-config default) or a downloaded Parakeet model
 * ([ConnectionProfile.Local]). Stored with the connection settings.
 *
 * Parakeet is used only while it is chosen, installed, supported by the
 * device and not known to fail loading; otherwise [profile] falls back to the
 * system recognizer, so a deleted or damaged model or a device that cannot run
 * it does not leave dictation broken. Parakeet does not transcribe Chinese,
 * Hindi or Arabic: a segment pinned to one of them fails with
 * `language_unsupported`, so people who dictate in them keep the system
 * recognizer.
 */
object OnDeviceEngine {
    enum class Choice(val wire: String) {
        SYSTEM("system"),
        PARAKEET("parakeet"),
        ;

        companion object {
            fun fromWire(raw: String?): Choice = entries.firstOrNull { it.wire == raw } ?: SYSTEM
        }
    }

    const val KEY_ON_DEVICE_ENGINE = "on_device_engine"

    fun choice(context: Context): Choice = Choice.fromWire(
        context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE)
            .getString(KEY_ON_DEVICE_ENGINE, null),
    )

    fun setChoice(context: Context, choice: Choice) {
        context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_ON_DEVICE_ENGINE, choice.wire)
            .apply()
    }

    /** True when a Parakeet model is installed, loads, and this device can run it. */
    fun parakeetReady(context: Context): Boolean = ParakeetOnDevice.usableFiles(context) != null

    /** The on-device profile to dictate with now. */
    fun profile(
        context: Context,
        system: ConnectionProfile.SystemOnDevice = ConnectionProfile.SystemOnDevice(),
    ): ConnectionProfile =
        if (choice(context) == Choice.PARAKEET && parakeetReady(context)) ConnectionProfile.Local else system

    /**
     * [profile] with its system on-device tier replaced by the chosen engine;
     * server and other profiles pass through unchanged.
     */
    fun resolve(context: Context, profile: ConnectionProfile): ConnectionProfile =
        if (profile is ConnectionProfile.SystemOnDevice) profile(context, profile) else profile
}
