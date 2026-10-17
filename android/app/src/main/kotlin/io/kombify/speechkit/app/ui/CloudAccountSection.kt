package io.kombify.speechkit.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import io.kombify.speechkit.R
import io.kombify.speechkit.app.companion.CompanionProvisioner
import io.kombify.speechkit.app.companion.recoverCloudSession
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.CloudAccount
import io.kombify.speechkit.net.CloudAccountApi
import io.kombify.speechkit.net.CloudAccountResult
import io.kombify.speechkit.net.CloudVoiceMinutes
import io.kombify.speechkit.net.KombifyCloudAuth
import io.kombify.speechkit.net.KombifyLinks
import io.kombify.speechkit.net.StoredServerProfile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.text.DateFormat
import java.util.Date

private sealed interface AccountState {
    data object Loading : AccountState
    data class Loaded(val account: CloudAccount) : AccountState
    data object NotProvisioned : AccountState
    data object SignedOut : AccountState
    data object Unavailable : AccountState
}

/**
 * The connected kombify account: who is signed in, which plan, how many hosted
 * voice minutes are left, and which of the voice features this app uses the
 * plan includes. It shows only what SpeechKit on Android actually uses
 * (dictation, Assist, Voice Agent); a feature the plan lacks says so and
 * leads to the plans page.
 */
@Composable
internal fun CloudAccountSection(
    provisioner: CompanionProvisioner,
    onSessionChanged: () -> Unit,
) {
    val context = LocalContext.current
    var reload by remember { mutableIntStateOf(0) }
    var state by remember { mutableStateOf<AccountState>(AccountState.Loading) }

    LaunchedEffect(reload) {
        state = AccountState.Loading
        val session = provisioner.currentSession() ?: StoredServerProfile.loadCloud(context)
        val token = session?.bearerToken
        if (session == null || token.isNullOrBlank()) {
            state = AccountState.SignedOut
            onSessionChanged()
            return@LaunchedEffect
        }
        val result = CloudAccountApi(origin = cloudOrigin(session.baseUrl)).load(
            accessToken = token,
            recover = { rejected ->
                withContext(Dispatchers.IO) {
                    recoverCloudSession(
                        context,
                        provisioner,
                        ConnectionProfile.Server(session.baseUrl, rejected),
                    )
                }?.bearerToken
            },
            // Signed out means no session is left, not that one renewal failed.
            sessionGone = {
                provisioner.currentSession() == null &&
                    StoredServerProfile.loadCloud(context)?.bearerToken.isNullOrBlank()
            },
        )
        state = when (result) {
            is CloudAccountResult.Loaded -> AccountState.Loaded(result.account)
            CloudAccountResult.NotProvisioned -> AccountState.NotProvisioned
            CloudAccountResult.SignedOut -> AccountState.SignedOut
            is CloudAccountResult.Unavailable -> AccountState.Unavailable
        }
        // A sign-in that could not be renewed may have been cleared underneath
        // this screen: let Settings re-read it.
        if (result == CloudAccountResult.SignedOut) onSessionChanged()
    }

    // A plan changed in the browser shows up when the user comes back.
    val lifecycle = androidx.compose.ui.platform.LocalLifecycleOwner.current
    DisposableEffect(lifecycle) {
        var skipReplayedResume = true
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                if (skipReplayedResume) skipReplayedResume = false else reload += 1
            }
        }
        lifecycle.lifecycle.addObserver(observer)
        onDispose { lifecycle.lifecycle.removeObserver(observer) }
    }

    Column(
        modifier = Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        when (val shown = state) {
            AccountState.Loading -> Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                Text(
                    stringResource(R.string.settings_connection_account_loading),
                    style = MaterialTheme.typography.bodySmall,
                )
            }

            is AccountState.Loaded -> AccountDetails(
                account = shown.account,
                onSeePlans = { KombifyLinks.open(context, KombifyLinks.PLANS_URL) },
                onBilling = { KombifyLinks.open(context, KombifyLinks.BILLING_URL) },
            )

            AccountState.NotProvisioned -> {
                Text(
                    stringResource(R.string.settings_connection_account_not_provisioned),
                    style = MaterialTheme.typography.bodySmall,
                )
                OutlinedButton(
                    onClick = { KombifyLinks.open(context, KombifyLinks.DASHBOARD_URL) },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.settings_connection_account_open_dashboard)) }
            }

            AccountState.SignedOut -> Text(
                stringResource(R.string.settings_connection_cloud_session_ended),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
            )

            AccountState.Unavailable -> Text(
                stringResource(R.string.settings_connection_account_unavailable),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
        TextButton(
            onClick = { reload += 1 },
            enabled = state != AccountState.Loading,
        ) { Text(stringResource(R.string.settings_connection_account_refresh)) }
    }
}

@Composable
private fun AccountDetails(
    account: CloudAccount,
    onSeePlans: () -> Unit,
    onBilling: () -> Unit,
) {
    val identity = account.email.ifBlank { account.name }
    if (identity.isNotBlank()) {
        Text(
            stringResource(R.string.settings_connection_account_signed_in_as, identity),
            style = MaterialTheme.typography.bodySmall,
        )
    }
    val planLabel = account.plan?.let(::planDisplayName)
        ?: stringResource(R.string.settings_connection_account_plan_free)
    Text(
        stringResource(R.string.settings_connection_account_plan, planLabel),
        style = MaterialTheme.typography.bodySmall,
    )
    account.voiceMinutes?.let { minutes ->
        Text(minutesLabel(minutes), style = MaterialTheme.typography.bodySmall)
    }
    FeatureRow(stringResource(R.string.mode_dictate), account.hostedDictation)
    FeatureRow(stringResource(R.string.mode_assist), account.assist)
    FeatureRow(stringResource(R.string.mode_voice_agent), account.voiceAgent)
    if (!account.allIncluded) {
        OutlinedButton(onClick = onSeePlans, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.settings_connection_account_see_plans))
        }
    }
    if (account.plan != null) {
        TextButton(onClick = onBilling) {
            Text(stringResource(R.string.settings_connection_account_billing))
        }
    }
}

@Composable
private fun FeatureRow(name: String, included: Boolean) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(name, style = MaterialTheme.typography.bodySmall)
        Text(
            stringResource(
                if (included) R.string.settings_connection_account_included
                else R.string.settings_connection_account_not_included,
            ),
            style = MaterialTheme.typography.bodySmall,
            color = if (included) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error,
        )
    }
}

@Composable
private fun minutesLabel(minutes: CloudVoiceMinutes): String {
    val reset = minutes.resetsAtEpochMs?.let {
        DateFormat.getDateInstance(DateFormat.MEDIUM).format(Date(it))
    }
    return when {
        minutes.limit == 0 -> stringResource(R.string.settings_connection_account_minutes_none)
        minutes.exhausted && reset != null ->
            stringResource(R.string.settings_connection_account_minutes_used_up, reset)
        minutes.exhausted -> stringResource(R.string.settings_connection_account_minutes_used_up_no_reset)
        reset != null -> stringResource(
            R.string.settings_connection_account_minutes_left,
            minutes.remaining,
            minutes.limit,
            reset,
        )
        else -> stringResource(
            R.string.settings_connection_account_minutes_left_no_reset,
            minutes.remaining,
            minutes.limit,
        )
    }
}

/** "all-you-need" becomes "All You Need". */
private fun planDisplayName(plan: String): String =
    plan.split('-', '_', ' ')
        .filter { it.isNotEmpty() }
        .joinToString(" ") { word -> word.replaceFirstChar { it.uppercaseChar() } }

/** The gateway origin of a stored cloud server URL (scheme and host, without the path). */
private fun cloudOrigin(baseUrl: String): String {
    val trimmed = baseUrl.trim().trimEnd('/')
    val cut = trimmed.indexOf("/v1/")
    val origin = if (cut >= 0) trimmed.substring(0, cut) else trimmed.removeSuffix("/v1")
    return origin.ifEmpty { KombifyCloudAuth.DEFAULT_AUDIENCE }
}
