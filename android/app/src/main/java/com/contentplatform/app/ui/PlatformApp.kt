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
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.contentplatform.app.dm.DmApi
import com.contentplatform.app.dm.DmStore
import com.contentplatform.app.age.AgeApi
import com.contentplatform.app.age.AgeStore
import com.contentplatform.app.auth.AuthApi
import com.contentplatform.app.auth.AuthStore
import com.contentplatform.app.auth.SecurityStore
import com.contentplatform.app.catalog.CatalogApi
import com.contentplatform.app.catalog.CatalogStore
import com.contentplatform.app.catalog.CreatorStudioStore
import com.contentplatform.app.dm.ModerationStore
import com.contentplatform.app.dm.SessionStore
import com.contentplatform.app.finance.FinanceApi
import com.contentplatform.app.finance.FinanceStore
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.dm.DmInboxScreen
import com.contentplatform.app.ui.dm.DmThreadScreen
import com.contentplatform.app.ui.finance.PayoutHistoryScreen
import com.contentplatform.app.ui.finance.PayoutRequestSheet
import com.contentplatform.app.ui.finance.StatementScreen
import com.contentplatform.app.ui.age.AgeReviewScreen
import com.contentplatform.app.ui.moderation.ModerationScreen
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.screens.AccountScreen
import com.contentplatform.app.ui.screens.CheckoutSheet
import com.contentplatform.app.ui.screens.SimulatedPaymentCard
import com.contentplatform.app.ui.auth.AuthScreen
import com.contentplatform.app.ui.auth.SecurityScreen
import com.contentplatform.app.ui.auth.VerifyEmailScreen
import com.contentplatform.app.ui.catalog.DiscoverScreen
import com.contentplatform.app.ui.catalog.MembershipsScreen
import com.contentplatform.app.ui.catalog.CreatorToolsScreen
import com.contentplatform.app.ui.finance.AdminPayoutsScreen
import com.contentplatform.app.ui.catalog.PublicCreatorScreen
import com.contentplatform.app.ui.screens.StudioScreen
import com.contentplatform.app.ui.theme.LocalVesperPalette
import com.contentplatform.app.ui.theme.ThemeController
import kotlinx.coroutines.launch

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
    val appScope = rememberCoroutineScope()
    // One session store for the whole process: the messaging store, the moderation store
    // and the shell all have to see the same sign-in, or a moderator who signs in during
    // this session would keep a fan's permissions.
    val sessions = remember(context) { SessionStore(context.applicationContext) }
    // One messaging store for the whole app, created once and reused across recompositions
    // and configuration changes within the process.
    val dmStore = remember(context) { DmStore(sessions, DmApi(sessions)) }
    // The same session and HTTP client serve safety reports; kept separate so a failed
    // report never disturbs a message thread.
    val moderationStore = remember(context) { ModerationStore(sessions, DmApi(sessions)) }
    // Money surfaces share one store: the studio card, the request sheet, the history and the
    // statement all read the same last server answer instead of three divergent copies.
    val financeStore = remember(context) { FinanceStore(sessions, FinanceApi(sessions)) }
    // Age verification and the owner review queue share one store, so the status card and
    // the queue never disagree about a decision the server just applied.
    val ageStore = remember(context) { AgeStore(sessions, AgeApi(sessions)) }
    val catalogStore = remember(context) { CatalogStore(sessions, CatalogApi(sessions)) }
    val authStore = remember(context) { AuthStore(sessions, AuthApi(sessions)) }
    val creatorTools = remember(context) { CreatorStudioStore(context.applicationContext, CatalogApi(sessions)) }
    val securityStore = remember(context) { SecurityStore(sessions, AuthApi(sessions)) }
    val signedIn = remember(sessions.revision) { sessions.current().signedIn }
    if (!signedIn) {
        Box(Modifier.fillMaxSize().background(palette.ground).safeDrawingPadding()) {
            AuthScreen(store = authStore)
        }
        return
    }
    if (authStore.pendingVerificationEmail != null) {
        Box(Modifier.fillMaxSize().background(palette.ground).safeDrawingPadding()) {
            VerifyEmailScreen(store = authStore, onPostpone = authStore::postponeVerification)
        }
        return
    }
    var requestOpen by rememberSaveable { mutableStateOf(false) }
    var statementOpen by rememberSaveable { mutableStateOf(false) }
    var payoutsOpen by rememberSaveable { mutableStateOf(false) }
    // Permissions are re-read whenever the stored session changes, not only at launch.
    LaunchedEffect(sessions.revision) {
        dmStore.syncSession()
        moderationStore.loadMe()
    }
    var destination by rememberSaveable { mutableStateOf(Destination.Home) }
    // The studio refreshes on every entry and whenever the session changes; a fan sees the
    // refusal, not a screen of zeros.
    LaunchedEffect(destination, sessions.revision) {
        if (destination == Destination.Studio) financeStore.loadStudio()
        if (destination == Destination.Account) ageStore.loadAccount()
        if (destination == Destination.Home) catalogStore.loadCreators()
        if (destination == Destination.Membership) catalogStore.loadMemberships()
    }
    var creatorHandle by rememberSaveable { mutableStateOf<String?>(null) }
    var messagesOpen by rememberSaveable { mutableStateOf(false) }
    var moderationOpen by rememberSaveable { mutableStateOf(false) }
    var ageReviewOpen by rememberSaveable { mutableStateOf(false) }
    var creatorToolsOpen by rememberSaveable { mutableStateOf(false) }
    var securityOpen by rememberSaveable { mutableStateOf(false) }
    var adminPayoutsOpen by rememberSaveable { mutableStateOf(false) }
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
                        Destination.Home -> DiscoverScreen(
                            store = catalogStore,
                            onOpenCreator = { handle ->
                                creatorHandle = handle
                                appScope.launch { catalogStore.open(handle) }
                            },
                        )
                        Destination.Membership -> MembershipsScreen(store = catalogStore)
                        Destination.Studio -> StudioScreen(
                            studio = financeStore.studio,
                            onRequestPayout = { requestOpen = true },
                            onOpenStatement = { statementOpen = true },
                            onOpenCreatorTools = { creatorToolsOpen = true },
                        )
                        Destination.Account -> AccountScreen(
                            mode = themeController.mode,
                            onModeChange = themeController::choose,
                            onOpenMessages = { messagesOpen = true },
                            accountEmail = sessions.current().email,
                            onOpenModeration = { moderationOpen = true },
                            canModerate = moderationStore.canModerate,
                            ageVerification = ageStore.verification,
                            ageReview = ageStore.review,
                            ageBusy = ageStore.requesting,
                            ageError = ageStore.requestError,
                            onRequestAgeReview = {
                                appScope.launch { ageStore.requestManual() }
                            },
                            canReviewAge = moderationStore.roles.contains("owner") &&
                                moderationStore.permissions.contains("verify.review"),
                            onOpenAgeReview = { ageReviewOpen = true },
                            onOpenSecurity = { securityOpen = true },
                            canReviewPayouts = moderationStore.permissions.contains("payouts.approve"),
                            onOpenAdminPayouts = { adminPayoutsOpen = true },
                        )
                    }
                }
            }
            VesperBottomBar(
                selected = destination,
                onSelect = { dest ->
                    creatorHandle = null
                    catalogStore.close()
                    destination = dest
                },
            )
        }

        // Pushed creator profile.
        AnimatedVisibility(
            visible = creatorHandle != null,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                PublicCreatorScreen(
                    store = catalogStore,
                    onBack = {
                        creatorHandle = null
                        catalogStore.close()
                    },
                    onMessage = { creatorId ->
                        appScope.launch {
                            val conversation = dmStore.openWith(creatorId)
                            if (conversation != null) {
                                openThreadId = conversation.id
                                messagesOpen = true
                            }
                        }
                    },
                    messageBusy = dmStore.openingConversation,
                    messageError = dmStore.openFailure,
                    onClearMessageError = dmStore::clearOpenFailure,
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

        // Finance queue for accounts holding payouts.approve.
        AnimatedVisibility(
            visible = adminPayoutsOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                AdminPayoutsScreen(store = financeStore, onBack = { adminPayoutsOpen = false })
            }
        }

        // Account security: verification, authenticator enrolment and sign-out.
        AnimatedVisibility(
            visible = securityOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                SecurityScreen(store = securityStore, onBack = { securityOpen = false })
            }
        }

        // Creator onboarding, tiers and publishing, pushed over Studio.
        AnimatedVisibility(
            visible = creatorToolsOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                CreatorToolsScreen(store = creatorTools, onBack = { creatorToolsOpen = false })
            }
        }

        // The owner-only age review queue, pushed over the account screen. The server
        // enforces the owner role, verify.review and a recent MFA step-up on every action.
        AnimatedVisibility(
            visible = ageReviewOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                AgeReviewScreen(store = ageStore, onBack = { ageReviewOpen = false })
            }
        }

        // The creator statement, pushed over Studio. It never becomes a fifth tab.
        AnimatedVisibility(
            visible = statementOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                StatementScreen(
                    store = financeStore,
                    onBack = { statementOpen = false },
                    onOpenPayouts = { payoutsOpen = true },
                )
            }
        }

        // Payout history is a second level of the same push: Back returns to the statement.
        AnimatedVisibility(
            visible = payoutsOpen,
            enter = slideInVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeIn(tween(motion.transition, easing = motion.easing)),
            exit = slideOutVertically(
                animationSpec = tween(motion.transition, easing = motion.easing),
            ) { height -> height / 10 } + fadeOut(tween(motion.feedback, easing = motion.easing)),
        ) {
            Box(Modifier.fillMaxSize().background(palette.ground)) {
                PayoutHistoryScreen(
                    store = financeStore,
                    onBack = { payoutsOpen = false },
                    onRequestPayout = { requestOpen = true },
                )
            }
        }

        // Request payout is a sheet over whichever money surface opened it. Dismissing it
        // clears the last receipt so the next open starts at the amount field.
        if (requestOpen) {
            PayoutRequestSheet(
                store = financeStore,
                onDismiss = {
                    financeStore.dismissRequest()
                    requestOpen = false
                },
            )
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
                        // A stable handle for devices tests and for anyone listening to the
                        // screen: "Tab You" is unambiguous where the label "You" is not.
                        .semantics { contentDescription = "Tab ${dest.label}" }
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
