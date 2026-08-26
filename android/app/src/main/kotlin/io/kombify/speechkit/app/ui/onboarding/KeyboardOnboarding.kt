package io.kombify.speechkit.app.ui.onboarding

import android.content.Context
import android.content.Intent
import android.provider.Settings
import android.view.inputmethod.InputMethodManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import io.kombify.speechkit.R

/**
 * Onboarding wizard for activating the SpeechKit typing keyboard.
 *
 * Two steps, and they are the whole setup:
 * 1. Enable the keyboard in system settings (InputMethodService)
 * 2. Select SpeechKit Keyboard as the active input method
 *
 * Voice Agent is a key on that keyboard. The system assistant overlay is a
 * separate Assist path and is not a third onboarding step.
 */
@Composable
fun KeyboardOnboardingWizard(
    isKeyboardEnabled: Boolean,
    isKeyboardSelected: Boolean,
    onComplete: () -> Unit,
    onBack: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    var currentStep by remember { mutableIntStateOf(0) }

    var liveKeyboardEnabled by remember { mutableStateOf(isKeyboardEnabled) }
    var liveKeyboardSelected by remember { mutableStateOf(isKeyboardSelected) }
    var liveSetupComplete by remember {
        mutableStateOf(KeyboardSetupChecker.isSetupComplete(isKeyboardEnabled, isKeyboardSelected))
    }

    val lifecycleOwner = LocalLifecycleOwner.current
    androidx.compose.runtime.DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                liveKeyboardEnabled = KeyboardSetupChecker.isKeyboardEnabled(context)
                liveKeyboardSelected = KeyboardSetupChecker.isKeyboardSelected(context)
                liveSetupComplete = KeyboardSetupChecker.isSetupComplete(context)
                if (currentStep == 0 && liveKeyboardEnabled) currentStep = 1
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        if (onBack != null) {
            TextButton(onClick = onBack) {
                Text(stringResource(R.string.keyboard_onboarding_back))
            }
        }

        Text(
            text = stringResource(R.string.keyboard_onboarding_title),
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )

        Text(
            text = stringResource(R.string.keyboard_onboarding_intro),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        Spacer(modifier = Modifier.height(8.dp))

        OnboardingStep(
            stepNumber = 1,
            title = stringResource(R.string.keyboard_onboarding_step1_title),
            description = stringResource(R.string.keyboard_onboarding_step1_body),
            isCompleted = liveKeyboardEnabled,
            isActive = currentStep == 0,
            buttonText = stringResource(R.string.keyboard_onboarding_step1_action),
            onAction = {
                context.startActivity(Intent(Settings.ACTION_INPUT_METHOD_SETTINGS).apply {
                    flags = Intent.FLAG_ACTIVITY_NEW_TASK
                })
            },
        )

        OnboardingStep(
            stepNumber = 2,
            title = stringResource(R.string.keyboard_onboarding_step2_title),
            description = stringResource(R.string.keyboard_onboarding_step2_body),
            isCompleted = liveKeyboardSelected,
            isActive = currentStep == 1 && liveKeyboardEnabled,
            buttonText = stringResource(R.string.keyboard_onboarding_step2_action),
            onAction = {
                val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
                imm.showInputMethodPicker()
            },
        )

        Spacer(modifier = Modifier.weight(1f))

        if (liveSetupComplete) {
            Button(
                onClick = onComplete,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(stringResource(R.string.keyboard_onboarding_done))
            }
        } else {
            // There is always a way out. Whether the keyboard is selected is
            // read from the system, and that read can disagree with what the
            // user just did - an OEM settings screen that reports late, a
            // selection the framework has not committed yet. Gating the only
            // exit on that read turns a wrong answer into a trap, so leaving
            // is offered unconditionally; the steps stay available afterwards.
            TextButton(
                onClick = onComplete,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(stringResource(R.string.keyboard_onboarding_later))
            }
        }
    }
}

@Composable
private fun OnboardingStep(
    stepNumber: Int,
    title: String,
    description: String,
    isCompleted: Boolean,
    isActive: Boolean,
    buttonText: String,
    onAction: () -> Unit,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = when {
                isCompleted -> MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.3f)
                isActive -> MaterialTheme.colorScheme.surface
                else -> MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)
            },
        ),
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(
                    text = if (isCompleted) "\u2713" else "$stepNumber",
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Bold,
                    color = if (isCompleted) MaterialTheme.colorScheme.primary
                    else MaterialTheme.colorScheme.onSurface,
                )

                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = title,
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Medium,
                    )
                    Text(
                        text = description,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            if (isActive && !isCompleted) {
                Spacer(modifier = Modifier.height(12.dp))
                Button(onClick = onAction) {
                    Text(buttonText)
                }
            }
        }
    }
}

/**
 * Checks whether SpeechKit's own typing keyboard is enabled and in use.
 *
 * Detects the HeliBoard fork's [LatinIME], which this APK ships under its own
 * application id. The same APK also registers a voice-only IME
 * (`SpeechKitVoiceImeService`) for hopping over from a third-party keyboard;
 * that is not a typing keyboard, and treating it as one sent testers to a
 * panel with no keys. Standalone HeliBoard (`helium314.keyboard/...`) is still
 * rejected: it is a different application.
 */
object KeyboardSetupChecker {
    private const val LATIN_IME_CLASS = "helium314.keyboard.latin.LatinIME"

    /**
     * Whether [id] — a flattened `package/class` component as the input-method
     * framework hands it out — is this application's typing keyboard.
     *
     * Expanding the short form handles both spellings, and comparing the
     * package to our own keeps a stranger's keyboard from counting as ours.
     * The HeliBoard service lives in its own namespace, so the system stores
     * the fully-qualified class (`io.kombify.speechkit/helium314.keyboard.
     * latin.LatinIME`); the `.oss` flavour still matches because only the
     * package is compared, not a hard-coded application id.
     *
     * The parsing is spelled out instead of calling ComponentName so this rule
     * can be covered by a plain unit test; the framework class is only a stub
     * off-device, which is how the earlier defect stayed invisible.
     */
    internal fun isSpeechKitIme(ownPackage: String, id: String?): Boolean {
        val flattened = id ?: return false
        val separator = flattened.indexOf('/')
        if (separator <= 0 || separator == flattened.length - 1) return false
        if (flattened.substring(0, separator) != ownPackage) return false
        val className = flattened.substring(separator + 1)
        val qualified = if (className.startsWith(".")) ownPackage + className else className
        return qualified == LATIN_IME_CLASS
    }

    fun enabledInputMethodIds(context: Context): List<String> = try {
        val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
        imm.enabledInputMethodList.map { it.id }
    } catch (e: Exception) {
        emptyList()
    }

    /**
     * Whether SpeechKit is the input method currently in use — not merely one
     * of the enabled ones. The previous implementation was byte-identical to
     * [isKeyboardEnabled], so onboarding declared the keyboard selected the
     * moment it was switched on in system settings, and its "now pick it"
     * step could never be reached.
     */
    fun selectedInputMethodId(context: Context): String? = try {
        Settings.Secure.getString(
            context.contentResolver,
            Settings.Secure.DEFAULT_INPUT_METHOD,
        )
    } catch (e: Exception) {
        null
    }

    fun isKeyboardEnabled(context: Context): Boolean =
        enabledInputMethodIds(context).any { isSpeechKitIme(context.packageName, it) }

    fun isKeyboardSelected(context: Context): Boolean =
        isSpeechKitIme(context.packageName, selectedInputMethodId(context))

    /**
     * Keyboard setup is done when the typing IME is enabled and selected.
     * The system assistant role is not an input and must not be added here:
     * Voice Agent is a key on that keyboard, not ROLE_ASSISTANT.
     */
    fun isSetupComplete(
        ownPackage: String,
        enabledInputMethodIds: Iterable<String>,
        selectedInputMethodId: String?,
    ): Boolean = enabledInputMethodIds.any { isSpeechKitIme(ownPackage, it) } &&
        isSpeechKitIme(ownPackage, selectedInputMethodId)

    fun isSetupComplete(enabled: Boolean, selected: Boolean): Boolean = enabled && selected

    fun isSetupComplete(context: Context): Boolean = isSetupComplete(
        context.packageName,
        enabledInputMethodIds(context),
        selectedInputMethodId(context),
    )
}
