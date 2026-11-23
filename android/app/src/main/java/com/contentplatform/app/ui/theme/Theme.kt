package com.contentplatform.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable

private val DarkScheme = darkColorScheme(
    primary = Plum300,
    onPrimary = Ink900,
    primaryContainer = Plum500,
    onPrimaryContainer = Mist100,
    secondary = Amber500,
    onSecondary = Ink900,
    background = Ink900,
    onBackground = Mist100,
    surface = Ink800,
    onSurface = Mist100,
    surfaceVariant = Ink700,
    onSurfaceVariant = Mist200,
    error = Danger600,
    onError = Mist100,
)

private val LightScheme = lightColorScheme(
    primary = Plum500,
    onPrimary = Mist100,
    primaryContainer = Mist200,
    onPrimaryContainer = Ink900,
    secondary = Amber500,
    onSecondary = Ink900,
    background = Mist100,
    onBackground = Ink900,
    surface = Mist100,
    onSurface = Ink900,
    surfaceVariant = Mist200,
    onSurfaceVariant = Mist700,
    error = Danger600,
    onError = Mist100,
)

/**
 * Applies the platform theme. Dark is the default because the majority of
 * viewing happens at night, but the light scheme is a first-class citizen and
 * follows the system setting.
 */
@Composable
fun ContentPlatformTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = if (darkTheme) DarkScheme else LightScheme,
        typography = PlatformTypography,
        content = content,
    )
}
