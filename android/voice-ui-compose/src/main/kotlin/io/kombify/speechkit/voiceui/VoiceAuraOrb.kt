package io.kombify.speechkit.voiceui

import android.provider.Settings
import androidx.annotation.DrawableRes
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.ColorMatrix
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp

/**
 * The assistant orb — the Compose implementation of the canonical Voice
 * Assistant visual (`speechkit-voice-assistant`, "Aura Orb").
 *
 * The normative specification is the `assistant` block of the Voice UI Kit's
 * `tokens.json` (clients/typescript/packages/voice-ui/src/tokens): the layer
 * stack, the per-state colour pairs, the animation periods, and the level
 * formulas below are all taken from there. Keep this file and the web element
 * in lockstep — a change to one without the other is visual drift, and the
 * token file is the arbiter.
 *
 * The mark is part of the motion system rather than a sticker: it desaturates
 * to a grey ghost while the assistant rests and returns to full colour with a
 * level-driven scale once the session is live (branding decision 2026-08-10:
 * rosette standard, k monogram and bare orb as the alternatives).
 */
enum class VoiceAuraState {
    INACTIVE,
    CONNECTING,
    LISTENING,
    PROCESSING,
    SPEAKING,
    RECOVERING,
    SETTLING,
    ERROR,
}

private data class AuraPalette(val lead: Color, val trail: Color, val alive: Boolean)

// Colour pairs mirror tokens.json `shared.aura-<state>-a/-b`; `alive=false`
// states rest (no animation, thinned effects, ghosted mark).
private fun paletteFor(state: VoiceAuraState): AuraPalette = when (state) {
    VoiceAuraState.INACTIVE -> AuraPalette(Color(0xFF9CA3B8), Color(0xFF64748B), alive = false)
    VoiceAuraState.CONNECTING -> AuraPalette(Color(0xFF38BDF8), Color(0xFF818CF8), alive = true)
    VoiceAuraState.LISTENING -> AuraPalette(Color(0xFF34D399), Color(0xFF38BDF8), alive = true)
    VoiceAuraState.PROCESSING -> AuraPalette(Color(0xFFFBBF24), Color(0xFFF472B6), alive = true)
    VoiceAuraState.SPEAKING -> AuraPalette(Color(0xFF818CF8), Color(0xFFA78BFA), alive = true)
    VoiceAuraState.RECOVERING -> AuraPalette(Color(0xFF22D3EE), Color(0xFF818CF8), alive = true)
    VoiceAuraState.SETTLING -> AuraPalette(Color(0xFF34D399), Color(0xFF818CF8), alive = true)
    VoiceAuraState.ERROR -> AuraPalette(Color(0xFFF87171), Color(0xFFFB7185), alive = false)
}

// Layer insets from tokens.json expressed as Compose scale factors
// (inset n% => scale 1 - 2n).
private const val SWEEP_SCALE = 0.88f
private const val INNER_SWEEP_SCALE = 0.68f
private const val HALO_SCALE = 0.80f
private const val CORE_SCALE = 0.48f
private const val SPARK_SCALE = 0.12f
private const val MARK_SCALE = 0.34f

/** True when the user asked the platform to suppress animation. */
@Composable
private fun animationsSuppressed(): Boolean {
    val resolver = LocalContext.current.contentResolver
    return remember(resolver) {
        Settings.Global.getFloat(resolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
    }
}

@Composable
fun VoiceAuraOrb(
    state: VoiceAuraState,
    modifier: Modifier = Modifier,
    level: Float = 0f,
    sizeDp: Int = 96,
    reduceMotion: Boolean = animationsSuppressed(),
    /**
     * Optional brand mark drawable for the orb centre. Null renders the pure
     * orb. This module ships no brand asset on purpose, exactly like the
     * published web kit: the host supplies its own, so the shared visual can
     * be reused without carrying one product's branding into every surface.
     */
    @DrawableRes markRes: Int? = null,
) {
    val palette = paletteFor(state)
    val clamped = level.coerceIn(0f, 1f)
    val animated = palette.alive && !reduceMotion
    val transition = rememberInfiniteTransition(label = "aura")

    // Slow rotation for the aurora sweep; the inner sweep counter-rotates on a
    // different period so the two never lock into a visible beat.
    val sweep by transition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(9000, easing = LinearEasing)),
        label = "sweep",
    )
    val innerSweep by transition.animateFloat(
        initialValue = 360f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(tween(13000, easing = LinearEasing)),
        label = "inner_sweep",
    )
    val breathe by transition.animateFloat(
        initialValue = 0.92f,
        targetValue = 1.06f,
        animationSpec = infiniteRepeatable(tween(4500), repeatMode = RepeatMode.Reverse),
        label = "breathe",
    )
    val glowAlpha by transition.animateFloat(
        initialValue = 0.55f,
        targetValue = 0.9f,
        animationSpec = infiniteRepeatable(tween(4500), repeatMode = RepeatMode.Reverse),
        label = "glow_alpha",
    )
    val coreAlpha by transition.animateFloat(
        initialValue = 0.78f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(3200), repeatMode = RepeatMode.Reverse),
        label = "core_alpha",
    )

    val haloScale by animateFloatAsState(
        targetValue = if (animated) 0.82f + clamped * 0.3f else 0.82f,
        animationSpec = tween(120),
        label = "halo",
    )
    val markScale by animateFloatAsState(
        targetValue = if (animated) 1f + clamped * 0.08f else 1f,
        animationSpec = tween(420),
        label = "mark_scale",
    )
    val markAlpha by animateFloatAsState(
        targetValue = if (palette.alive) 1f else 0.5f,
        animationSpec = tween(420),
        label = "mark_alpha",
    )

    Box(
        modifier = modifier.size(sizeDp.dp),
        contentAlignment = Alignment.Center,
    ) {
        // Breathing outer glow.
        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .scale(if (animated) breathe else 1f),
        ) {
            val radius = size.minDimension / 2f
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        palette.lead.copy(alpha = 0.38f),
                        palette.lead.copy(alpha = 0.08f),
                        Color.Transparent,
                    ),
                    radius = radius,
                ),
                radius = radius,
                alpha = if (animated) glowAlpha else 0.55f,
            )
        }

        // Rotating aurora sweeps. Modifier.blur is a no-op below API 31; the
        // sweeps then read sharper but keep their colour and motion.
        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .scale(SWEEP_SCALE)
                .rotate(if (animated) sweep else 0f)
                .blur(6.dp),
        ) {
            drawCircle(
                brush = Brush.sweepGradient(
                    listOf(
                        Color.Transparent,
                        palette.lead.copy(alpha = 0.55f),
                        Color.Transparent,
                        palette.trail.copy(alpha = 0.45f),
                        Color.Transparent,
                    ),
                ),
                radius = size.minDimension / 2f,
                alpha = 0.9f,
            )
        }
        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .scale(INNER_SWEEP_SCALE)
                .rotate(if (animated) innerSweep else 0f)
                .blur(4.dp),
        ) {
            drawCircle(
                brush = Brush.sweepGradient(
                    listOf(
                        Color.Transparent,
                        palette.trail.copy(alpha = 0.40f),
                        Color.Transparent,
                    ),
                ),
                radius = size.minDimension / 2f,
                alpha = 0.8f,
            )
        }

        // Level-reactive halo ring.
        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .scale(HALO_SCALE * haloScale),
        ) {
            drawCircle(
                color = palette.lead.copy(
                    alpha = if (palette.alive) 0.35f + clamped * 0.5f else 0.18f,
                ),
                radius = size.minDimension / 2f,
                style = Stroke(width = 2f + clamped * 3f),
            )
        }

        // Glassy translucent core.
        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .scale(CORE_SCALE),
        ) {
            val radius = size.minDimension / 2f
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        Color.White.copy(alpha = 0.30f),
                        Color.White.copy(alpha = 0.06f),
                        Color.White.copy(alpha = 0.02f),
                    ),
                    radius = radius,
                ),
                radius = radius,
                alpha = if (animated) coreAlpha else 0.78f,
            )
            drawCircle(
                color = Color.White.copy(alpha = 0.14f),
                radius = radius,
                style = Stroke(width = 1f),
            )
        }

        // Centre spark — backlight for the brand mark.
        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .scale(SPARK_SCALE),
        ) {
            val radius = size.minDimension / 2f
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(
                        palette.lead.copy(alpha = 0.95f),
                        palette.lead.copy(alpha = 0.20f),
                        Color.Transparent,
                    ),
                    radius = radius,
                ),
                radius = radius,
                alpha = if (palette.alive) 0.85f else 0.4f,
            )
        }

        // Brand mark, when the host supplied one. Greyscale while resting,
        // full colour once alive.
        if (markRes != null) {
            Image(
                painter = painterResource(id = markRes),
                contentDescription = null,
                contentScale = ContentScale.Fit,
                modifier = Modifier
                    .fillMaxSize()
                    .scale(MARK_SCALE * markScale),
                alpha = markAlpha,
                colorFilter = if (palette.alive) {
                    null
                } else {
                    ColorFilter.colorMatrix(ColorMatrix().apply { setToSaturation(0f) })
                },
            )
        }
    }
}
