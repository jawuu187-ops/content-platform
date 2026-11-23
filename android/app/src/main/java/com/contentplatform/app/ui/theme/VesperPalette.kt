package com.contentplatform.app.ui.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow

/**
 * The product palette, in two designed states.
 *
 * Light is the default experience: warm near-white ground, bone chrome, oxblood as the
 * only action colour. Dark is not an inversion — it is the same room at night. The ground
 * keeps a brown undertone (#12100F) rather than going black, ink is bone-tinted (#F3EBE1)
 * rather than pure white, and the oxblood family splits in two: a deeper fill for buttons
 * ([action]) and a lifted tone for text and accents ([actionText]) so small type stays
 * readable on a dark ground.
 *
 * Every screen reads these roles, never a raw hex, so a new screen is theme-correct by
 * construction. Contrast is asserted in VesperPaletteTest.
 */
@Immutable
data class VesperPalette(
    /** Browsing ground: the surface most of a screen sits on. */
    val ground: Color,
    /** Chrome: bars, lists, billing rows. Bone in light, near-black in dark. */
    val chrome: Color,
    /** Cards and sheets. */
    val surface: Color,
    /** Raised surface for pressed/elevated states. */
    val surfaceRaised: Color,
    /** Letterbox behind media, and the viewer background. */
    val media: Color,
    /** Primary text. */
    val ink: Color,
    /** Secondary text and body copy. */
    val inkSecondary: Color,
    /** Metadata, captions, disabled labels. */
    val muted: Color,
    /** The only action colour: filled buttons, selected states, flags. */
    val action: Color,
    /** Text and icons on [action]. */
    val onAction: Color,
    /** Action colour used as type, where a fill would be too heavy. */
    val actionText: Color,
    /** Paid, active, confirmed. */
    val sage: Color,
    /** Pending, expiring, in review. */
    val brass: Color,
    /** Refused, declined, destructive. */
    val danger: Color,
    /** Hairline rules. */
    val rule: Color,
    /** Section rules and stronger separators. */
    val ruleStrong: Color,
    /** Sheet and dialog scrim. */
    val scrim: Color,
    val isDark: Boolean,
)

val VesperLight = VesperPalette(
    ground = Color(0xFFFBF8F3),
    chrome = Color(0xFFF4EFE7),
    surface = Color(0xFFFFFDFA),
    surfaceRaised = Color(0xFFFFFFFF),
    media = Color(0xFF141110),
    ink = Color(0xFF191412),
    inkSecondary = Color(0xFF3C322C),
    muted = Color(0xFF6F635A),
    action = Color(0xFF7A1F2B),
    onAction = Color(0xFFFBF8F3),
    actionText = Color(0xFF7A1F2B),
    sage = Color(0xFF2F6B4F),
    brass = Color(0xFF8C6A2A),
    danger = Color(0xFFA8322A),
    rule = Color(0x24191412),
    ruleStrong = Color(0x4D191412),
    scrim = Color(0xCC0D0B0A),
    isDark = false,
)

val VesperDark = VesperPalette(
    ground = Color(0xFF12100F),
    chrome = Color(0xFF0A0908),
    surface = Color(0xFF221E1C),
    surfaceRaised = Color(0xFF2A2522),
    media = Color(0xFF0A0807),
    ink = Color(0xFFF3EBE1),
    inkSecondary = Color(0xFFD9CDC2),
    muted = Color(0xFFA2938A),
    action = Color(0xFF8E2A37),
    onAction = Color(0xFFF6EFE6),
    actionText = Color(0xFFD06B76),
    sage = Color(0xFF6FBF95),
    brass = Color(0xFFD3A85C),
    danger = Color(0xFFE2705F),
    rule = Color(0x2EF3EBE1),
    ruleStrong = Color(0x5CF3EBE1),
    scrim = Color(0xE60A0807),
    isDark = true,
)

/** WCAG relative luminance of an sRGB colour. */
fun relativeLuminance(color: Color): Double {
    fun channel(value: Float): Double {
        val c = value.toDouble()
        return if (c <= 0.04045) c / 12.92 else ((c + 0.055) / 1.055).pow(2.4)
    }
    return 0.2126 * channel(color.red) +
        0.7152 * channel(color.green) +
        0.0722 * channel(color.blue)
}

/** WCAG contrast ratio between two opaque colours; 1.0 (identical) to 21.0 (black/white). */
fun contrastRatio(foreground: Color, background: Color): Double {
    val a = relativeLuminance(foreground)
    val b = relativeLuminance(background)
    return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}
