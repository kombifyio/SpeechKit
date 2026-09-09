package io.kombify.speechkit.app.companion

import io.kombify.speechkit.domain.ConnectionMode

/**
 * Only a provisioned Companion session may enter kombify_cloud. Empty,
 * rejected, and unavailable outcomes leave the current mode unchanged so a
 * tester service bearer is never used as a Cloud fallback.
 */
fun cloudModeAfterProvision(outcome: CompanionProvision): ConnectionMode? =
    when (outcome) {
        is CompanionProvision.Session -> ConnectionMode.KOMBIFY_CLOUD
        CompanionProvision.Empty,
        CompanionProvision.Rejected,
        CompanionProvision.Unavailable,
        -> null
    }
