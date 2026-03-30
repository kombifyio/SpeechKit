package io.kombify.speechkit.app.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

// kombify brand colors
private val KombifyPrimary = Color(0xFF1A73E8)
private val KombifyOnPrimary = Color(0xFFFFFFFF)
private val KombifyPrimaryContainer = Color(0xFFD3E3FD)
private val KombifyOnPrimaryContainer = Color(0xFF001D36)
private val KombifySecondary = Color(0xFF565F71)
private val KombifySecondaryContainer = Color(0xFFDAE2F9)
private val KombifyTertiary = Color(0xFF705575)
private val KombifyTertiaryContainer = Color(0xFFFAD8FD)
private val KombifyError = Color(0xFFBA1A1A)
private val KombifySurface = Color(0xFFFAFAFD)

private val LightColorScheme = lightColorScheme(
    primary = KombifyPrimary,
    onPrimary = KombifyOnPrimary,
    primaryContainer = KombifyPrimaryContainer,
    onPrimaryContainer = KombifyOnPrimaryContainer,
    secondary = KombifySecondary,
    secondaryContainer = KombifySecondaryContainer,
    tertiary = KombifyTertiary,
    tertiaryContainer = KombifyTertiaryContainer,
    error = KombifyError,
    surface = KombifySurface,
)

private val DarkColorScheme = darkColorScheme(
    primary = Color(0xFFA8C7FA),
    onPrimary = Color(0xFF002E6A),
    primaryContainer = Color(0xFF004494),
    onPrimaryContainer = KombifyPrimaryContainer,
    secondary = Color(0xFFBEC6DC),
    secondaryContainer = Color(0xFF3E4759),
    tertiary = Color(0xFFDDBCE0),
    tertiaryContainer = Color(0xFF573E5C),
    error = Color(0xFFFFB4AB),
    surface = Color(0xFF1A1B1E),
)

@Composable
fun SpeechKitTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit,
) {
    val colorScheme = when {
        // Use dynamic color on Android 12+ if available
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }
        darkTheme -> DarkColorScheme
        else -> LightColorScheme
    }

    MaterialTheme(
        colorScheme = colorScheme,
        typography = MaterialTheme.typography,
        content = content,
    )
}
