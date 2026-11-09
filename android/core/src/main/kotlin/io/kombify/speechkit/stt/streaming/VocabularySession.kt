package io.kombify.speechkit.stt.streaming

import io.kombify.speechkit.vocabulary.ActiveVocabulary
import io.kombify.speechkit.vocabulary.Vocabulary
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

/**
 * Carries the signed-in account's vocabulary into a [StreamingSttSession]:
 * - every segment starts with the vocabulary as [DictationSegmentOptions.keyterms]
 *   (hosted tiers send them in the start frame, the system recognizer takes
 *   them as biasing strings, others ignore them);
 * - with [rewriteResults], drafts and finals have the pack's aliases replaced
 *   by the canonical term. Hosted tiers apply the vocabulary themselves, so
 *   only the on-device tiers need this.
 *
 * Reads the vocabulary per segment and per event, so a pack installed or
 * dropped while a session is open applies from the next one.
 */
class VocabularySession(
    private val delegate: StreamingSttSession,
    private val rewriteResults: Boolean,
    private val keytermsWithAliases: Boolean,
    private val vocabulary: () -> Vocabulary = { ActiveVocabulary.current },
) : StreamingSttSession {

    override val capturesOwnAudio: Boolean get() = delegate.capturesOwnAudio

    override val events: Flow<TranscriptEvent> =
        if (rewriteResults) delegate.events.map(::rewrite) else delegate.events

    override suspend fun startSegment(options: DictationSegmentOptions) {
        val terms = vocabulary().keyterms(keytermsWithAliases)
        val merged = if (terms.isEmpty()) {
            options
        } else {
            val seen = HashSet<String>()
            options.copy(keyterms = (options.keyterms + terms).filter { seen.add(it.lowercase()) })
        }
        delegate.startSegment(merged)
    }

    override suspend fun sendAudio(pcm: ByteArray) = delegate.sendAudio(pcm)

    override suspend fun finishSegment() = delegate.finishSegment()

    override suspend fun close() = delegate.close()

    override suspend fun keepAlive(): Boolean = delegate.keepAlive()

    override fun toString(): String = "VocabularySession($delegate)"

    private fun rewrite(event: TranscriptEvent): TranscriptEvent {
        val vocab = vocabulary()
        if (vocab.isEmpty) return event
        return when (event) {
            is TranscriptEvent.Draft -> event.copy(text = vocab.rewrite(event.text))
            is TranscriptEvent.Final -> event.copy(text = vocab.rewrite(event.text))
            else -> event
        }
    }
}
