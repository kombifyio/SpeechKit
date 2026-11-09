package io.kombify.speechkit.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.kombify.speechkit.R
import io.kombify.speechkit.vocabulary.ActiveVocabulary
import io.kombify.speechkit.vocabulary.Vocabulary

/**
 * Shows the vocabulary the signed-in account carries to this phone and proves that it reaches the
 * recognizer.
 *
 * The card lists the terms and runs the account's own misheard variants through the very rewrite
 * the on-device tiers apply ([Vocabulary.rewrite]). What a recognizer produces is therefore turned
 * into the canonical term in front of the user, which is the part that used to be invisible.
 */
@Composable
internal fun VocabularyCard() {
    // Read at composition time: the vocabulary is installed when the account signs in and dropped
    // when it signs out, and the settings screen is built again when it is opened.
    val vocabulary = ActiveVocabulary.current

    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                stringResource(R.string.settings_vocabulary_title),
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.Medium,
            )
            Text(
                stringResource(R.string.settings_vocabulary_subtitle),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )

            if (vocabulary.isEmpty) {
                Text(
                    stringResource(R.string.settings_vocabulary_empty),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                return@Column
            }

            Text(
                stringResource(R.string.settings_vocabulary_count, vocabulary.canonicalTerms.size),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.primary,
            )

            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                for (term in vocabulary.canonicalTerms) {
                    Text("· $term", style = MaterialTheme.typography.bodySmall)
                }
            }

            VocabularyAliases(vocabulary)
        }
    }
}

/**
 * The account's own misheard variants next to the term they stand for, and the result of the
 * rewrite that the on-device tiers apply. The words come from the account, so this shows the real
 * effect and not a fixed example.
 */
@Composable
private fun VocabularyAliases(vocabulary: Vocabulary) {
    val pairs = remember(vocabulary) { vocabulary.aliasPairs() }
    if (pairs.isEmpty()) {
        Text(
            stringResource(R.string.settings_vocabulary_no_aliases),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(
            stringResource(R.string.settings_vocabulary_sample_heard),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        for ((alias, term) in pairs) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(alias, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodySmall)
                Text(
                    "→ $term",
                    modifier = Modifier.weight(1f),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}
