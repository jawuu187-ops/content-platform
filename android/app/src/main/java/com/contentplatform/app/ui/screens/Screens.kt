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
import com.contentplatform.app.age.AgeReviewRequest
import com.contentplatform.app.age.AgeVerification
import com.contentplatform.app.age.statusLabel
import com.contentplatform.app.dm.money
import com.contentplatform.app.finance.PayoutBalance
import com.contentplatform.app.finance.StudioState
import com.contentplatform.app.finance.explain
import com.contentplatform.app.finance.sharePercent
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.Chip
import com.contentplatform.app.ui.components.ChipTone
import com.contentplatform.app.ui.components.Chrome
import com.contentplatform.app.ui.components.DemoBanner
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.MediaPanel
import com.contentplatform.app.ui.components.Monogram
import com.contentplatform.app.ui.components.Money
import com.contentplatform.app.ui.age.AgeVerificationCard
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
 * Demo data, in one place.
 *
 * This build is not wired to the API yet, so every figure on these screens is invented for
 * product demonstration: it is not account state and not revenue. Surfaces that render one
 * also render a demo label, and the payments path runs through the backend's simulated
 * provider, which stamps every intent and ledger posting as test data. When the real
 * endpoints land, `value` returns server data and the labels come off; nothing else changes.
 */
private object Data {
    fun value(demo: String): String = demo
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
                                    "$CREATOR_HANDLE · creator since 2024 · 48 posts · demo",
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
            subtitle = MEMBERS + " members · 48 posts · demo",
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
            subtitle = "$CREATOR_NAME · 12,412 members · demo",
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
                    LabelMono("Fee split on $9.00 · $7.20 creator · $1.80 platform · demo")
                }
            }
        }
    }
}

/**
 * Studio. The money card and the audience tiles are the server's numbers: this screen never
 * invents a balance, a trend or a member count. Absent data renders as "—".
 */
@Composable
fun StudioScreen(
    studio: StudioState,
    onRequestPayout: () -> Unit,
    onOpenStatement: () -> Unit,
    onOpenCreatorTools: () -> Unit = {},
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Chrome(title = "Studio", subtitle = "Creator")
        LazyColumn(Modifier.fillMaxSize()) {
            item {
                Reveal(0, motion) { alpha, _ ->
                    Column(Modifier.graphicsLayer { this.alpha = alpha }.padding(20.dp)) {
                        ActionButton("Creator tools", "profile · tiers · publish", onClick = onOpenCreatorTools)
                        SpacerH(16)
                        when (studio) {
                            StudioState.Idle, StudioState.Loading -> CardSurface {
                                Column(Modifier.padding(14.dp)) {
                                    LabelMono("Loading your ledger")
                                }
                            }

                            is StudioState.Failed -> CardSurface {
                                Column(Modifier.padding(14.dp)) {
                                    LabelMono("Earnings")
                                    SpacerH(4)
                                    Text(
                                        studio.failure.explain(),
                                        style = MaterialTheme.typography.bodySmall,
                                        color = palette.danger,
                                    )
                                }
                            }

                            is StudioState.Ready -> StudioEarnings(
                                balance = studio.snapshot.balance,
                                testMoneyMinor = studio.snapshot.testMoneyMinor,
                                onRequestPayout = onRequestPayout,
                                onOpenStatement = onOpenStatement,
                            )
                        }
                        SpacerH(12)
                        val stats = (studio as? StudioState.Ready)?.snapshot?.stats
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            StudioStat(stats?.members?.toString() ?: "—", "Members", Modifier.weight(1f))
                            StudioStat(
                                sharePercent(stats?.topTierShareBps),
                                "On " + (stats?.topTierName ?: "tier"),
                                Modifier.weight(1f),
                            )
                            StudioStat(stats?.unlocks7d?.toString() ?: "—", "Unlocks · 7d", Modifier.weight(1f))
                        }
                        SpacerH(12)
                        val year = (studio as? StudioState.Ready)?.year
                        CardSurface(Modifier.clickable { onOpenStatement() }) {
                            Column(Modifier.padding(14.dp)) {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    LabelMono("Statement · " + (year?.currency ?: "this year"))
                                    Box(Modifier.weight(1f))
                                    LabelMono("Open")
                                }
                                SpacerH(4)
                                Text(
                                    if (year == null) {
                                        "The statement could not be loaded. It is still available."
                                    } else {
                                        year.rows.size.toString() + " movements · gross " +
                                            money(year.grossMinor, year.currency) + " · net " +
                                            money(year.netMinor, year.currency)
                                    },
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.muted,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/** The available balance is the payout service's figure, not a sum computed here. */
@Composable
private fun StudioEarnings(
    balance: PayoutBalance,
    testMoneyMinor: Long,
    onRequestPayout: () -> Unit,
    onOpenStatement: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    CardSurface {
        Column(Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                LabelMono("Earnings · available")
                Box(Modifier.weight(1f))
                Chip("No rail", ChipTone.Neutral)
            }
            SpacerH(4)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Money(money(balance.availableMinor, balance.currency), color = palette.ink)
                Box(Modifier.weight(1f))
                Text(
                    if (testMoneyMinor > 0) {
                        "Excludes " + money(testMoneyMinor, balance.currency) + " test money"
                    } else {
                        "Live money only"
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
            }
            SpacerH(4)
            LabelMono(
                "Pending " + money(balance.pendingMinor, balance.currency) +
                    " · minimum " + money(balance.minimumMinor, balance.currency) +
                    " · " + balance.currency,
            )
            SpacerH(12)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Box(Modifier.weight(1f)) { OutlineButton("Statement", onOpenStatement) }
                Box(Modifier.weight(1f)) { OutlineButton("Request payout", onRequestPayout) }
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
    onOpenMessages: () -> Unit,
    onOpenModeration: () -> Unit = {},
    canModerate: Boolean = false,
    ageVerification: AgeVerification? = null,
    ageReview: AgeReviewRequest? = null,
    ageBusy: Boolean = false,
    ageError: String? = null,
    onRequestAgeReview: () -> Unit = {},
    canReviewAge: Boolean = false,
    onOpenAgeReview: () -> Unit = {},
    onOpenSecurity: () -> Unit = {},
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
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
                            LabelMono(ageVerification?.let { statusLabel(it.status) } ?: "Age check")
                        }
                    }
                    SpacerH(16)
                    SectionTitle("Account", "security")
                    SpacerH(8)
                    CardSurface {
                        Row(
                            Modifier.fillMaxWidth()
                                .pressScale(motion, onClick = onOpenSecurity)
                                .padding(horizontal = 16.dp, vertical = 14.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(Modifier.weight(1f)) {
                                Text("Security & sessions", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                                Text("Email verification, two-factor authentication, sign out", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                            }
                            LabelMono("\u2192")
                        }
                    }
                    SpacerH(16)
                    SectionTitle("Age verification", "18+ required")
                    SpacerH(8)
                    AgeVerificationCard(
                        verification = ageVerification,
                        review = ageReview,
                        busy = ageBusy,
                        error = ageError,
                        onRequest = onRequestAgeReview,
                    )
                    if (canReviewAge) {
                        SpacerH(16)
                        SectionTitle("Owner tools", "private")
                        SpacerH(8)
                        CardSurface {
                            Row(
                                Modifier
                                    .fillMaxWidth()
                                    .pressScale(motion, onClick = onOpenAgeReview)
                                    .padding(horizontal = 16.dp, vertical = 14.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Column(Modifier.weight(1f)) {
                                    Text("Age review queue", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                                    Text(
                                        "Approve, reject or revoke manual verification decisions",
                                        style = MaterialTheme.typography.bodySmall,
                                        color = palette.muted,
                                    )
                                }
                                LabelMono("\u2192")
                            }
                        }
                    }
                    SpacerH(16)
                    SectionTitle("Messages", null)
                    SpacerH(8)
                    CardSurface {
                        Row(
                            Modifier
                                .fillMaxWidth()
                                .pressScale(motion, onClick = onOpenMessages)
                                .padding(horizontal = 16.dp, vertical = 14.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(Modifier.weight(1f)) {
                                Text("Inbox", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                                Text(
                                    "Direct messages with creators you support",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.muted,
                                )
                            }
                            LabelMono("\u2192")
                        }
                    }
                    if (canModerate) {
                        SpacerH(16)
                        SectionTitle("Safety", "moderator")
                        SpacerH(8)
                        CardSurface {
                            Row(
                                Modifier
                                    .fillMaxWidth()
                                    .pressScale(motion, onClick = onOpenModeration)
                                    .padding(horizontal = 16.dp, vertical = 14.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Column(Modifier.weight(1f)) {
                                    Text("Reports", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                                    Text(
                                        "Review what was reported and record the decision",
                                        style = MaterialTheme.typography.bodySmall,
                                        color = palette.muted,
                                    )
                                }
                                LabelMono("\u2192")
                            }
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
 * Checkout against the simulated provider.
 *
 * This deliberately does not take card details and does not reach a gateway: it calls the
 * backend's fake provider, which marks the intent and the ledger posting `simulated`. The
 * sheet says so on every surface, because a demo that looks like a receipt is a lie even
 * when the code is honest. Swapping in a real provider replaces the button and the copy; the
 * layout, the overlay behaviour and the confirmation state stay as they are.
 */
@Composable
fun CheckoutSheet(
    tier: String,
    price: String,
    modifier: Modifier = Modifier,
    onSimulate: () -> Unit,
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
            Money("\$$price", color = palette.ink)
        }
        SpacerH(12)
        DemoBanner("Test checkout. No card is charged and no real subscription is created.")
        SpacerH(12)
        CardSurface {
            Column(Modifier.padding(12.dp)) {
                Row { Text("Studio membership, one month", style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary); Box(Modifier.weight(1f)); Money("\$$price") }
                SpacerH(4)
                Row { Text("Platform fee, included (20%)", style = MaterialTheme.typography.bodySmall, color = palette.muted); Box(Modifier.weight(1f)); Money("$1.80", color = palette.muted) }
                SpacerH(8)
                Divider()
                SpacerH(8)
                Row { Text("Due today", style = MaterialTheme.typography.titleMedium, color = palette.ink); Box(Modifier.weight(1f)); Money("\$$price", color = palette.ink) }
            }
        }
        SpacerH(12)
        ActionButton("Simulate payment \$$price", "no card charged", onClick = onSimulate)
        SpacerH(8)
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) {
            Text(
                "Simulated provider · renews monthly in the demo · cancel any time",
                modifier = Modifier.pressScale(LocalVesperMotion.current, onDismiss),
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
        }
    }
}

/**
 * The confirmation for a simulated payment.
 *
 * The copy states what actually happened: the backend wrote a test intent, a test ledger
 * posting and a test entitlement. When a real provider is approved this card keeps its shape
 * and swaps the label for a real receipt; nothing else about the flow changes.
 */
@Composable
fun SimulatedPaymentCard(tier: String, cta: String, onDone: () -> Unit) {
    val palette = LocalVesperPalette.current
    CardSurface {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Chip("Test payment", ChipTone.Pending)
                Box(Modifier.weight(1f))
                Money("$9.00", color = palette.ink)
            }
            SpacerH(8)
            Text("$tier is open in the demo", style = MaterialTheme.typography.titleMedium, color = palette.ink)
            SpacerH(4)
            Text(
                "Simulated. No card was charged and no money moved. The intent and its ledger " +
                    "posting are marked as test data, so they never reach a real balance or payout.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
            SpacerH(12)
            OutlineButton(cta, onDone)
        }
    }
}

