package com.contentplatform.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.unit.dp
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.rememberVesperMotion

/** The palette every screen reads. Default is light, matching the approved board. */
val LocalVesperPalette = staticCompositionLocalOf { VesperLight }

/** 14 dp cards, 8 dp actions, 20 dp sheets. Media stays square-cornered inside its parent. */
val VesperShapes = Shapes(
    extraSmall = RoundedCornerShape(6.dp),
    small = RoundedCornerShape(8.dp),
    medium = RoundedCornerShape(14.dp),
    large = RoundedCornerShape(20.dp),
    extraLarge = RoundedCornerShape(28.dp),
)

private fun VesperPalette.toColorScheme() = if (isDark) {
    darkColorScheme(
        primary = action,
        onPrimary = onAction,
        primaryContainer = action,
        onPrimaryContainer = onAction,
        secondary = sage,
        onSecondary = media,
        tertiary = brass,
        onTertiary = media,
        background = ground,
        onBackground = ink,
        surface = surface,
        onSurface = ink,
        surfaceVariant = chrome,
        onSurfaceVariant = inkSecondary,
        surfaceContainer = chrome,
        outline = ruleStrong,
        outlineVariant = rule,
        error = danger,
        onError = media,
        scrim = scrim,
    )
} else {
    lightColorScheme(
        primary = action,
        onPrimary = onAction,
        primaryContainer = chrome,
        onPrimaryContainer = ink,
        secondary = sage,
        onSecondary = onAction,
        tertiary = brass,
        onTertiary = onAction,
        background = ground,
        onBackground = ink,
        surface = surface,
        onSurface = ink,
        surfaceVariant = chrome,
        onSurfaceVariant = muted,
        surfaceContainer = chrome,
        outline = ruleStrong,
        outlineVariant = rule,
        error = danger,
        onError = onAction,
        scrim = scrim,
    )
}

/**
 * Applies the palette, type scale, shapes and motion vocabulary.
 *
 * [mode] is the stored preference; [System] resolves with the device. Switching mode
 * recomposes in place — the caller keeps its navigation state and scroll positions, so a
 * theme change never restarts a screen.
 */
@Composable
fun VesperTheme(
    mode: ThemeMode = ThemeMode.System,
    content: @Composable () -> Unit,
) {
    val dark = when (mode) {
        ThemeMode.System -> isSystemInDarkTheme()
        ThemeMode.Light -> false
        ThemeMode.Dark -> true
    }
    val palette = if (dark) VesperDark else VesperLight
    val motion = rememberVesperMotion()

    CompositionLocalProvider(
        LocalVesperPalette provides palette,
        LocalVesperMotion provides motion,
    ) {
        MaterialTheme(
            colorScheme = palette.toColorScheme(),
            typography = VesperTypography,
            shapes = VesperShapes,
            content = content,
        )
    }
}
