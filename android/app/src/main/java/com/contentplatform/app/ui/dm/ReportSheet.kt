package com.contentplatform.app.ui.dm

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.ui.unit.dp
import com.contentplatform.app.dm.ModerationReason
import com.contentplatform.app.dm.ModerationStore
import com.contentplatform.app.dm.ReportSubmitState
import com.contentplatform.app.ui.components.ActionButton
import com.contentplatform.app.ui.components.Chip
import com.contentplatform.app.ui.components.ChipTone
import com.contentplatform.app.ui.components.Divider
import com.contentplatform.app.ui.components.LabelMono
import com.contentplatform.app.ui.components.OutlineButton
import com.contentplatform.app.ui.components.SpacerH
import com.contentplatform.app.ui.motion.LocalVesperMotion
import com.contentplatform.app.ui.motion.pressScale
import com.contentplatform.app.ui.theme.LocalVesperPalette
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * Report sheet.
 *
 * The subject is fixed by the screen that opened this: a message report names that message,
 * an account report names the creator. The sheet cannot be talked into naming something
 * else, and the server re-checks that the reporter could see the subject at all.
 *
 * A duplicate report is not an error: the server returns the open report and the sheet says
 * that it is already with the safety team, so nobody is punished for pressing twice.
 */
@Composable
fun ReportSheet(
    store: ModerationStore,
    subjectType: String,
    subjectId: String,
    subjectLabel: String,
    scope: CoroutineScope,
    onDismiss: () -> Unit,
) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    val state = store.submitState

    var reason by remember { mutableStateOf("") }
    var details by remember { mutableStateOf("") }

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
            if (state is ReportSubmitState.Done) {
                Text(
                    if (state.report.duplicate) "Already reported" else "Report received",
                    style = MaterialTheme.typography.titleMedium,
                    color = palette.ink,
                )
                SpacerH(6)
                Text(
                    if (state.report.duplicate) {
                        "This is already with the safety team. Opening it again did not " +
                            "create a second report."
                    } else {
                        "A moderator reviews reports in the order they arrive. You can follow " +
                            "the outcome from your reports."
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = palette.muted,
                )
                SpacerH(12)
                Chip(
                    text = state.report.status.replaceFirstChar { it.uppercase() },
                    tone = ChipTone.Pending,
                )
                SpacerH(8)
                LabelMono("Reference ${state.report.id}")
                SpacerH(16)
                ActionButton(label = "Done", price = null, onClick = onDismiss)
                SpacerH(8)
                OutlineButton("Close", onClick = onDismiss)
                return@Column
            }

            Text("Report $subjectLabel", style = MaterialTheme.typography.titleMedium, color = palette.ink)
            SpacerH(6)
            Text(
                "Reports go to the safety team with the exact item you are looking at. " +
                    "The person you report is not told who reported them.",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
            SpacerH(16)

            LabelMono("What is wrong")
            SpacerH(8)
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                ModerationReason.codes.forEach { code ->
                    ReasonRow(
                        label = ModerationReason.label(code),
                        selected = reason == code,
                        onClick = { reason = code },
                    )
                }
            }
            SpacerH(16)

            LabelMono("Anything else (optional)")
            SpacerH(6)
            Box(
                Modifier
                    .fillMaxWidth()
                    .heightIn(min = 84.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(palette.surfaceRaised)
                    .border(1.dp, palette.ruleStrong, RoundedCornerShape(8.dp))
                    .padding(12.dp),
            ) {
                BasicTextField(
                    value = details,
                    onValueChange = { if (it.length <= 2000) details = it },
                    textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
                    cursorBrush = SolidColor(palette.action),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Default),
                    modifier = Modifier
                        .fillMaxWidth()
                        .semantics { contentDescription = "What happened" },
                    decorationBox = { inner ->
                        Box(Modifier.fillMaxWidth()) {
                            if (details.isEmpty()) {
                                Text(
                                    "What happened, and when",
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = palette.muted,
                                )
                            }
                            inner()
                        }
                    },
                )
            }
            SpacerH(4)
            LabelMono("${details.length} / 2000")
            SpacerH(16)

            val failure = state as? ReportSubmitState.Failed
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
                label = if (state is ReportSubmitState.Sending) "Sending" else "Send report",
                price = null,
                enabled = reason.isNotBlank() && state !is ReportSubmitState.Sending,
                onClick = { scope.launch { store.submit(subjectType, subjectId, reason, details) } },
            )
            SpacerH(8)
            OutlineButton("Cancel", onClick = onDismiss)
        }
    }
}

@Composable
private fun ReasonRow(label: String, selected: Boolean, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(if (selected) palette.action.copy(alpha = 0.14f) else palette.surfaceRaised)
            .border(
                1.dp,
                if (selected) palette.action else palette.rule,
                RoundedCornerShape(8.dp),
            )
            .pressScale(motion, onClick = onClick)
            .semantics { contentDescription = "Reason $label" }
            .padding(horizontal = 12.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            label,
            style = MaterialTheme.typography.bodyMedium,
            color = if (selected) palette.ink else palette.inkSecondary,
            modifier = Modifier.weight(1f),
        )
        if (selected) Chip("Selected", ChipTone.Action)
    }
    Divider()
}
