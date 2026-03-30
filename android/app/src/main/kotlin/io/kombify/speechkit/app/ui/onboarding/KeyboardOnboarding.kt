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
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver

/**
 * Onboarding wizard for activating the SpeechKit keyboard (HeliBoard fork) and assistant.
 *
 * Three steps:
 * 1. Enable the keyboard in system settings (InputMethodService)
 * 2. Select SpeechKit as the active keyboard
 * 3. (Optional) Set as default voice assistant
 */
@Composable
fun KeyboardOnboardingWizard(
    isKeyboardEnabled: Boolean,
    isKeyboardSelected: Boolean,
    isAssistantSet: Boolean,
    onComplete: () -> Unit,
    onBack: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    var currentStep by remember { mutableIntStateOf(0) }

    var liveKeyboardEnabled by remember { mutableStateOf(isKeyboardEnabled) }
    var liveKeyboardSelected by remember { mutableStateOf(isKeyboardSelected) }
    var liveAssistantSet by remember { mutableStateOf(isAssistantSet) }

    val lifecycleOwner = LocalLifecycleOwner.current
    androidx.compose.runtime.DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                liveKeyboardEnabled = KeyboardSetupChecker.isKeyboardEnabled(context)
                liveKeyboardSelected = KeyboardSetupChecker.isKeyboardSelected(context)
                liveAssistantSet = KeyboardSetupChecker.isAssistantSet(context)
                if (currentStep == 0 && liveKeyboardEnabled) currentStep = 1
                if (currentStep == 1 && liveKeyboardSelected) currentStep = 2
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
                Text("\u2190 Zurueck")
            }
        }

        Text(
            text = "SpeechKit einrichten",
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )

        Text(
            text = "Drei kurze Schritte, um SpeechKit als Tastatur und Voice Assistant zu aktivieren.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        Spacer(modifier = Modifier.height(8.dp))

        OnboardingStep(
            stepNumber = 1,
            title = "Tastatur aktivieren",
            description = "Aktiviere SpeechKit in den Eingabemethoden-Einstellungen.",
            isCompleted = liveKeyboardEnabled,
            isActive = currentStep == 0,
            buttonText = "Einstellungen oeffnen",
            onAction = {
                context.startActivity(Intent(Settings.ACTION_INPUT_METHOD_SETTINGS).apply {
                    flags = Intent.FLAG_ACTIVITY_NEW_TASK
                })
            },
            onNext = { currentStep = 1 },
        )

        OnboardingStep(
            stepNumber = 2,
            title = "Tastatur auswaehlen",
            description = "Waehle SpeechKit als aktive Eingabemethode.",
            isCompleted = liveKeyboardSelected,
            isActive = currentStep == 1 && liveKeyboardEnabled,
            buttonText = "Tastatur waehlen",
            onAction = {
                val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
                imm.showInputMethodPicker()
            },
            onNext = { currentStep = 2 },
        )

        OnboardingStep(
            stepNumber = 3,
            title = "Voice Assistant (optional)",
            description = "Setze SpeechKit als Standard-Assistenten fuer systemweiten Sprachzugriff.",
            isCompleted = liveAssistantSet,
            isActive = currentStep == 2 && liveKeyboardSelected,
            buttonText = "Assistent einrichten",
            onAction = {
                context.startActivity(Intent(Settings.ACTION_VOICE_INPUT_SETTINGS).apply {
                    flags = Intent.FLAG_ACTIVITY_NEW_TASK
                })
            },
            onNext = { onComplete() },
            isOptional = true,
        )

        Spacer(modifier = Modifier.weight(1f))

        if (liveKeyboardEnabled && liveKeyboardSelected) {
            Button(
                onClick = onComplete,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("Fertig")
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
    onNext: () -> Unit,
    isOptional: Boolean = false,
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
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            text = title,
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.Medium,
                        )
                        if (isOptional) {
                            Text(
                                text = " (optional)",
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    Text(
                        text = description,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            if (isActive && !isCompleted) {
                Spacer(modifier = Modifier.height(12.dp))
                Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Button(onClick = onAction) {
                        Text(buttonText)
                    }
                    if (isOptional) {
                        OutlinedButton(onClick = onNext) {
                            Text("Ueberspringen")
                        }
                    }
                }
            }
        }
    }
}

/**
 * Checks if the SpeechKit keyboard (HeliBoard fork) is enabled and selected.
 *
 * Detects HeliBoard's LatinIME via InputMethodManager.
 */
object KeyboardSetupChecker {
    private const val IME_ID_SUFFIX = "helium314.keyboard.latin.LatinIME"

    fun isKeyboardEnabled(context: Context): Boolean {
        return try {
            val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
            imm.enabledInputMethodList.any { it.id.contains(IME_ID_SUFFIX) }
        } catch (e: Exception) {
            false
        }
    }

    fun isKeyboardSelected(context: Context): Boolean {
        return try {
            val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
            imm.enabledInputMethodList.any { it.id.contains(IME_ID_SUFFIX) }
        } catch (e: Exception) {
            false
        }
    }

    fun isAssistantSet(context: Context): Boolean {
        return try {
            val assistComponent = Settings.Secure.getString(
                context.contentResolver,
                "assistant",
            ) ?: return false
            assistComponent.contains("speechkit")
        } catch (e: Exception) {
            false
        }
    }
}
