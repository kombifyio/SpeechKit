package io.kombify.speechkit.net

import android.content.Context
import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.vocabulary.ActiveVocabulary
import io.kombify.speechkit.vocabulary.Vocabulary
import io.kombify.speechkit.vocabulary.VocabularyTerm
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.IOException

/**
 * Carries the signed-in account's vocabulary to this device (the Android
 * counterpart of the desktop's `cloudsignin/vocabulary.go`).
 *
 * The hosted server publishes the account vocabulary as an ordinary
 * customization template pack. While the user is signed in, the app fetches
 * that pack and installs it as the [ActiveVocabulary] every dictation session
 * reads. The last pack is cached on disk so a start without network still
 * recognizes the terms; signing out drops it again. The list itself comes from
 * the server only and is never part of these sources.
 */
object AccountVocabulary {
    private const val PACK_PATH = "/customization/templates/operator-terms/pack"
    private const val CACHE_FILE = "account-vocabulary.json"

    private val lock = Any()

    // Counts sign-outs. A sync that was already on the wire when the user
    // signed out must not install the vocabulary again once its fetch returns.
    private var epoch = 0L

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val packAdapter by lazy {
        DictationStreamCodec.defaultMoshi().adapter(VocabularyPackDto::class.java)
    }

    /** Installs the cached pack at process start, so an offline start keeps the terms. */
    fun restore(context: Context) {
        if (StoredServerProfile.loadCloud(context) == null) return
        val raw = runCatching { cacheFile(context).readText() }.getOrNull() ?: return
        val pack = parse(raw) ?: return
        synchronized(lock) { ActiveVocabulary.set(pack.toVocabulary()) }
        VoiceLog.i(VoiceLog.NET, "account vocabulary restored words=${pack.words.orEmpty().size}")
    }

    /** Syncs once without blocking the caller; a failure is logged, never raised. */
    fun syncInBackground(context: Context) {
        val appContext = context.applicationContext
        scope.launch {
            try {
                sync(appContext)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                VoiceLog.w(VoiceLog.NET, "account vocabulary sync failed", e)
            }
        }
    }

    /**
     * Fetches the pack with the account bearer and installs it. A device that
     * is not signed in, or whose request fails, keeps what it has.
     *
     * @return true when a pack was installed.
     */
    suspend fun sync(
        context: Context,
        client: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    ): Boolean = withContext(Dispatchers.IO) {
        // The epoch first: a sign-out between the two reads must reject this account's pack.
        val startEpoch = synchronized(lock) { epoch }
        val profile = StoredServerProfile.loadCloud(context) ?: return@withContext false
        val url = profile.normalizedBaseUrl.trimEnd('/') + PACK_PATH
        var bearer = CloudBearer.fresh(profile.bearerToken, profile.cloudSessionGeneration)?.takeIf { it.isNotBlank() }
            ?: return@withContext false

        var reply = get(client, url, bearer)
        if (reply.status == 401) {
            bearer = CloudBearer.recoverRejected(bearer, profile.cloudSessionGeneration) ?: return@withContext false
            reply = get(client, url, bearer)
        }
        if (reply.status !in 200..299) {
            VoiceLog.i(VoiceLog.NET, "account vocabulary not synced http=${reply.status}")
            return@withContext false
        }
        val pack = parse(reply.body) ?: return@withContext false

        synchronized(lock) {
            if (epoch != startEpoch) return@withContext false
            ActiveVocabulary.set(pack.toVocabulary())
            runCatching { cacheFile(context).writeText(reply.body) }
        }
        VoiceLog.i(VoiceLog.NET, "account vocabulary synced words=${pack.words.orEmpty().size}")
        true
    }

    /** Drops the vocabulary and its cache, on sign-out. */
    fun clear(context: Context) {
        synchronized(lock) {
            epoch += 1
            ActiveVocabulary.clear()
            runCatching { cacheFile(context).delete() }
        }
    }

    private class Reply(val status: Int, val body: String)

    private fun get(client: OkHttpClient, url: String, bearer: String): Reply {
        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer $bearer")
            .header("Accept", "application/json")
            .get()
            .build()
        return try {
            client.newCall(request).execute().use { Reply(it.code, it.body?.string().orEmpty()) }
        } catch (e: IOException) {
            Reply(0, "")
        }
    }

    private fun parse(raw: String): VocabularyPackDto? =
        runCatching { packAdapter.fromJson(raw) }.getOrNull()

    private fun VocabularyPackDto.toVocabulary(): Vocabulary = Vocabulary(
        words.orEmpty().filter { it.enabled }.map { VocabularyTerm(it.term, it.soundsLike.orEmpty()) },
    )

    private fun cacheFile(context: Context) = File(context.applicationContext.filesDir, CACHE_FILE)
}

/** The parts of the Go `speechcustomize.Pack` this app reads. */
@JsonClass(generateAdapter = true)
internal data class VocabularyPackDto(
    val words: List<VocabularyWordDto>? = null,
)

@JsonClass(generateAdapter = true)
internal data class VocabularyWordDto(
    val term: String = "",
    @Json(name = "sounds_like") val soundsLike: List<String>? = null,
    val enabled: Boolean = false,
)
