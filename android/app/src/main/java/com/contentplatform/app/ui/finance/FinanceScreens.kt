package com.contentplatform.app.ui.finance

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.contentplatform.app.dm.money
import com.contentplatform.app.dm.parseAmountToMinor
import com.contentplatform.app.finance.FinanceFailure
import com.contentplatform.app.finance.FinanceStore
import com.contentplatform.app.finance.Payout
import com.contentplatform.app.finance.PayoutDetailState
import com.contentplatform.app.finance.PayoutEvent
import com.contentplatform.app.finance.PayoutRequestState
import com.contentplatform.app.finance.PayoutStatus
import com.contentplatform.app.finance.StatementPeriod
import com.contentplatform.app.finance.StatementState
import com.contentplatform.app.finance.StudioState
import com.contentplatform.app.finance.explain
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.Chip
import com.contentplatform.app.ui.components.ChipTone
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.Money
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.components.SpacerW
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

/**
 * Creator money screens: request, history and statement.
 *
 * None of these screens computes a number. The balance, the totals and the per-row figures
 * are what the API returned; the only arithmetic here is choosing which preset to show and
 * whether typed text parses as an amount.
 */

@Composable
private fun FinanceHeader(
    title: String,
    subtitle: String?,
    onBack: () -> Unit,
    trailing: (@Composable () -> Unit)? = null,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column(Modifier.fillMaxWidth().background(palette.chrome)) {
        Row(
            Modifier.fillMaxWidth().height(60.dp).padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .pressScale(motion, onClick = onBack)
                    .semantics { contentDescription = "Back" },
                contentAlignment = Alignment.Center,
            ) {
                Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink)
            }
            Column(Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.titleMedium, color = palette.ink)
                if (subtitle != null) {
                    Text(subtitle, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                }
            }
            trailing?.invoke()
        }
        Divider()
    }
}

/** The no-rail statement, shown wherever money is about to move. */
@Composable
private fun NoRailNotice(
    headline: String,
    body: String,
) {
    val palette = LocalVesperPalette.current
    Box(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(4.dp))
            .background(palette.surfaceRaised)
            .border(1.dp, palette.rule, RoundedCornerShape(4.dp))
            .padding(12.dp),
    ) {
        Row {
            Box(Modifier.width(2.dp).height(52.dp).background(palette.action))
            SpacerW(10)
            Column {
                Text(headline, style = MaterialTheme.typography.bodySmall, color = palette.ink)
                SpacerH(2)
                Text(body, style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }
        }
    }
}

// ------------------------------------------------------------------ request

@Composable
fun PayoutRequestSheet(
    store: FinanceStore,
    onDismiss: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    BackHandler(onBack = onDismiss)
    val ready = store.studio as? StudioState.Ready
    val balance = ready?.snapshot?.balance
    val state = store.request

    var amountText by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }
    val parsed = remember(amountText) { parseAmountToMinor(amountText) }

    Box(Modifier.fillMaxSize()) {
        Box(
            Modifier
                .fillMaxSize()
                .background(palette.scrim)
                .pressScale(motion, onClick = onDismiss),
        )
        Column(
            Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .background(palette.surface)
                .border(1.dp, palette.rule, RoundedCornerShape(topStart = 14.dp, topEnd = 14.dp))
                .imePadding()
                .verticalScroll(rememberScrollState())
                .padding(20.dp),
        ) {
            Box(
                Modifier
                    .align(Alignment.CenterHorizontally)
                    .width(40.dp)
                    .height(4.dp)
                    .clip(CircleShape)
                    .background(palette.ruleStrong),
            )
            SpacerH(14)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Request payout", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Box(Modifier.weight(1f))
                Box(
                    Modifier.size(32.dp).pressScale(motion, onClick = onDismiss)
                        .semantics { contentDescription = "Close" },
                    contentAlignment = Alignment.Center,
                ) {
                    Text("\u2715", style = MaterialTheme.typography.bodyMedium, color = palette.muted)
                }
            }

            if (balance == null) {
                SpacerH(12)
                Text("The studio balance has not loaded yet.", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                SpacerH(12)
                OutlineButton("Close", onDismiss)
                return@Column
            }

            if (state is PayoutRequestState.Done) {
                SpacerH(12)
                CardSurface {
                    Column(Modifier.padding(14.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Chip("Requested", ChipTone.Pending)
                            Box(Modifier.weight(1f))
                            Money(money(state.payout.amountMinor, state.payout.currency), color = palette.ink)
                        }
                        SpacerH(8)
                        Text("Waiting for finance", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                        SpacerH(4)
                        Text(
                            "Nothing is reserved yet. If it is approved, the amount leaves your " +
                                "available balance and waits in payout clearing; no money is transferred.",
                            style = MaterialTheme.typography.bodySmall,
                            color = palette.muted,
                        )
                    }
                }
                SpacerH(12)
                ActionButton("Done", null, onClick = {
                    store.dismissRequest()
                    onDismiss()
                })
                return@Column
            }

            SpacerH(12)
            Row(verticalAlignment = Alignment.Bottom) {
                Column(Modifier.weight(1f)) {
                    LabelMono("Available")
                    Money(money(balance.availableMinor, balance.currency), color = palette.ink, modifier = Modifier.padding(top = 2.dp))
                }
                Text(
                    "Minimum " + money(balance.minimumMinor, balance.currency),
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
            }

            SpacerH(14)
            LabelMono("Amount")
            SpacerH(4)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(money(0, balance.currency).take(1), style = MaterialTheme.typography.titleLarge, color = palette.ink)
                SpacerW(4)
                BasicTextField(
                    value = amountText,
                    onValueChange = { amountText = it.filter { ch -> ch.isDigit() || ch == '.' } },
                    singleLine = true,
                    textStyle = MaterialTheme.typography.titleLarge.copy(color = palette.ink),
                    cursorBrush = SolidColor(palette.action),
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Decimal,
                        imeAction = ImeAction.Done,
                    ),
                    modifier = Modifier
                        .weight(1f)
                        .semantics { contentDescription = "Payout amount" },
                )
                Text(
                    "Max " + money(10_000_000L, balance.currency),
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
            }
            SpacerH(6)
            Divider(strong = amountText.isNotEmpty())

            SpacerH(10)
            val half = balance.availableMinor / 2
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                PresetButton(
                    "Min " + money(balance.minimumMinor, balance.currency),
                    Modifier.weight(1f),
                ) { amountText = plainAmount(balance.minimumMinor) }
                PresetButton(
                    "Half " + money(half, balance.currency),
                    Modifier.weight(1f),
                ) { amountText = plainAmount(half) }
                PresetButton(
                    "All " + money(balance.availableMinor, balance.currency),
                    Modifier.weight(1f),
                ) { amountText = plainAmount(balance.availableMinor) }
            }

            SpacerH(14)
            Row(verticalAlignment = Alignment.CenterVertically) {
                LabelMono("Method")
                Box(Modifier.weight(1f))
                Chip("Not connected", ChipTone.Neutral)
            }
            SpacerH(4)
            Text(
                "No bank, wallet or account is connected. The request stores an informational " +
                    "label only; the API defaults it to “manual”.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )

            SpacerH(12)
            LabelMono("Note")
            SpacerH(4)
            BasicTextField(
                value = note,
                onValueChange = { if (it.length <= 500) note = it },
                textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.inkSecondary),
                cursorBrush = SolidColor(palette.action),
                modifier = Modifier
                    .fillMaxWidth()
                    .semantics { contentDescription = "Payout note" },
            )
            SpacerH(4)
            Row {
                Text("Optional", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                Box(Modifier.weight(1f))
                Text("${note.length} / 500", style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }

            SpacerH(14)
            NoRailNotice(
                headline = "Approval reserves " + money(parsed ?: 0L, balance.currency) + " in your ledger. It does not transfer money.",
                body = "No payout rail is connected. An approved amount leaves your available " +
                    "balance and waits in payout clearing; nothing is sent to a bank or wallet, " +
                    "and no request is ever marked paid.",
            )

            val refusal = requestRefusal(parsed, balance.minimumMinor, balance.availableMinor, balance.currency)
            if (state is PayoutRequestState.Failed) {
                SpacerH(8)
                Text(state.failure.explain(), style = MaterialTheme.typography.bodySmall, color = palette.danger)
            } else if (refusal != null) {
                SpacerH(8)
                Text(refusal, style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }

            SpacerH(12)
            ActionButton(
                label = "Request " + money(parsed ?: 0L, balance.currency),
                price = null,
                enabled = refusal == null && state !is PayoutRequestState.Submitting,
                onClick = {
                    val amount = parsed ?: return@ActionButton
                    scope.launch {
                        store.requestPayout(amount, balance.currency, note)
                    }
                },
            )
        }
    }
}

/**
 * The same bounds the server enforces, shown before submission. Returns null when the typed
 * amount may be sent; the server still has the final word.
 */
private fun requestRefusal(
    amountMinor: Long?,
    minimumMinor: Long,
    availableMinor: Long,
    currency: String,
): String? = when {
    amountMinor == null -> "Enter an amount."
    amountMinor < minimumMinor -> "The smallest payout is " + money(minimumMinor, currency) + "."
    amountMinor > availableMinor -> "That is more than your available balance."
    amountMinor > 10_000_000L -> "The largest payout is " + money(10_000_000L, currency) + "."
    else -> null
}

@Composable
private fun PresetButton(label: String, modifier: Modifier, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Box(
        modifier
            .height(44.dp)
            .clip(RoundedCornerShape(8.dp))
            .border(1.dp, palette.ruleStrong, RoundedCornerShape(8.dp))
            .pressScale(motion, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = palette.ink)
    }
}

private fun plainAmount(minor: Long): String {
    val units = minor / 100
    val cents = minor % 100
    return if (cents == 0L) "$units" else "$units.${cents.toString().padStart(2, '0')}"
}

// ------------------------------------------------------------------ history

@Composable
fun PayoutHistoryScreen(
    store: FinanceStore,
    onBack: () -> Unit,
    onRequestPayout: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    BackHandler(onBack = onBack)
    var expanded by remember { mutableStateOf<String?>(null) }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        FinanceHeader(
            title = "Payouts",
            subtitle = null,
            onBack = onBack,
            trailing = {
                Text(
                    "Refresh",
                    modifier = Modifier.pressScale(motion, onClick = { scope.launch { store.loadStudio() } }),
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
            },
        )

        when (val state = store.studio) {
            StudioState.Idle, StudioState.Loading -> FinanceLoading("Loading payouts")
            is StudioState.Failed -> FinanceFailureBlock(state.failure, onRetry = { scope.launch { store.loadStudio() } })
            is StudioState.Ready -> {
                val balance = state.snapshot.balance
                val payouts = state.snapshot.payouts
                LazyColumn(Modifier.fillMaxSize().padding(20.dp)) {
                    item {
                        Row(verticalAlignment = Alignment.Bottom) {
                            Column(Modifier.weight(1f)) {
                                LabelMono("Available")
                                SpacerH(2)
                                Money(money(balance.availableMinor, balance.currency), color = palette.ink)
                            }
                            Column(horizontalAlignment = Alignment.End) {
                                LabelMono("Pending · " + money(balance.pendingMinor, balance.currency))
                                SpacerH(2)
                                Text(
                                    "Minimum " + money(balance.minimumMinor, balance.currency) + " · " + balance.currency,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.muted,
                                )
                            }
                        }
                        SpacerH(12)
                        ActionButton("Request payout", null, onClick = onRequestPayout)
                        SpacerH(12)
                        NoRailNotice(
                            headline = "No rail is connected.",
                            body = "“Reserved” means the amount left your available balance and is " +
                                "held in payout clearing. It is not a transfer, and this build has " +
                                "no paid state.",
                        )
                        SpacerH(8)
                        LabelMono("Requests")
                        SpacerH(4)
                    }
                    if (payouts.isEmpty()) {
                        item {
                            Text(
                                "No payout requests yet.",
                                style = MaterialTheme.typography.bodySmall,
                                color = palette.muted,
                            )
                        }
                    }
                    items(payouts, key = { it.id }) { payout ->
                        PayoutRow(
                            payout = payout,
                            detail = if (expanded == payout.id) store.detail else PayoutDetailState.Idle,
                            onToggle = {
                                if (expanded == payout.id) {
                                    expanded = null
                                    store.clearDetail()
                                } else {
                                    expanded = payout.id
                                    scope.launch { store.loadDetail(payout.id) }
                                }
                            },
                            onCancel = { scope.launch { store.cancelPayout(payout.id, payout.currency) } },
                        )
                        SpacerH(8)
                    }
                    item {
                        SpacerH(4)
                        Text(
                            "A rejected or cancelled request can be asked for again. A reserved " +
                                "payout does not block a new request.",
                            style = MaterialTheme.typography.bodySmall,
                            color = palette.muted,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun PayoutRow(
    payout: Payout,
    detail: PayoutDetailState,
    onToggle: () -> Unit,
    onCancel: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    CardSurface {
        Column(Modifier.padding(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Chip(statusLabel(payout.status), statusTone(payout.status))
                Box(Modifier.weight(1f))
                Money(money(payout.amountMinor, payout.currency), color = palette.ink)
            }
            SpacerH(6)
            val method = if (payout.method.isBlank()) "manual" else payout.method
            Text(
                "method label: $method · no destination stored · " + FinanceStore.shortDate(payout.createdAt),
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
            if (payout.decisionNote.isNotBlank()) {
                SpacerH(4)
                Text(
                    "“" + payout.decisionNote + "” — finance",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.inkSecondary,
                )
            }
            SpacerH(8)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    "Events",
                    modifier = Modifier.pressScale(motion, onClick = onToggle),
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.ink,
                )
                Box(Modifier.weight(1f))
                if (payout.status == PayoutStatus.Requested) {
                    Text(
                        "Cancel",
                        modifier = Modifier.pressScale(motion, onClick = onCancel),
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.danger,
                    )
                }
            }
            when (detail) {
                PayoutDetailState.Idle -> Unit
                PayoutDetailState.Loading -> {
                    SpacerH(6)
                    Text("Loading history…", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                }
                is PayoutDetailState.Failed -> {
                    SpacerH(6)
                    Text(detail.message, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                }
                is PayoutDetailState.Ready -> {
                    SpacerH(6)
                    detail.events.forEach { event -> EventLine(event) }
                    if (detail.events.isEmpty()) {
                        Text("No events recorded.", style = MaterialTheme.typography.bodySmall, color = palette.muted)
                    }
                }
            }
        }
    }
}

@Composable
private fun EventLine(event: PayoutEvent) {
    val palette = LocalVesperPalette.current
    val move = if (event.fromStatus.isBlank()) {
        event.toStatus
    } else {
        event.fromStatus + " → " + event.toStatus
    }
    Text(
        "${event.action.ifBlank { "event" }} · $move · " + FinanceStore.shortDate(event.createdAt),
        style = MaterialTheme.typography.bodySmall,
        color = palette.muted,
    )
}

private fun statusLabel(status: String): String = when (status) {
    PayoutStatus.Requested -> "Requested"
    PayoutStatus.Approved -> "Reserved"
    PayoutStatus.Rejected -> "Rejected"
    PayoutStatus.Cancelled -> "Cancelled"
    else -> status.replaceFirstChar { it.uppercase() }
}

private fun statusTone(status: String): ChipTone = when (status) {
    PayoutStatus.Approved -> ChipTone.Paid
    PayoutStatus.Rejected -> ChipTone.Danger
    PayoutStatus.Requested -> ChipTone.Pending
    else -> ChipTone.Neutral
}

// ------------------------------------------------------------------ statement

@Composable
fun StatementScreen(
    store: FinanceStore,
    onBack: () -> Unit,
    onOpenPayouts: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    val context = androidx.compose.ui.platform.LocalContext.current
    BackHandler(onBack = onBack)

    val ready = store.studio as? StudioState.Ready
    val currency = ready?.snapshot?.balance?.currency ?: "USD"

    LaunchedEffect(Unit) {
        store.loadStatementForStudio()
    }
    LaunchedEffect(store.statementPeriod, store.includeSimulated, store.statementCurrency) {
        if (store.statement is StatementState.Ready || store.statement is StatementState.Failed) {
            store.loadStatement(store.statementCurrency)
        }
    }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        FinanceHeader(
            title = "Statement",
            subtitle = store.statementCurrency,
            onBack = onBack,
            trailing = {
                Text(
                    "Payouts",
                    modifier = Modifier.pressScale(motion, onClick = onOpenPayouts),
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.ink,
                )
            },
        )

        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(20.dp)) {
            when (val state = store.statement) {
                StatementState.Idle, StatementState.Loading -> FinanceLoading("Loading statement")
                is StatementState.Failed -> {
                    FinanceFailureBlock(state.failure, onRetry = { scope.launch { store.loadStatement(currency) } })
                    SpacerH(12)
                    PeriodControls(store)
                }
                is StatementState.Ready -> {
                    val statement = state.statement
                    Row(verticalAlignment = Alignment.Top) {
                        Column(Modifier.weight(1f)) {
                            LabelMono("Net movement")
                            SpacerH(2)
                            Money(money(statement.netMinor, statement.currency), color = palette.ink)
                        }
                        Column(horizontalAlignment = Alignment.End) {
                            LabelMono("Gross " + money(statement.grossMinor, statement.currency))
                            SpacerH(2)
                            LabelMono(statement.rows.size.toString() + " entries")
                            SpacerH(2)
                            Text(
                                statement.excludedSimulated.toString() + " test row(s) excluded",
                                style = MaterialTheme.typography.bodySmall,
                                color = palette.muted,
                            )
                        }
                    }

                    SpacerH(12)
                    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                        listOf("USD", "EUR", "GBP").forEach { code ->
                            PeriodTab(code, statement.currency == code) { store.chooseCurrency(code) }
                        }
                    }
                    SpacerH(8)
                    PeriodControls(store)

                    SpacerH(8)
                    Text(
                        "One currency per statement: USD, EUR or GBP, never converted. Periods " +
                            "over 400 days or 5,000 movements are refused, not truncated.",
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )

                    SpacerH(14)
                    LabelMono("Movements")
                    SpacerH(6)
                    if (statement.rows.isEmpty()) {
                        Text(
                            "No movements in this period.",
                            style = MaterialTheme.typography.bodySmall,
                            color = palette.muted,
                        )
                    }
                    statement.rows.forEach { row ->
                        Row(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
                            Column(Modifier.weight(1f)) {
                                Text(
                                    FinanceStore.shortDate(row.occurredAt) + " · " + row.kind.replaceFirstChar { it.uppercase() },
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.ink,
                                )
                                Text(
                                    shortReference(row.reference) + (if (row.simulated) " · test" else ""),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.muted,
                                )
                            }
                            Column(horizontalAlignment = Alignment.End) {
                                Text(
                                    if (row.purpose.isNotBlank()) money(row.grossMinor, statement.currency) else "—",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = palette.muted,
                                )
                                Text(
                                    (if (row.netMinor >= 0) "+ " else "") + money(row.netMinor, statement.currency),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = if (row.netMinor >= 0) palette.sage else palette.ink,
                                )
                            }
                        }
                        Divider()
                    }

                    SpacerH(10)
                    Text(
                        if (store.includeSimulated) {
                            "Test rows are included. Tap to leave them out."
                        } else {
                            "Live money only. Tap to include test rows."
                        },
                        modifier = Modifier.pressScale(
                            motion,
                            onClick = { store.toggleIncludeSimulated() },
                        ),
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.ink,
                    )

                    SpacerH(12)
                    ActionButton(
                        label = "Download CSV",
                        price = null,
                        enabled = !store.saving,
                        onClick = { scope.launch { store.saveStatement(context, statement) } },
                    )
                    SpacerH(6)
                    Text(
                        "Minor units · RFC 3339 · CSV only. PDF and email receipts are not built.",
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )
                }
            }

            store.notice?.let { message ->
                SpacerH(10)
                Text(message, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
            }
        }
    }
}

@Composable
private fun PeriodControls(store: FinanceStore) {
    val palette = LocalVesperPalette.current
    Column {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            PeriodTab("This year", store.statementPeriod == StatementPeriod.ThisYear) {
                store.choosePeriod(StatementPeriod.ThisYear)
            }
            PeriodTab("30 days", store.statementPeriod == StatementPeriod.Last30Days) {
                store.choosePeriod(StatementPeriod.Last30Days)
            }
            PeriodTab("Custom", store.statementPeriod == StatementPeriod.Custom) {
                store.choosePeriod(StatementPeriod.Custom)
            }
        }
        Divider()
        if (store.statementPeriod == StatementPeriod.Custom) {
            SpacerH(8)
            DateField("From", store.customFrom, store::editCustomFrom)
            SpacerH(6)
            DateField("To", store.customTo, store::editCustomTo)
        }
        SpacerH(4)
        Box(Modifier.fillMaxWidth()) {
            Text(
                "UTC days",
                modifier = Modifier.align(Alignment.CenterEnd),
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
        }
    }
}

@Composable
private fun PeriodTab(label: String, selected: Boolean, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Column(
        Modifier.pressScale(motion, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodySmall,
            color = if (selected) palette.ink else palette.muted,
        )
        SpacerH(4)
        Box(
            Modifier
                .width(28.dp)
                .height(2.dp)
                .background(if (selected) palette.action else palette.ground),
        )
    }
}

@Composable
private fun DateField(label: String, value: String, onChange: (String) -> Unit) {
    val palette = LocalVesperPalette.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        LabelMono(label)
        SpacerW(8)
        BasicTextField(
            value = value,
            onValueChange = { if (it.length <= 10) onChange(it) },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodySmall.copy(color = palette.ink),
            cursorBrush = SolidColor(palette.action),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, imeAction = ImeAction.Done),
            modifier = Modifier
                .weight(1f)
                .semantics { contentDescription = "$label date" },
        )
        Text("YYYY-MM-DD", style = MaterialTheme.typography.bodySmall, color = palette.muted)
    }
    Divider()
}

private fun shortReference(reference: String): String {
    val value = reference.substringAfterLast(':')
    return if (value.length <= 14) value else value.take(8) + "…" + value.takeLast(4)
}

// ------------------------------------------------------------------ shared

@Composable
private fun FinanceLoading(label: String) {
    Column(Modifier.fillMaxSize().padding(24.dp)) {
        LabelMono(label)
    }
}

@Composable
private fun FinanceFailureBlock(failure: FinanceFailure, onRetry: () -> Unit) {
    val palette = LocalVesperPalette.current
    Column {
        Text(failure.explain(), style = MaterialTheme.typography.bodySmall, color = palette.danger)
        SpacerH(8)
        OutlineButton("Try again", onRetry)
    }
}
