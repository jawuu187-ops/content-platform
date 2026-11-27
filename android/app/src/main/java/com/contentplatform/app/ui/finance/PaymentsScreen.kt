package com.contentplatform.app.ui.finance

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.contentplatform.app.finance.FanPayment
import com.contentplatform.app.finance.FinanceStore
import com.contentplatform.app.finance.PaymentsState
import com.contentplatform.app.finance.money
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

/** A fan's payment intents: memberships, unlocks and tips, including simulated records. */
@Composable
fun PaymentsScreen(store: FinanceStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { store.loadPayments() }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Row(
            Modifier.fillMaxWidth().height(60.dp).background(palette.chrome).padding(horizontal = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(48.dp).pressScale(motion, onClick = onBack)
                    .semantics { contentDescription = "Back" },
                contentAlignment = Alignment.Center,
            ) { Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink) }
            Column {
                Text("Payments", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text("Your receipts and test records", style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }
        }
        when (val state = store.payments) {
            PaymentsState.Idle, PaymentsState.Loading ->
                LabelMono("Loading payments…", modifier = Modifier.padding(20.dp))
            is PaymentsState.Failed -> Text(state.message, color = palette.danger, modifier = Modifier.padding(20.dp))
            is PaymentsState.Ready -> {
                if (state.items.isEmpty()) {
                    Text("No payments yet.", color = palette.muted, modifier = Modifier.padding(20.dp))
                }
                LazyColumn(
                    Modifier.fillMaxSize(),
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
                ) {
                    items(state.items, key = { it.id }) { payment ->
                        PaymentRow(payment)
                        SpacerH(8)
                    }
                    item {
                        SpacerH(4)
                        OutlineButton("Refresh", onClick = { scope.launch { store.loadPayments() } })
                    }
                }
            }
        }
    }
}

@Composable
private fun PaymentRow(payment: FanPayment) {
    val palette = LocalVesperPalette.current
    CardSurface {
        Column(Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(
                        payment.purpose.replaceFirstChar { it.uppercase() } + " · " + payment.status,
                        style = MaterialTheme.typography.titleMedium,
                        color = palette.ink,
                    )
                    Text(
                        payment.createdAt.take(10) + " · " + payment.provider,
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )
                }
                Text(money(payment.amountMinor, payment.currency), color = palette.ink)
            }
            if (payment.simulated) {
                SpacerH(6)
                Text(
                    "SIMULATED · no card was charged and no real money moved.",
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.brass,
                )
            }
        }
    }
}
