package io.kombify.speechkit.app.companion

import io.kombify.speechkit.R

/**
 * What Settings shows after a kombify Cloud connect attempt.
 *
 * The click must never look like a no-op: every outcome has a message next to
 * the button. Opening Companion is reserved for a missing install — bouncing
 * back on a signed-out Companion is what turned Connect into a ping-pong.
 */
data class CloudConnectUi(
    val messageRes: Int,
    val openCompanion: Boolean,
)

fun cloudConnectUi(
    outcome: CompanionProvision,
    companionInstalled: Boolean,
): CloudConnectUi = when (outcome) {
    is CompanionProvision.Session -> CloudConnectUi(
        messageRes = R.string.settings_connection_cloud_connected,
        openCompanion = false,
    )
    CompanionProvision.Empty -> CloudConnectUi(
        messageRes = R.string.settings_connection_cloud_signed_out,
        openCompanion = false,
    )
    CompanionProvision.Rejected -> CloudConnectUi(
        messageRes = R.string.settings_connection_cloud_rejected,
        openCompanion = false,
    )
    CompanionProvision.Unavailable -> CloudConnectUi(
        messageRes = if (companionInstalled) {
            R.string.settings_connection_cloud_failed
        } else {
            R.string.settings_connection_cloud_missing
        },
        openCompanion = !companionInstalled,
    )
}
