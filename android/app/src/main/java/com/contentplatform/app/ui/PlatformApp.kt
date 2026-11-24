package com.contentplatform.app.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.contentplatform.app.dm.DmApi
import com.contentplatform.app.dm.DmStore
import com.contentplatform.app.dm.ModerationStore
import com.contentplatform.app.dm.SessionStore
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.dm.DmInboxScreen
import com.contentplatform.app.ui.dm.DmThreadScreen
import com.contentplatform.app.ui.moderation.ModerationScreen
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.screens.AccountScreen
import com.contentplatform.app.ui.screens.CheckoutSheet
import com.contentplatform.app.ui.screens.SimulatedPaymentCard
import com.contentplatform.app.ui.screens.CreatorScreen
import com.contentplatform.app.ui.screens.HomeScreen
import com.contentplatform.app.ui.screens.MembershipScreen
import com.contentplatform.app.ui.screens.StudioScreen
import com.contentplatform.app.ui.theme.LocalVesperPalette
import com.contentplatform.app.ui.theme.ThemeController

/**
 * Four destinations and one overlay stack.
 *
 * Home → creator profile is a pushed surface, not a tab: it slides up over the feed and can
 * be dismissed without losing the feed's scroll position. Checkout is a sheet over
 * Membership for the same reason. Only the bottom destinations animate horizontally, so the
 * app has one direction of travel and never feels like a pile of unrelated screens.
 */
enum class Destination(val label: String) {
    Home("Home"),
    Membership("Tiers"),
    Studio("Studio"),
    Account("You"),
}

@Composable
fun PlatformApp(themeController: ThemeController) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val context = LocalContext.current
    // One messaging store for the whole app, created once and reused across recompositions
    // and configuration changes within the process.
    val dmStore = remember(context) {
        val sessions = SessionStore(context.applicationContext)
        DmStore(sessions, DmApi(sessions))
    }
    // The same session and HTTP client serve safety reports; kept separate so a failed
    // report never disturbs a message thread.
    val moderationStore = remember(context) {
        val sessions = SessionStore(context.applicationContext)
        ModerationStore(sessions, DmApi(sessions))
    }
    LaunchedEffect(Unit) { moderationStore.loadMe() }

    var destination by rememberSaveable { mutableStateOf(Destination.Home) }
    var creatorOpen by rememberSaveable { mutableStateOf(false) }
    var messagesOpen by rememberSaveable { mutableStateOf(false) }
    var moderationOpen by rememberSaveable { mutableStateOf(false) }
    var openThreadId by rememberSaveable { mutableStateOf<String?>(null) }
    // Checkout is a pair of (tier, price) in the demo. A real provider replaces the price
    // with a server-issued intent; the sheet does not compute it.
    var checkout by remember { mutableStateOf<Pair<String, String>?>(null) }
    var lastCheckout by remember { mutableStateOf<Pair<String, String>?>(null) }
    var simulatedTier by remember { mutableStateOf<String?>(null) }
    if (checkout != null) lastCheckout = checkout

    Box(Modifier.fillMaxSize().background(palette.ground).safeDrawingPadding()) {
        Column(Modifier.fillMaxSize()) {
            Box(Modifier.weight(1f)) {
                AnimatedContent(
                    targetState = destination,
                    transitionSpec = {
                        val forward = targetState.ordinal >= initialState.ordinal
                        val enter = slideInHorizontally(
                            animationSpec = tween(motion.transition, easing = motion.easing),
                        ) { width -> if (forward) width / 12 else -width / 12 } +
                            fadeIn(tween(motion.transition, easing = motion.easing))
                        val exit = slideOutHorizontally(
                            animationSpec = tween(motion.feedback, easing = motion.easing),
                        ) { width -> if (forward) -width / 12 else width / 12 } +
                            fadeOut(tween(motion.feedback, easing = motion.easing))
                        enter togetherWith exit
                    },
                    label = "destination",
                ) { dest ->
                    when (dest) {
                        Destination.Home -> HomeScreen(
                            onOpenCreator = { creatorOpen = true },
                            onOpenMembership = { destination = Destination.Membership },
                        )
                        Destination.Membership -> MembershipScreen(
                            onJoin = { tier, price -> checkout = tier to price },
                        )
                        Destination.Studio -> StudioScreen()
                        Destination.Account -> AccountScreen(
                            mode = themeController.mode,
                            onModeChange = themeController::choose,
                            onOpenMessages = { messagesOpen = true },
                            onOpenModeration = { moderationOpen = true },
                            canModerate = moderationStore.canModerate,
                        )
                    }
                }
            }
            VesperBottomBar(
                selected = destination,
                onSelect = { dest ->
                    creatorOpen = false
                    destination = dest
                },
            )
        }

        // Pushed creator profile.
        AnimatedVisibility(
            visible = creatorOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                CreatorScreen(
                    onBack = { creatorOpen = false },
                    onOpenMembership = {
                        creatorOpen = false
                        destination = Destination.Membership
                    },
                )
            }
        }

        // Direct messages: a pushed surface over the current destination, like the
        // creator profile. The thread is a second level of the same push, so Back returns
        // to the inbox and Back again returns to the tab the person was on.
        AnimatedVisibility(
            visible = messagesOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                val threadId = openThreadId
                if (threadId == null) {
                    DmInboxScreen(
                        store = dmStore,
                        onBack = { messagesOpen = false },
                        onOpenThread = { openThreadId = it },
                    )
                } else {
                    LaunchedEffect(threadId) { dmStore.loadThread(threadId) }
                    DmThreadScreen(
                        store = dmStore,
                        moderation = moderationStore,
                        onBack = { openThreadId = null },
                    )
                }
            }
        }

        // The safety queue, pushed over the tab the moderator was on.
        AnimatedVisibility(
            visible = moderationOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                ModerationScreen(store = moderationStore, onBack = { moderationOpen = false })
            }
        }

        // Checkout: scrim and sheet animate independently, so the scrim never slides.
        Box(Modifier.fillMaxSize()) {
            AnimatedVisibility(
                visible = checkout != null,
                enter = fadeIn(tween(motion.transition, easing = motion.easing)),
                exit = fadeOut(tween(motion.feedback, easing = motion.easing)),
                modifier = Modifier.fillMaxSize(),
            ) {
                Box(
                    Modifier
                        .fillMaxSize()
                        .background(palette.scrim)
                        .pressScale(motion, onClick = { checkout = null }),
                )
            }
            AnimatedVisibility(
                visible = checkout != null,
                enter = slideInVertically(
                    animationSpec = tween(motion.transition, easing = motion.easing),
                ) { height -> height },
                exit = slideOutVertically(
                    animationSpec = tween(motion.feedback, easing = motion.easing),
                ) { height -> height },
                modifier = Modifier.align(Alignment.BottomCenter),
            ) {
                lastCheckout?.let { (tier, price) ->
                    CheckoutSheet(
                        tier = tier,
                        price = price,
                        onSimulate = {
                            simulatedTier = tier
                            checkout = null
                        },
                        onDismiss = { checkout = null },
                    )
                }
            }
        }

        // The confirmation is an overlay too, so the membership list is never lost.
        AnimatedVisibility(
            visible = simulatedTier != null,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 2 } + fadeIn(tween(motion.transition)),
            exit = slideOutVertically(tween(motion.feedback)) { height -> height / 2 } +
                fadeOut(tween(motion.feedback)),
            modifier = Modifier.align(Alignment.BottomCenter).padding(20.dp),
        ) {
            SimulatedPaymentCard(
                tier = simulatedTier ?: "Studio",
                cta = "Done",
                onDone = { simulatedTier = null },
            )
        }
    }
}

@Composable
private fun VesperBottomBar(selected: Destination, onSelect: (Destination) -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column {
        Divider()
        Row(
            Modifier
                .fillMaxWidth()
                .height(64.dp)
                .background(palette.chrome)
                .padding(horizontal = 8.dp),
            horizontalArrangement = Arrangement.SpaceEvenly,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Destination.entries.forEach { dest ->
                val active = dest == selected
                val tint by animateColorAsState(
                    targetValue = if (active) palette.ink else palette.muted,
                    animationSpec = tween(motion.feedback, easing = motion.easing),
                    label = "tabTint",
                )
                val markColor by animateColorAsState(
                    targetValue = if (active) palette.action else palette.ruleStrong,
                    animationSpec = tween(motion.feedback, easing = motion.easing),
                    label = "tabMark",
                )
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .pressScale(motion, onClick = { onSelect(dest) })
                        .padding(vertical = 8.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Box(
                        Modifier
                            .size(20.dp)
                            .clip(shapeFor(dest))
                            .background(markColor),
                    )
                    Text(
                        dest.label,
                        style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.Medium),
                        color = tint,
                    )
                }
            }
        }
    }
}

private fun shapeFor(destination: Destination): Shape = when (destination) {
    Destination.Home -> CircleShape
    Destination.Membership -> RoundedCornerShape(4.dp)
    Destination.Studio -> RoundedCornerShape(2.dp)
    Destination.Account -> CircleShape
}
