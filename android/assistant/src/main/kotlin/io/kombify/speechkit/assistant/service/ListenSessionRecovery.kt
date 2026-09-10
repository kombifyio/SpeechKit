package io.kombify.speechkit.assistant.service

import io.kombify.speechkit.assistant.intent.CloudSessionRecovery
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.SpeechKitApiException

/** A 401 on the Cloud session re-provisions; any other failure is left alone. */
internal fun recoveredListenProfile(
    failed: ConnectionProfile,
    error: SpeechKitApiException,
    recovery: CloudSessionRecovery?,
): ConnectionProfile.Server? {
    if (error.httpStatus != 401) return null
    return recovery?.recoverFromUnauthorized(failed)
}
