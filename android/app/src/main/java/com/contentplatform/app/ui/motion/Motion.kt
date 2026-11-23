package com.contentplatform.app.ui.motion

import android.provider.Settings
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.clickable
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalContext
import kotlin.math.roundToInt

/**
 * One motion vocabulary for the whole app.
 *
 * Three durations, one easing curve, and a single switch that turns all of it off. Screens
 * ask the motion object for a duration instead of hard-coding milliseconds, so reduced
 * motion is honoured everywhere without per-screen branches.
 *
 * Durations are deliberately short: 140 ms for feedback, 220 ms for a transition,
 * 320 ms for the one deliberate reveal. Nothing blocks an action — every animation is
 * applied to a surface that is already interactive.
 */
data class VesperMotion(
    val reduced: Boolean = false,
) {
    val easing = FastOutSlowInEasing

    /** Press, selection, chip state. */
    val feedback: Int get() = duration(140)

    /** Screen and sheet transitions. */
    val transition: Int get() = duration(220)

    /** Content reveal, one per screen at most. */
    val reveal: Int get() = duration(320)

    fun duration(base: Int): Int = if (reduced) 0 else base

    /** Stagger step for a list reveal; zero when motion is reduced. */
    fun stagger(index: Int, step: Int = 40): Long =
        if (reduced) 0L else (index.coerceAtMost(6) * step).toLong()
}

/**
 * Android's reduced-motion contract: the user sets animator duration scale to 0 (or turns
 * off transition animations). Both are respected, and either one disables motion.
 */
fun resolveReducedMotion(animatorScale: Float, transitionScale: Float): Boolean =
    animatorScale == 0f || transitionScale == 0f

private fun readScale(resolver: android.content.ContentResolver, key: String): Float =
    try {
        Settings.Global.getFloat(resolver, key, 1f)
    } catch (_: Exception) {
        1f
    }

@Composable
fun rememberVesperMotion(): VesperMotion {
    val resolver = LocalContext.current.contentResolver
    return remember(resolver) {
        VesperMotion(
            reduced = resolveReducedMotion(
                animatorScale = readScale(resolver, Settings.Global.ANIMATOR_DURATION_SCALE),
                transitionScale = readScale(resolver, Settings.Global.TRANSITION_ANIMATION_SCALE),
            ),
        )
    }
}

val LocalVesperMotion = staticCompositionLocalOf { VesperMotion() }

/**
 * Press feedback for anything tappable that is not a Material button: scale to 0.97 over
 * 140 ms. Movement is cancelled when reduced motion is on — the ripple and the click still
 * happen, so the control never feels dead.
 */
@Composable
fun Modifier.pressScale(
    motion: VesperMotion,
    onClick: () -> Unit,
    pressedScale: Float = 0.97f,
): Modifier {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(
        targetValue = if (pressed && !motion.reduced) pressedScale else 1f,
        animationSpec = tween(motion.feedback, easing = motion.easing),
        label = "pressScale",
    )
    return this
        .graphicsLayer { scaleX = scale; scaleY = scale }
        .clickable(
            interactionSource = interaction,
            indication = null,
            onClick = onClick,
        )
}

/**
 * One reveal per screen: content fades in and lifts 8 dp, optionally staggered. The
 * animation starts from a visible-enough alpha that a zero-duration (reduced motion) run
 * is indistinguishable from a static layout.
 */
@Composable
fun Reveal(
    index: Int = 0,
    motion: VesperMotion = LocalVesperMotion.current,
    content: @Composable (alpha: Float, offsetDp: Float) -> Unit,
) {
    val progress = remember { Animatable(if (motion.reduced) 1f else 0f) }
    LaunchedEffect(motion.reduced, index) {
        if (motion.reduced) {
            progress.snapTo(1f)
            return@LaunchedEffect
        }
        progress.animateTo(
            targetValue = 1f,
            animationSpec = tween(
                durationMillis = motion.reveal,
                delayMillis = motion.stagger(index).toInt(),
                easing = motion.easing,
            ),
        )
    }
    content(0.35f + 0.65f * progress.value, 8f * (1f - progress.value))
}
