package io.kombify.speechkit.app.companion

import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentContract
import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentSessionRequest
import io.kombify.speechkit.net.VoiceAgentStartFrame

/** The attested request's media choice survives unchanged into the existing WS start. */
internal fun VoiceAgentSessionRequest.voiceAgentStartOptions(): VoiceAgentStartFrame {
    require(VoiceAgentContract.supportsMediaProvider(VoiceAgentContract.VERSION, mediaProvider)) {
        VoiceAgentContract.ERROR_MEDIA_PROVIDER_UNAVAILABLE
    }
    return VoiceAgentStartFrame(
        mediaProvider = mediaProvider?.takeIf { it.isNotEmpty() },
        locale = locale?.takeIf { Regex("^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$").matches(it) },
    )
}
