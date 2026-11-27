package com.contentplatform.app.ui.finance

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
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
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.contentplatform.app.finance.AdminQueueState
import com.contentplatform.app.finance.FinanceStore
import com.contentplatform.app.finance.money
import com.contentplatform.app.ui.components.CardSurface
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.launch

/** Finance queue: approval reserves ledger money; settlement records an external transfer. */
@Composable
fun AdminPayoutsScreen(store: FinanceStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val scope = rememberCoroutineScope()
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { store.loadAdminQueue() }

    Column(Modifier.fillMaxSize().background(palette.ground).imePadding()) {
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
                Text("Payout review", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                Text("Finance permission required", style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }
        }
        when (val state = store.adminQueue) {
            AdminQueueState.Idle, AdminQueueState.Loading ->
                LabelMono("Loading payouts…", modifier = Modifier.padding(20.dp))
            is AdminQueueState.Failed -> Text(state.message, color = palette.danger, modifier = Modifier.padding(20.dp))
            is AdminQueueState.Ready -> {
                if (state.items.isEmpty()) {
                    Text("No payout requests.", color = palette.muted, modifier = Modifier.padding(20.dp))
                }
                LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp)) {
                    items(state.items, key = { it.id }) { payout ->
                        AdminPayoutRow(store, payout)
                        SpacerH(10)
                    }
                }
            }
        }
        store.adminNotice?.let {
            Text(it, color = palette.inkSecondary, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(16.dp))
        }
    }
}

@Composable
private fun AdminPayoutRow(store: FinanceStore, payout: com.contentplatform.app.finance.AdminPayout) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var note by remember(payout.id) { mutableStateOf("") }
    var reference by remember(payout.id) { mutableStateOf("") }

    CardSurface {
        Column(Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(money(payout.amountMinor, payout.currency), style = MaterialTheme.typography.titleMedium, color = palette.ink)
                    Text("creator " + payout.creatorId.take(8) + " · " + payout.method, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                }
                LabelMono(payout.status.uppercase())
            }
            Text("Requested " + payout.createdAt.take(10), style = MaterialTheme.typography.bodySmall, color = palette.muted)
            payout.note.takeIf { it.isNotBlank() }?.let {
                Text("Creator note: " + it, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
            }
            Text(
                "Approval reserves money in the ledger. Settlement only records that an external transfer happened; this screen cannot move money.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.brass,
            )
            SpacerH(8)
            SmallField("Decision note", note, { note = it })
            SpacerH(6)
            SmallField("External settlement reference (settle only)", reference, { reference = it })
            SpacerH(8)
            when (payout.status) {
                "requested" -> Row {
                    OutlineButton("Approve", onClick = {
                        scope.launch { store.adminDecision(payout.id, "approve", note.ifBlank { "reviewed" }, "") }
                    })
                    SpacerH(6)
                    OutlineButton("Reject", onClick = {
                        scope.launch { store.adminDecision(payout.id, "reject", note, "") }
                    })
                }
                "approved" -> OutlineButton("Record settlement", onClick = {
                    scope.launch { store.adminDecision(payout.id, "settle", note, reference) }
                })
                else -> Text("Closed: " + payout.status, style = MaterialTheme.typography.bodySmall, color = palette.muted)
            }
        }
    }
}

@Composable
private fun SmallField(label: String, value: String, onChange: (String) -> Unit) {
    val palette = LocalVesperPalette.current
    Column {
        LabelMono(label)
        Box(
            Modifier.fillMaxWidth().height(40.dp)
                .background(palette.surfaceRaised, androidx.compose.foundation.shape.RoundedCornerShape(8.dp))
                .padding(horizontal = 10.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            BasicTextField(
                value = value,
                onValueChange = onChange,
                singleLine = true,
                textStyle = MaterialTheme.typography.bodySmall.copy(color = palette.ink),
                cursorBrush = SolidColor(palette.ink),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
