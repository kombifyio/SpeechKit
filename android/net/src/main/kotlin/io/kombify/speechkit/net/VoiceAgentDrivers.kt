package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionProfile

/** Selects a protocol adapter once; all hosts retain the same media and conversation lifecycle. */
fun voiceAgentDriver(
    profile: ConnectionProfile,
    renewalClaimNamespace: String,
    context: Context? = null,
    renewalProfile: (suspend () -> ConnectionProfile)? = null,
    target: RegisteredAgentTarget? = null,
    localHandoff: (() -> Unit)? = null,
    localReady: suspend () -> Boolean = { false },
    policyOverride: io.kombify.speechkit.domain.ExecutionPolicy? = null,
): VoiceAgentSessionDriver = when (profile) {
    is ConnectionProfile.Firebase -> FirebaseVoiceAgentDriver(
        profile, requireNotNull(context), localHandoff, localReady, policyOverride,
    )
    is ConnectionProfile.Byok -> GeminiDirectVoiceAgentDriver(
        profile, requireNotNull(context) { "Direct voice requires device credential custody" }, localHandoff, localReady = localReady, policyOverride = policyOverride,
    )
    else -> VoiceAgentController(
        profile = profile,
        renewalClaimNamespace = renewalClaimNamespace,
        context = context,
        renewalProfile = renewalProfile,
        target = target,
        localHandoff = localHandoff,
        localReady = localReady,
        policyOverride = policyOverride,
    )
}
