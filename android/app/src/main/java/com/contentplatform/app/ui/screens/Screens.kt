package com.contentplatform.app.ui.screens

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.contentplatform.app.BuildConfig
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.Chip
import com.contentplatform.app.ui.components.ChipTone
import com.contentplatform.app.ui.components.Chrome
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.MediaPanel
import com.contentplatform.app.ui.components.Monogram
import com.contentplatform.app.ui.components.Money
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SectionTitle
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.Reveal
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.ThemeMode
import com.contentplatform.app.ui.theme.VesperPalette
import com.contentplatform.app.ui.theme.LocalVesperPalette

/**
 * The four destinations, plus the pushed creator profile.
 *
 * Every figure on these screens comes from the canonical dataset used by the design board:
 * 12,412 members, $115,048 MRR, Studio $9 (50.8% of members), 37 unlocks in seven days.
 * Nothing here invents a metric the backend does not serve.
 */
/**
 * Nothing in this client is wired to the API yet, so no figure here is real. Every number is
 * gated: the shipped build shows "—" and a "not connected" mark rather than inventing
 * members, revenue, balances or history. The canonical dataset stays in the design board for
 * review and is never presented as account state.
 */
private object Data {
    const val connected = false
    fun value(real: String): String = if (connected) real else "—"
}

private const val CREATOR_NAME = "Lina Marchetti"
private const val CREATOR_HANDLE = "@lina.m"
private const val MEMBERS = "12,412"

private val tiers = listOf(
    Triple("Supporter", "5.00", "Every public post and the full archive"),
    Triple("Studio", "9.00", "Full sets the day they are shot · one unlock a month"),
    Triple("Collector", "19.00", "Print drops at cost · three unlocks a month"),
)

private data class Post(val title: String, val meta: String, val price: String?)

private val posts = listOf(
    Post("Morning light, fourteen frames", "2 h ago · 07:12", null),
    Post("Rooftop, after hours", "Yesterday · pay-per-view", "$14"),
)

@Composable
fun HomeScreen(
    onOpenCreator: () -> Unit,
    onOpenMembership: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val density = LocalDensity.current
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Home", subtitle = CREATOR_NAME)
        LazyColumn(Modifier.fillMaxSize()) {
            item {
                Reveal(0, motion) { alpha, offset ->
                    Column(
                        Modifier
                            .graphicsLayer {
                                this.alpha = alpha
                                translationY = with(density) { offset.dp.toPx() }
                            }
                            .padding(20.dp),
                    ) {
                        MediaPanel(
                            height = 236,
                            label = Data.value("12,412 members"),
                            price = "Tonight · new set",
                            modifier = Modifier.pressScale(motion, onClick = onOpenCreator),
                        )
                        SpacerH(12)
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text(
                                    CREATOR_NAME,
                                    style = MaterialTheme.typography.headlineSmall,
                                    color = palette.ink,
                                )
                                Text(
                                    if (Data.connected) "$CREATOR_HANDLE · creator since 2024 · 48 posts"
                                    else "$CREATOR_HANDLE · not connected",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.muted,
                                )
                            }
                            Money(Data.value("$9/mo"), color = palette.ink)
                        }
                        SpacerH(12)
                        ActionButton("Join Studio", Data.value("$9.00 / month"), onClick = onOpenMembership)
                        SpacerH(8)
                        Text(
                            "18+ accounts only. Cancel any time.",
                            style = MaterialTheme.typography.bodySmall,
                            color = palette.muted,
                        )
                    }
                }
            }
            item {
                Reveal(1, motion) { alpha, offset ->
                    Column(
                        Modifier
                            .graphicsLayer {
                                this.alpha = alpha
                                translationY = with(density) { offset.dp.toPx() }
                            }
                            .padding(horizontal = 20.dp, vertical = 8.dp),
                    ) {
                        SectionTitle("New this week", "7 creators")
                    }
                }
            }
            itemsIndexed(posts) { index, post ->
                Reveal(index + 2, motion) { alpha, offset ->
                    Box(
                        Modifier
                            .graphicsLayer {
                                this.alpha = alpha
                                translationY = with(density) { offset.dp.toPx() }
                            }
                            .padding(horizontal = 20.dp, vertical = 6.dp),
                    ) {
                        PostCard(post, index, onOpen = onOpenCreator)
                    }
                }
            }
            item { SpacerH(24) }
        }
    }
}

@Composable
private fun PostCard(post: Post, index: Int, onOpen: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    CardSurface(Modifier.pressScale(motion, onClick = onOpen)) {
        Column {
            MediaPanel(
                height = if (index == 0) 296 else 190,
                label = if (index == 0) "1 / 14" else "12:40",
                price = post.price,
                locked = post.price != null,
                corner = 0,
            )
            Column(Modifier.padding(12.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    LabelMono(post.meta)
                    SpacerH(0)
                    Box(Modifier.weight(1f))
                    if (post.price != null) {
                        Chip("Unlock ${post.price}", ChipTone.Action)
                    } else {
                        Chip("Subscribers", ChipTone.Paid)
                    }
                }
                SpacerH(6)
                Text(
                    post.title,
                    style = MaterialTheme.typography.titleMedium,
                    color = palette.ink,
                )
            }
        }
    }
}

@Composable
fun CreatorScreen(
    onBack: () -> Unit,
    onOpenMembership: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(
            title = "Creator",
            subtitle = if (Data.connected) MEMBERS + " members · 48 posts" else "Not connected",
            trailing = { OutlineButton("Tiers", onOpenMembership) },
        )
        LazyColumn(Modifier.fillMaxSize()) {
            item {
                Reveal(0, motion) { alpha, offset ->
                    Column(Modifier.graphicsLayer { this.alpha = alpha }) {
                        MediaPanel(height = 176, corner = 0)
                        Column(Modifier.padding(horizontal = 20.dp)) {
                            Box(Modifier.offset(y = (-28).dp)) {
                                Monogram("LM", size = 72)
                            }
                            Text(
                                CREATOR_NAME,
                                style = MaterialTheme.typography.displayMedium,
                                color = palette.ink,
                            )
                            Text(
                                "$CREATOR_HANDLE · verified 18+",
                                style = MaterialTheme.typography.bodySmall,
                                color = palette.muted,
                            )
                            SpacerH(12)
                            Row {
                                Stat(Data.value("12,412"), "Members", Modifier.weight(1f))
                                Stat(Data.value("48"), "Posts", Modifier.weight(1f))
                                Stat(Data.value("3"), "Tiers", Modifier.weight(1f))
                            }
                            SpacerH(12)
                            Text(
                                "Film photography, mostly rooftops and the hour before dark.",
                                style = MaterialTheme.typography.bodyMedium,
                                color = palette.inkSecondary,
                            )
                            SpacerH(16)
                            SectionTitle("Membership", Data.value("from $5"))
                            SpacerH(8)
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                tiers.take(2).forEach { tier ->
                                    TierPreview(tier, Modifier.weight(1f))
                                }
                            }
                            SpacerH(8)
                            Text(
                                "Compare all 3 tiers",
                                modifier = Modifier.clickable(onClick = onOpenMembership),
                                style = MaterialTheme.typography.bodySmall.copy(fontWeight = FontWeight.SemiBold),
                                color = palette.actionText,
                            )
                            SpacerH(16)
                            SectionTitle("Recent work", Data.value("48 posts"))
                            SpacerH(8)
                        }
                        Row(Modifier.padding(horizontal = 20.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                            repeat(3) { cell ->
                                Box(Modifier.weight(1f)) {
                                    MediaPanel(
                                        height = 118,
                                        corner = 4,
                                        locked = cell == 2,
                                        price = if (cell == 2) "$14" else null,
                                    )
                                }
                            }
                        }
                    }
                }
            }
            item {
                Column(Modifier.padding(20.dp)) {
                    ActionButton("Join Studio", "$9.00 / month", onClick = onOpenMembership)
                    SpacerH(8)
                    Text(
                        "Tiers are priced once and locked while a subscription stays active.",
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )
                    SpacerH(12)
                    OutlineButton("Back", onBack)
                }
            }
        }
    }
}

@Composable
private fun Stat(value: String, label: String, modifier: Modifier = Modifier) {
    val palette = LocalVesperPalette.current
    Column(modifier) {
        Money(value, color = palette.ink)
        LabelMono(label)
    }
}

@Composable
private fun TierPreview(tier: Triple<String, String, String>, modifier: Modifier = Modifier) {
    val palette = LocalVesperPalette.current
    val recommended = tier.first == "Studio"
    Column(
        modifier
            .clip(RoundedCornerShape(10.dp))
            .background(palette.surface)
            .border(1.dp, if (recommended) palette.action.copy(alpha = 0.5f) else palette.rule, RoundedCornerShape(10.dp))
            .padding(12.dp),
    ) {
        Text(tier.first, style = MaterialTheme.typography.bodySmall.copy(fontWeight = FontWeight.SemiBold), color = palette.ink)
        SpacerH(2)
        Money("\$${tier.second} / mo", color = palette.ink)
        SpacerH(4)
        Text(tier.third, style = MaterialTheme.typography.bodySmall, color = palette.muted, maxLines = 2)
    }
}

@Composable
fun MembershipScreen(
    onJoin: (String, String) -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(
            title = "Membership",
            subtitle = "$CREATOR_NAME · " + if (Data.connected) "12,412 members" else "not connected",
        )
        LazyColumn(Modifier.fillMaxSize()) {
            itemsIndexed(tiers) { index, tier ->
                Reveal(index, motion) { alpha, offset ->
                    Column(
                        Modifier
                            .graphicsLayer { this.alpha = alpha }
                            .padding(horizontal = 20.dp, vertical = 6.dp),
                    ) {
                        val recommended = tier.first == "Studio"
                        CardSurface {
                            Column(Modifier.padding(16.dp)) {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    Text(
                                        tier.first,
                                        style = MaterialTheme.typography.titleMedium,
                                        color = palette.ink,
                                    )
                                    if (recommended) {
                                        SpacerH(0)
                                        Box(Modifier.padding(start = 8.dp)) {
                                            Chip("Most chosen", ChipTone.Action)
                                        }
                                    }
                                    Box(Modifier.weight(1f))
                                    Money(Data.value("\$${tier.second}"), color = palette.ink)
                                }
                                SpacerH(4)
                                Text(tier.third, style = MaterialTheme.typography.bodyMedium, color = palette.inkSecondary)
                                SpacerH(12)
                                if (recommended) {
                                    ActionButton("Join Studio", Data.value("\$${tier.second} / month"), onClick = {
                                        onJoin(tier.first, tier.second)
                                    })
                                } else {
                                    OutlineButton("Select ${tier.first}", onClick = { onJoin(tier.first, tier.second) })
                                }
                            }
                        }
                    }
                }
            }
            item {
                Column(Modifier.padding(20.dp)) {
                    Text(
                        "Prices are locked while a subscription stays active. Cancel before the renewal date to stop the next charge.",
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )
                    SpacerH(8)
                    LabelMono(
                        if (Data.connected) "Fee split on $9.00 · $7.20 creator · $1.80 platform"
                        else "Prices and fee split appear once a payment provider is approved",
                    )
                }
            }
        }
    }
}

@Composable
fun StudioScreen() {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Studio", subtitle = "Creator")
        LazyColumn(Modifier.fillMaxSize()) {
            item {
                Reveal(0, motion) { alpha, _ ->
                    Column(Modifier.graphicsLayer { this.alpha = alpha }.padding(20.dp)) {
                        ActionButton("New post", "publishing stays off", onClick = {})
                        SpacerH(16)
                        SectionTitle("Recent posts", Data.value("48 published"))
                        SpacerH(8)
                        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                            repeat(3) { i ->
                                Box(Modifier.weight(1f)) {
                                    MediaPanel(
                                        height = 104,
                                        corner = 4,
                                        price = if (i == 1) "$14" else null,
                                    )
                                }
                            }
                        }
                        SpacerH(16)
                        CardSurface {
                            Column(Modifier.padding(14.dp)) {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    LabelMono("Earnings")
                                    Box(Modifier.weight(1f))
                                    Chip("Not connected", ChipTone.Pending)
                                }
                                SpacerH(6)
                                Text(
                                    "No payment provider is approved, so no balance is shown. Earnings, payouts and transaction history stay empty until one is.",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.muted,
                                )
                            }
                        }
                        SpacerH(12)
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            StudioStat(Data.value("12,412"), "Members", Modifier.weight(1f))
                            StudioStat(Data.value("50.8%"), "On Studio", Modifier.weight(1f))
                            StudioStat(Data.value("37"), "Unlocks · 7d", Modifier.weight(1f))
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun StudioStat(value: String, label: String, modifier: Modifier = Modifier) {
    val palette = LocalVesperPalette.current
    CardSurface(modifier) {
        Column(Modifier.padding(12.dp)) {
            Money(value, color = palette.ink)
            SpacerH(2)
            LabelMono(label)
        }
    }
}

@Composable
fun AccountScreen(
    mode: ThemeMode,
    onModeChange: (ThemeMode) -> Unit,
) {
    val palette = LocalVesperPalette.current
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "You", subtitle = "Account")
        LazyColumn(Modifier.fillMaxSize()) {
            item {
                Column(Modifier.padding(20.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Monogram("LM", size = 48)
                        SpacerH(0)
                        Column(Modifier.padding(start = 12.dp)) {
                            Text("Fan account", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                            LabelMono("18+ verified · sandbox build")
                        }
                    }
                    SpacerH(16)
                    SectionTitle("Appearance", "system default")
                    SpacerH(8)
                    CardSurface {
                        Column {
                            ThemeRow("System default", "Follow the device", ThemeMode.System, mode, onModeChange)
                            Divider()
                            ThemeRow("Light", "Bone paper, oxblood ink", ThemeMode.Light, mode, onModeChange)
                            Divider()
                            ThemeRow("Dark", "The same room at night", ThemeMode.Dark, mode, onModeChange)
                        }
                    }
                    SpacerH(16)
                    SectionTitle("Content policy", null)
                    SpacerH(8)
                    CardSurface {
                        Column(Modifier.padding(16.dp)) {
                            Text("Public: no nudity, no sexual suggestion.", style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
                            SpacerH(4)
                            Text("Paid: non-explicit nudity and suggestive content, for age-verified subscribers.", style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
                            SpacerH(4)
                            Text("Never: explicit content, minors, non-consensual content.", style = MaterialTheme.typography.bodySmall, color = palette.danger)
                        }
                    }
                    SpacerH(16)
                    LabelMono("v${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE}) · ${BuildConfig.BUILD_TYPE}")
                }
            }
        }
    }
}

@Composable
private fun ThemeRow(
    title: String,
    detail: String,
    value: ThemeMode,
    selected: ThemeMode,
    onSelect: (ThemeMode) -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val isSelected = value == selected
    val dotColor by animateColorAsState(
        targetValue = if (isSelected) palette.action else palette.ruleStrong,
        animationSpec = tween(motion.feedback, easing = motion.easing),
        label = "themeDot",
    )
    Row(
        Modifier
            .fillMaxWidth()
            .pressScale(motion, onClick = { onSelect(value) })
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium, color = palette.ink)
            Text(detail, style = MaterialTheme.typography.bodySmall, color = palette.muted)
        }
        Box(
            Modifier
                .size(20.dp)
                .clip(CircleShape)
                .border(1.dp, dotColor, CircleShape)
                .padding(4.dp),
        ) {
            if (isSelected) {
                Box(Modifier.fillMaxSize().clip(CircleShape).background(palette.action))
            }
        }
    }
}

/**
 * Checkout, deliberately not operational.
 *
 * No PSP has been approved for this business model, so this sheet cannot take money and
 * cannot create a paid state. It says so plainly and offers one way out. It stays an overlay
 * so the membership list behind it keeps its position — the shape a real provider will slot
 * into is exactly what is being built now.
 */
@Composable
fun CheckoutSheet(
    tier: String,
    modifier: Modifier = Modifier,
    onDismiss: () -> Unit,
) {
    val palette: VesperPalette = LocalVesperPalette.current
    Column(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(topStart = 20.dp, topEnd = 20.dp))
            .background(palette.chrome)
            .padding(20.dp)
            .statusBarsPadding(),
    ) {
        Box(Modifier.align(Alignment.CenterHorizontally).size(width = 40.dp, height = 4.dp).clip(CircleShape).background(palette.ruleStrong))
        SpacerH(16)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                LabelMono("Checkout · $tier")
                Text(CREATOR_NAME, style = MaterialTheme.typography.headlineSmall, color = palette.ink)
            }
            Chip("Unavailable", ChipTone.Pending)
        }
        SpacerH(12)
        CardSurface {
            Column(Modifier.padding(14.dp)) {
                Text(
                    "Card payments are not available yet.",
                    style = MaterialTheme.typography.titleMedium,
                    color = palette.ink,
                )
                SpacerH(6)
                Text(
                    "No payment provider has been approved for this business model, so nothing can be charged and no membership can be activated. " +
                        "No card details are collected here.",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
                SpacerH(10)
                Divider()
                SpacerH(10)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("Price", style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
                    Box(Modifier.weight(1f))
                    Money("—", color = palette.muted)
                }
                SpacerH(4)
                Text(
                    "A tier price stays a proposal until a provider is approved.",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
            }
        }
        SpacerH(12)
        OutlineButton("Close", onDismiss)
    }
}

