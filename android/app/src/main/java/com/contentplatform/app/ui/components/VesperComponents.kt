package com.contentplatform.app.ui.components

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.VesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import com.contentplatform.app.ui.theme.MoneyStyle
import com.contentplatform.app.ui.theme.VesperMono

/**
 * The shared component set. Six pieces, every screen is built from these:
 *
 *  - [Chrome]      a themed bar (ground + rule + title)
 *  - [Chip]        status, price and counter marks, never a button
 *  - [ActionButton] the only filled action, with the price inside it
 *  - [MediaPanel]  the media frame, with an honest placeholder until the pipeline lands
 *  - [CardSurface] the 14 dp card every list item uses
 *  - [Monogram]    the avatar fallback while media is not loaded
 *
 * Nothing here hard-codes a colour; both themes are read from [LocalVesperPalette].
 */

enum class ChipTone { Neutral, Action, Paid, Pending, Danger }

@Composable
fun Chrome(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    trailing: (@Composable () -> Unit)? = null,
) {
    val palette = LocalVesperPalette.current
    Column(modifier.fillMaxWidth().background(palette.chrome)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(56.dp)
                .padding(horizontal = 20.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.titleMedium, color = palette.ink)
                if (subtitle != null) {
                    Text(
                        subtitle,
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )
                }
            }
            trailing?.invoke()
        }
        Box(Modifier.fillMaxWidth().height(1.dp).background(palette.rule))
    }
}

@Composable
fun Chip(
    text: String,
    tone: ChipTone = ChipTone.Neutral,
    modifier: Modifier = Modifier,
) {
    val palette = LocalVesperPalette.current
    val (container, content) = when (tone) {
        ChipTone.Neutral -> palette.surfaceRaised to palette.inkSecondary
        ChipTone.Action -> palette.action to palette.onAction
        ChipTone.Paid -> palette.sage.copy(alpha = 0.16f) to palette.sage
        ChipTone.Pending -> palette.brass.copy(alpha = 0.16f) to palette.brass
        ChipTone.Danger -> palette.danger.copy(alpha = 0.16f) to palette.danger
    }
    Box(
        modifier
            .clip(RoundedCornerShape(50))
            .background(container)
            .border(1.dp, palette.rule, RoundedCornerShape(50))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.labelSmall,
            color = content,
            maxLines = 1,
        )
    }
}

/** The only filled action. The price lives inside it, as the board requires. */
@Composable
fun ActionButton(
    label: String,
    price: String?,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val container = if (enabled) palette.action else palette.action.copy(alpha = 0.4f)
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(52.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(container)
            .pressScale(motion, onClick = { if (enabled) onClick() })
            .semantics { contentDescription = label },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(
            label,
            modifier = Modifier.padding(start = 16.dp),
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.SemiBold,
            color = palette.onAction,
        )
        if (price != null) {
            Text(
                price,
                modifier = Modifier.padding(end = 16.dp),
                style = MoneyStyle,
                color = palette.onAction.copy(alpha = 0.92f),
            )
        }
    }
}

@Composable
fun OutlineButton(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Box(
        modifier
            .height(52.dp)
            .clip(RoundedCornerShape(8.dp))
            .border(1.dp, palette.ruleStrong, RoundedCornerShape(8.dp))
            .pressScale(motion, onClick = onClick)
            .padding(horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = palette.ink)
    }
}

@Composable
fun CardSurface(
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val palette = LocalVesperPalette.current
    Box(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(palette.surface)
            .border(1.dp, palette.rule, RoundedCornerShape(14.dp)),
    ) { content() }
}

/**
 * Media frame. The image pipeline is not implemented, so this draws a graded placeholder
 * in the media ink rather than a fake photograph — a reviewer must never mistake it for
 * real content. Chips sit on the frame exactly where they will sit on the real thing.
 */
@Composable
fun MediaPanel(
    height: Int,
    modifier: Modifier = Modifier,
    label: String? = null,
    price: String? = null,
    locked: Boolean = false,
    corner: Int = 14,
) {
    val palette = LocalVesperPalette.current
    val shape = RoundedCornerShape(corner.dp)
    Box(
        modifier
            .fillMaxWidth()
            .height(height.dp)
            .clip(shape)
            .background(
                Brush.verticalGradient(
                    listOf(
                        palette.media,
                        if (palette.isDark) palette.surfaceRaised else palette.chrome,
                    ),
                ),
            ),
    ) {
        if (locked) {
            Box(
                Modifier
                    .fillMaxSize()
                    .background(palette.media.copy(alpha = 0.55f)),
            )
        }
        if (label != null) {
            Chip(label, ChipTone.Neutral, Modifier.align(Alignment.BottomStart).padding(8.dp))
        }
        if (price != null) {
            Chip(price, ChipTone.Action, Modifier.align(Alignment.TopEnd).padding(8.dp))
        }
    }
}

@Composable
fun Monogram(initials: String, size: Int = 40) {
    val palette = LocalVesperPalette.current
    Box(
        Modifier
            .size(size.dp)
            .clip(CircleShape)
            .background(if (palette.isDark) palette.surfaceRaised else palette.chrome)
            .border(1.dp, palette.rule, CircleShape),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            initials,
            style = MaterialTheme.typography.bodySmall,
            color = palette.inkSecondary,
        )
    }
}

@Composable
fun Money(text: String, modifier: Modifier = Modifier, color: Color? = null) {
    val palette = LocalVesperPalette.current
    Text(
        text,
        modifier = modifier,
        style = MoneyStyle,
        color = color ?: palette.ink,
        maxLines = 1,
        overflow = TextOverflow.Clip,
    )
}

@Composable
fun LabelMono(text: String, modifier: Modifier = Modifier, color: Color? = null) {
    val palette = LocalVesperPalette.current
    Text(
        text,
        modifier = modifier,
        style = MaterialTheme.typography.labelSmall.copy(fontFamily = VesperMono),
        color = color ?: palette.muted,
    )
}

@Composable
fun Divider(modifier: Modifier = Modifier, strong: Boolean = false) {
    val palette = LocalVesperPalette.current
    Box(modifier.fillMaxWidth().height(1.dp).background(if (strong) palette.ruleStrong else palette.rule))
}

@Composable
fun SpacerH(dp: Int) = Spacer(Modifier.height(dp.dp))

@Composable
fun SpacerW(dp: Int) = Spacer(Modifier.width(dp.dp))

/** Fade used by every overlay scrim, so sheets open with the same weight. */
@Composable
fun rememberScrimAlpha(visible: Boolean): Float {
    val motion = LocalVesperMotion.current
    val alpha by animateFloatAsState(
        targetValue = if (visible) 1f else 0f,
        animationSpec = tween(motion.transition, easing = motion.easing),
        label = "scrim",
    )
    return alpha
}

@Composable
fun Scrim(alpha: Float, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion: VesperMotion = LocalVesperMotion.current
    Box(
        Modifier
            .fillMaxSize()
            .background(palette.scrim.copy(alpha = palette.scrim.alpha * alpha))
            .pressScale(motion, onClick = onClick),
    )
}

@Composable
fun SectionTitle(text: String, meta: String? = null) {
    val palette = LocalVesperPalette.current
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 20.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = MaterialTheme.typography.titleMedium, color = palette.ink)
        Spacer(Modifier.weight(1f))
        if (meta != null) LabelMono(meta)
    }
}

@Composable
fun KeyedColor(alpha: Float): Modifier = Modifier.graphicsLayer { this.alpha = alpha }
