package com.contentplatform.app.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import com.contentplatform.app.R

/**
 * Three families, seven steps. Nothing else is allowed.
 *
 * Bodoni Moda carries names and decision prices, exactly as the board does. Archivo is
 * everything a thumb touches and all running copy. IBM Plex Mono is money, dates, ids and
 * ledger rows, set tabular so columns align without effort.
 *
 * Fonts are bundled (SIL OFL, ~570 KB total) so the first frame is correct offline and no
 * network font provider is needed. Licences: android/app/src/main/assets/OFL.txt.
 */
val VesperDidone = FontFamily(
    Font(R.font.bodoni_regular, FontWeight.Normal),
    Font(R.font.bodoni_medium, FontWeight.Medium),
)

val VesperGrotesk = FontFamily(
    Font(R.font.archivo_regular, FontWeight.Normal),
    Font(R.font.archivo_semibold, FontWeight.SemiBold),
)

val VesperMono = FontFamily(
    Font(R.font.plexmono_regular, FontWeight.Normal),
    Font(R.font.plexmono_medium, FontWeight.Medium),
)

// 40 / 28 / 21 / 16 / 14 / 12 / 11 — the audit holds the board to the same seven.
val VesperTypography = Typography(
    displayLarge = TextStyle(
        fontFamily = VesperDidone,
        fontWeight = FontWeight.Normal,
        fontSize = 40.sp,
        lineHeight = 44.sp,
        letterSpacing = 0.sp,
    ),
    displayMedium = TextStyle(
        fontFamily = VesperDidone,
        fontWeight = FontWeight.Normal,
        fontSize = 28.sp,
        lineHeight = 32.sp,
        letterSpacing = 0.sp,
    ),
    headlineSmall = TextStyle(
        fontFamily = VesperDidone,
        fontWeight = FontWeight.Medium,
        fontSize = 21.sp,
        lineHeight = 24.sp,
        letterSpacing = 0.sp,
    ),
    titleMedium = TextStyle(
        fontFamily = VesperGrotesk,
        fontWeight = FontWeight.SemiBold,
        fontSize = 16.sp,
        lineHeight = 22.sp,
        letterSpacing = 0.sp,
    ),
    bodyMedium = TextStyle(
        fontFamily = VesperGrotesk,
        fontWeight = FontWeight.Normal,
        fontSize = 14.sp,
        lineHeight = 22.sp,
        letterSpacing = 0.sp,
    ),
    bodySmall = TextStyle(
        fontFamily = VesperGrotesk,
        fontWeight = FontWeight.Normal,
        fontSize = 12.sp,
        lineHeight = 18.sp,
        letterSpacing = 0.sp,
    ),
    labelSmall = TextStyle(
        fontFamily = VesperMono,
        fontWeight = FontWeight.Normal,
        fontSize = 11.sp,
        lineHeight = 16.sp,
        letterSpacing = 0.22.sp,
    ),
    labelMedium = TextStyle(
        fontFamily = VesperMono,
        fontWeight = FontWeight.Medium,
        fontSize = 11.sp,
        lineHeight = 16.sp,
        letterSpacing = 0.22.sp,
    ),
)

/** Money and ids: mono, tabular, same size as body copy. */
val MoneyStyle = TextStyle(
    fontFamily = VesperMono,
    fontWeight = FontWeight.Medium,
    fontSize = 14.sp,
    lineHeight = 20.sp,
    fontFeatureSettings = "tnum",
)
