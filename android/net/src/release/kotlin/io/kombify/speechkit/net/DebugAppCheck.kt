package io.kombify.speechkit.net

import com.google.firebase.appcheck.FirebaseAppCheck

internal fun installDebugAppCheck(check: FirebaseAppCheck): Nothing =
    error("Debug App Check is unavailable in release builds")
