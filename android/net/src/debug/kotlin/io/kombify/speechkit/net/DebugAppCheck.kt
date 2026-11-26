package io.kombify.speechkit.net

import com.google.firebase.appcheck.FirebaseAppCheck
import com.google.firebase.appcheck.debug.DebugAppCheckProviderFactory

internal fun installDebugAppCheck(check: FirebaseAppCheck) {
    check.installAppCheckProviderFactory(DebugAppCheckProviderFactory.getInstance(), false)
}
