package com.contentplatform.app.ui.dm

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
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
import com.contentplatform.app.dm.DmStore
import com.contentplatform.app.dm.MaxTipMinor
import com.contentplatform.app.dm.MaxTipNote
import com.contentplatform.app.dm.MinTipMinor
import com.contentplatform.app.dm.TipCheck
import com.contentplatform.app.dm.TipState
import com.contentplatform.app.dm.checkTip
import com.contentplatform.app.dm.money
import com.contentplatform.app.dm.parseAmountToMinor
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.DemoBanner
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.components.SpacerW
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * The tip sheet.
 *
 * A tip is the only payment whose amount the person chooses, so the amount control is the
 * centre of this screen and both bounds are shown rather than discovered by a rejection.
 *
 * The receipt distinguishes test money from real money using the server's own `simulated`
 * flag (D-063). With the fake provider installed, the receipt is a labelled demo record and
 * says so in the primary surface, not a footnote: nothing here may read as a real charge.
 */

@Composable
fun TipSheet(
    peerName: String,
    store: DmStore,
    scope: CoroutineScope,
    onDismiss: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val state = store.tipState

    var preset by remember { mutableStateOf<Long?>(500L) }
    var custom by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }

    val customMinor = remember(custom) { parseAmountToMinor(custom) }
    val amount = preset ?: customMinor ?: 0L
    val check = checkTip(amount)

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
            if (state is TipState.Done) {
                Receipt(state, onDismiss)
                return@Column
            }

            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Send a tip", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                SpacerW(8)
                LabelMono("to $peerName")
            }
            SpacerH(6)
            Text(
                "A tip is a one-off thank-you. It does not change your membership or unlock " +
                    "anything.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
            SpacerH(16)

            LabelMono("Amount")
            SpacerH(6)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf(500L, 1000L, 2500L, 5000L).forEach { value ->
                    AmountChoice(
                        label = money(value),
                        selected = preset == value,
                        onClick = {
                            preset = value
                            custom = ""
                        },
                    )
                }
            }
            SpacerH(10)
            AmountField(
                value = custom,
                onValueChange = {
                    custom = it
                    preset = null
                },
                placeholder = "Other amount, e.g. 15",
                enabled = state !is TipState.Sending,
            )
            SpacerH(6)
            LabelMono(
                text = when (check) {
                    TipCheck.BelowMinimum -> "The smallest tip is ${money(MinTipMinor)}"
                    TipCheck.AboveMaximum -> "The largest tip is ${money(MaxTipMinor)}"
                    TipCheck.Ok -> "Charged once. No subscription is created."
                },
                color = if (check == TipCheck.Ok) palette.muted else palette.danger,
            )
            SpacerH(16)

            LabelMono("Note (optional)")
            SpacerH(6)
            AmountField(
                value = note,
                onValueChange = { if (it.length <= MaxTipNote) note = it },
                placeholder = "Say what it is for",
                enabled = state !is TipState.Sending,
                singleLine = false,
            )
            SpacerH(4)
            LabelMono("${note.length} / $MaxTipNote")
            SpacerH(16)

            val failure = state as? TipState.Failed
            if (failure != null) {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(8.dp))
                        .background(palette.danger.copy(alpha = 0.14f))
                        .border(1.dp, palette.danger.copy(alpha = 0.36f), RoundedCornerShape(8.dp))
                        .padding(horizontal = 10.dp, vertical = 8.dp),
                ) {
                    Text(failure.message, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
                }
                SpacerH(12)
            }

            ActionButton(
                label = when {
                    state is TipState.Sending -> "Sending"
                    failure != null -> "Try again"
                    else -> "Send ${money(amount)}"
                },
                price = null,
                enabled = check == TipCheck.Ok && state !is TipState.Sending,
                onClick = { scope.launch { store.sendTip(amount, note) } },
            )
            SpacerH(10)
            Text(
                "If this instance is running the test payment provider, the receipt will be " +
                    "labelled TEST and no card is charged.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
            SpacerH(8)
            OutlineButton("Cancel", onClick = onDismiss)
        }
    }
}

@Composable
private fun Receipt(state: TipState.Done, onDismiss: () -> Unit) {
    val palette = LocalVesperPalette.current
    val payment = state.payment
    Text("Tip sent", style = MaterialTheme.typography.titleMedium, color = palette.ink)
    SpacerH(4)
    LabelMono("${money(payment.amountMinor, payment.currency)} · ${payment.status}")
    SpacerH(12)
    if (payment.simulated) {
        DemoBanner("Test payment: no card was charged and this receipt is not real money.")
        SpacerH(12)
    }
    if (payment.note.isNotBlank()) {
        Text(payment.note, style = MaterialTheme.typography.bodyMedium, color = palette.inkSecondary)
        SpacerH(12)
    }
    Divider()
    SpacerH(12)
    LabelMono("Reference ${payment.id}")
    SpacerH(16)
    ActionButton(label = "Done", price = null, onClick = onDismiss)
}

@Composable
private fun AmountChoice(label: String, selected: Boolean, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Box(
        Modifier
            .height(40.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(if (selected) palette.action else palette.surfaceRaised)
            .border(1.dp, if (selected) palette.action else palette.rule, RoundedCornerShape(8.dp))
            .pressScale(motion, onClick = onClick)
            .semantics { contentDescription = "Tip $label" }
            .padding(horizontal = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodyMedium,
            color = if (selected) palette.onAction else palette.ink,
        )
    }
}

@Composable
private fun AmountField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    enabled: Boolean,
    singleLine: Boolean = true,
) {
    val palette = LocalVesperPalette.current
    Box(
        Modifier
            .fillMaxWidth()
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(palette.surfaceRaised)
            .border(1.dp, palette.ruleStrong, RoundedCornerShape(8.dp))
            .padding(horizontal = 12.dp, vertical = 12.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            enabled = enabled,
            singleLine = singleLine,
            textStyle = MaterialTheme.typography.bodyMedium.copy(
                color = if (enabled) palette.ink else palette.muted,
            ),
            cursorBrush = SolidColor(palette.action),
            keyboardOptions = KeyboardOptions(
                keyboardType = if (singleLine) KeyboardType.Decimal else KeyboardType.Text,
                imeAction = if (singleLine) ImeAction.Next else ImeAction.Default,
            ),
            modifier = Modifier.fillMaxWidth(),
            decorationBox = { inner ->
                Box(Modifier.fillMaxWidth()) {
                    if (value.isEmpty()) {
                        Text(placeholder, style = MaterialTheme.typography.bodyMedium, color = palette.muted)
                    }
                    inner()
                }
            },
        )
    }
}
