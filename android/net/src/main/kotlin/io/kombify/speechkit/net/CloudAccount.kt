package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi
import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.time.Instant

/** Plan feature keys the hosted gateway decides on for SpeechKit. */
object CloudFeatureKeys {
    const val SERVER_HOSTED = "speechkit.server.hosted"
    const val VOICE_AGENT_LIVE = "speechkit.voiceagent.live"
    const val MANAGED_STT = "speechkit.provider.managed.stt"
    const val MANAGED_TTS = "speechkit.provider.managed.tts"
    const val MANAGED_VOICE_AGENT = "speechkit.provider.managed.voiceagent"
    const val HOME_ASSISTANT_BRIDGE = "speechkit.homeassistant.bridge"
    const val DEVICE_FLEET = "speechkit.device.fleet"

    internal val ALL = listOf(
        SERVER_HOSTED,
        VOICE_AGENT_LIVE,
        MANAGED_STT,
        MANAGED_TTS,
        MANAGED_VOICE_AGENT,
        HOME_ASSISTANT_BRIDGE,
        DEVICE_FLEET,
    )

    const val MINUTES_BUDGET = "speechkit.minutes"
}

/** Hosted voice minutes of the current period. */
data class CloudVoiceMinutes(
    val limit: Int,
    val remaining: Int,
    /** When the period resets, epoch milliseconds; null when the Gateway did not say. */
    val resetsAtEpochMs: Long?,
    val exhausted: Boolean,
)

/** What the signed-in kombify account is and what its plan lets this app do. */
data class CloudAccount(
    val email: String,
    val name: String,
    /** Plan id as the Gateway names it ("pro", "starter", ...); null on the free tier. */
    val plan: String?,
    val voiceMinutes: CloudVoiceMinutes?,
    /** Hosted dictation: hosted server plus managed speech-to-text. */
    val hostedDictation: Boolean,
    val voiceAgent: Boolean,
    val assist: Boolean,
    /** False when the budgets call failed: plan name and minutes are then unknown, not "free". */
    val planKnown: Boolean = true,
) {
    val allIncluded: Boolean get() = hostedDictation && voiceAgent && assist
}

sealed interface CloudAccountResult {
    data class Loaded(val account: CloudAccount) : CloudAccountResult

    /** Signed in at the identity provider, but no cloud account exists yet. */
    data object NotProvisioned : CloudAccountResult

    /** The sign-in is gone: its refresh token was refused or the user disconnected. */
    data object SignedOut : CloudAccountResult

    /** Network or server trouble; the sign-in itself is not in question. */
    data class Unavailable(val httpStatus: Int? = null, val call: String? = null) : CloudAccountResult {
        /** Short code for support, e.g. `profile 401`; null when nothing is known. */
        val supportCode: String?
            get() = when {
                call != null && httpStatus != null -> "$call $httpStatus"
                call != null -> "$call network"
                httpStatus != null -> httpStatus.toString()
                else -> null
            }
    }
}

/**
 * Reads the account, plan and voice budget from the hosted gateway
 * (the paths in [KombifyCloudAuth]: profile, feature decision, usage budgets)
 * with the user's own bearer.
 */
class CloudAccountApi(
    private val origin: String = KombifyCloudAuth.DEFAULT_AUDIENCE,
    private val client: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    moshi: Moshi = DictationStreamCodec.defaultMoshi(),
) {
    private val profileAdapter = moshi.adapter(CloudProfileDto::class.java)
    private val decideAdapter = moshi.adapter(DecideResponseDto::class.java)
    private val decideRequestAdapter = moshi.adapter(DecideRequestDto::class.java)
    private val budgetsAdapter = moshi.adapter(BudgetsResponseDto::class.java)

    private class Reply(val status: Int, val body: String)

    private fun Reply.logIfFailed(call: String): Reply {
        if (status !in 200..299) VoiceLog.w(VoiceLog.NET, "cloud account $call failed http=$status")
        return this
    }

    /**
     * @param recover renews a rejected bearer (Companion re-provision or refresh
     *   token) and returns the new one, or null. Called at most once. Null
     *   covers network trouble as well as a dead session, so it alone never
     *   means signed out.
     * @param sessionGone whether no session is stored any more (the refresh
     *   token was refused, the user disconnected). Only this turns a failed
     *   renewal into [CloudAccountResult.SignedOut]; otherwise the view shows
     *   [CloudAccountResult.Unavailable] and the user can retry.
     */
    suspend fun load(
        accessToken: String,
        recover: suspend (rejected: String) -> String?,
        sessionGone: () -> Boolean = { false },
        cloudSessionGeneration: String? = null,
    ): CloudAccountResult = withContext(Dispatchers.IO) {
        try {
            var token = CloudBearer.fresh(accessToken, cloudSessionGeneration)
                ?: return@withContext CloudAccountResult.SignedOut
            if (sessionGone()) return@withContext CloudAccountResult.SignedOut
            var profile = get(KombifyCloudAuth.PROFILE_PATH, token).logIfFailed(CALL_PROFILE)
            if (profile.status == 401) {
                token = recover(token) ?: return@withContext if (sessionGone()) {
                    CloudAccountResult.SignedOut
                } else {
                    CloudAccountResult.Unavailable(profile.status, CALL_PROFILE)
                }
                profile = get(KombifyCloudAuth.PROFILE_PATH, token).logIfFailed(CALL_PROFILE)
                if (profile.status == 401) return@withContext CloudAccountResult.SignedOut
            }
            if (profile.status == 403 && profile.body.contains("identity_not_provisioned")) {
                return@withContext CloudAccountResult.NotProvisioned
            }
            if (profile.status !in 200..299) {
                return@withContext CloudAccountResult.Unavailable(profile.status, CALL_PROFILE)
            }
            val identity = runCatching { profileAdapter.fromJson(profile.body) }.getOrNull()

            val bearer = token
            val (decide, budgets) = coroutineScope {
                val decision = async { postDecide(bearer).logIfFailed(CALL_DECIDE) }
                val budgetCall = async { get(KombifyCloudAuth.BUDGETS_PATH, bearer).logIfFailed(CALL_BUDGETS) }
                decision.await() to budgetCall.await()
            }
            if (decide.status !in 200..299) {
                return@withContext CloudAccountResult.Unavailable(decide.status, CALL_DECIDE)
            }
            val missing = runCatching { decideAdapter.fromJson(decide.body) }.getOrNull()?.missing
                ?: run {
                    VoiceLog.w(VoiceLog.NET, "cloud account $CALL_DECIDE unreadable http=${decide.status}")
                    return@withContext CloudAccountResult.Unavailable(decide.status, CALL_DECIDE)
                }
            // The plan name rides on the budgets answer. When that call fails the
            // card still shows the features from the decision, with plan and
            // minutes left out (planKnown = false), rather than calling a paying
            // user "Free" or failing the whole card.
            val usage = if (budgets.status in 200..299) {
                runCatching { budgetsAdapter.fromJson(budgets.body) }.getOrNull().also {
                    if (it == null) {
                        VoiceLog.w(VoiceLog.NET, "cloud account $CALL_BUDGETS unreadable http=${budgets.status}")
                    }
                }
            } else {
                null
            }

            fun has(key: String) = key !in missing
            CloudAccountResult.Loaded(
                CloudAccount(
                    email = identity?.email.orEmpty(),
                    name = identity?.name.orEmpty(),
                    plan = usage?.plan?.trim()?.takeIf { it.isNotEmpty() },
                    voiceMinutes = usage?.budgets
                        ?.firstOrNull { it.key == CloudFeatureKeys.MINUTES_BUDGET }
                        ?.toVoiceMinutes(),
                    hostedDictation = has(CloudFeatureKeys.SERVER_HOSTED) && has(CloudFeatureKeys.MANAGED_STT),
                    voiceAgent = has(CloudFeatureKeys.VOICE_AGENT_LIVE),
                    assist = has(CloudFeatureKeys.SERVER_HOSTED),
                    planKnown = usage != null,
                ),
            )
        } catch (e: CancellationException) {
            throw e
        } catch (e: IOException) {
            VoiceLog.w(VoiceLog.NET, "cloud account request failed ${e.javaClass.simpleName}")
            CloudAccountResult.Unavailable()
        }
    }

    private fun get(path: String, bearer: String): Reply = execute(
        Request.Builder().url(origin.trimEnd('/') + path).get(),
        bearer,
    )

    private fun postDecide(bearer: String): Reply = execute(
        Request.Builder()
            .url(origin.trimEnd('/') + KombifyCloudAuth.FEATURE_DECISION_PATH)
            .post(
                decideRequestAdapter.toJson(DecideRequestDto(CloudFeatureKeys.ALL))
                    .toRequestBody(JSON),
            ),
        bearer,
    )

    private fun execute(builder: Request.Builder, bearer: String): Reply {
        val request = builder
            .header("Authorization", "Bearer $bearer")
            .header("Accept", "application/json")
            .build()
        client.newCall(request).execute().use { response ->
            return Reply(response.code, response.body?.string().orEmpty())
        }
    }

    private companion object {
        val JSON = "application/json".toMediaType()
        const val CALL_PROFILE = "profile"
        const val CALL_DECIDE = "decide"
        const val CALL_BUDGETS = "budgets"
    }
}

private fun BudgetDto.toVoiceMinutes(): CloudVoiceMinutes {
    val max = (limit ?: 0.0).coerceAtLeast(0.0)
    val left = (remaining ?: (max - (used ?: 0.0) - (reserved ?: 0.0))).coerceAtLeast(0.0)
    return CloudVoiceMinutes(
        limit = max.toInt(),
        remaining = left.toInt(),
        resetsAtEpochMs = resetsAt?.let { runCatching { Instant.parse(it).toEpochMilli() }.getOrNull() },
        exhausted = status == "exhausted" || left <= 0.0,
    )
}

@JsonClass(generateAdapter = true)
internal data class CloudProfileDto(
    val email: String? = null,
    val name: String? = null,
)

@JsonClass(generateAdapter = true)
internal data class DecideRequestDto(val requested: List<String>)

@JsonClass(generateAdapter = true)
internal data class DecideResponseDto(
    /** Requested keys the plan does not hold; absent means the answer is unusable. */
    val missing: List<String>? = null,
)

@JsonClass(generateAdapter = true)
internal data class BudgetsResponseDto(
    val plan: String? = null,
    val tier: String? = null,
    val budgets: List<BudgetDto> = emptyList(),
)

@JsonClass(generateAdapter = true)
internal data class BudgetDto(
    val key: String = "",
    val limit: Double? = null,
    val used: Double? = null,
    val reserved: Double? = null,
    val remaining: Double? = null,
    @Json(name = "resets_at") val resetsAt: String? = null,
    val status: String? = null,
)
