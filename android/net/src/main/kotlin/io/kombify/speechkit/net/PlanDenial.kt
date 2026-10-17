package io.kombify.speechkit.net

import android.content.Context
import android.content.Intent
import android.net.Uri
import com.squareup.moshi.JsonAdapter
import com.squareup.moshi.Moshi
import com.squareup.moshi.Types

/**
 * A hosted-voice request the Gateway refused because the user's kombify plan
 * does not include it (feature missing) or its voice minutes are used up.
 *
 * The Gateway answers with a flat envelope:
 * `{"error":"speechkit_preflight_denied","error_code":"speechkit_feature_not_entitled",
 * "missing_features":[...],"user_guidance":{"title","body","next_steps":[...]}}`.
 */
data class PlanDenial(
    val kind: Kind,
    val errorCode: String,
    val missingFeatures: List<String>,
    /** Server-written guidance; null when the response carried none. */
    val title: String?,
    val body: String?,
    val nextSteps: List<String>,
) {
    enum class Kind {
        /** The plan does not include the feature. */
        FEATURE_NOT_IN_PLAN,

        /** The plan includes it, but the voice minutes are used up or not granted. */
        BUDGET,
    }
}

/** A denial turned into text for one surface. */
data class PlanDenialText(val title: String, val body: String)

/**
 * The one mapper from a plan denial to user text: the server's own guidance
 * when it sent some, otherwise the localized generic message.
 */
fun PlanDenial.userText(context: Context): PlanDenialText {
    val budget = kind == PlanDenial.Kind.BUDGET
    return PlanDenialText(
        title = title?.trim()?.takeIf { it.isNotEmpty() }
            ?: context.getString(if (budget) R.string.plan_denial_budget_title else R.string.plan_denial_title),
        body = body?.trim()?.takeIf { it.isNotEmpty() }
            ?: context.getString(if (budget) R.string.plan_denial_budget_body else R.string.plan_denial_body),
    )
}

/** Customer pages that open in the system browser. They carry no token. */
object KombifyLinks {
    const val PLANS_URL = KombifyCloudAuth.PLANS_URL
    const val BILLING_URL = KombifyCloudAuth.BILLING_URL
    const val DASHBOARD_URL = KombifyCloudAuth.DASHBOARD_URL

    fun intent(url: String): Intent =
        Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)

    /** Opens [url] in the browser; false when no browser could take it. */
    fun open(context: Context, url: String): Boolean =
        runCatching { context.startActivity(intent(url)) }.isSuccess
}

/** Result of reading a non-2xx response body of the Gateway or a self-hosted server. */
internal data class ParsedApiError(
    val code: String,
    val message: String?,
    val denial: PlanDenial?,
)

private val PLAN_ERROR_CODES = KombifyCloudAuth.PLAN_ERROR_CODES

private val looseJsonAdapter: JsonAdapter<Map<String, Any?>> =
    Moshi.Builder().build().adapter(
        Types.newParameterizedType(Map::class.java, String::class.java, Any::class.java),
    )

/**
 * Reads both error shapes: the self-hosted server's nested
 * `{"error":{"code","message"}}` and the Gateway's flat envelope, where
 * `error` is a plain string and the details sit beside it.
 */
internal fun parseApiError(raw: String, status: Int): ParsedApiError {
    val root = runCatching { looseJsonAdapter.fromJson(raw) }.getOrNull()
    val nested = root?.get("error") as? Map<*, *>
    val nestedCode = (nested?.get("code") as? String)?.trim().orEmpty()
    val flatCode = (root?.get("error_code") as? String)?.trim().orEmpty()
    val flatError = (root?.get("error") as? String)?.trim().orEmpty()
    val code = nestedCode.ifEmpty { flatCode }.ifEmpty { flatError }.ifEmpty { "http_$status" }

    val guidance = root?.get("user_guidance") as? Map<*, *>
    val guidanceTitle = (guidance?.get("title") as? String)?.trim()?.takeIf { it.isNotEmpty() }
    val guidanceBody = (guidance?.get("body") as? String)?.trim()?.takeIf { it.isNotEmpty() }
    val message = (nested?.get("message") as? String)?.trim()?.takeIf { it.isNotEmpty() }
        ?: guidanceBody
        ?: (root?.get("message") as? String)?.trim()?.takeIf { it.isNotEmpty() }
        ?: (root?.get("remediation") as? String)?.trim()?.takeIf { it.isNotEmpty() }

    val missing = (root?.get("missing_features") as? List<*>)
        ?.filterIsInstance<String>()
        .orEmpty()
    val planDenied = status in 400..499 &&
        (status == 402 || missing.isNotEmpty() || code in PLAN_ERROR_CODES)
    val denial = if (planDenied) {
        PlanDenial(
            kind = if (status == 402 || code == "speechkit_budget_unavailable" || code == "speechkit_budget_exhausted") {
                PlanDenial.Kind.BUDGET
            } else {
                PlanDenial.Kind.FEATURE_NOT_IN_PLAN
            },
            errorCode = code,
            missingFeatures = missing,
            title = guidanceTitle,
            body = guidanceBody,
            nextSteps = (guidance?.get("next_steps") as? List<*>)
                ?.filterIsInstance<String>()
                .orEmpty(),
        )
    } else {
        null
    }
    return ParsedApiError(code = code, message = message, denial = denial)
}
