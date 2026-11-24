package com.contentplatform.app.ui.moderation

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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.contentplatform.app.dm.DetailState
import com.contentplatform.app.dm.ModerationReason
import com.contentplatform.app.dm.ModerationStore
import com.contentplatform.app.dm.QueueState
import com.contentplatform.app.dm.Report
import com.contentplatform.app.dm.explain
import com.contentplatform.app.dm.reportTime
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
import kotlinx.coroutines.launch

/**
 * The moderation queue.
 *
 * This screen is only offered to an account whose permissions include `safety.review`, and
 * the server enforces the same rule, so hiding the entry is a convenience and not the
 * security boundary. Every decision requires a note; the report's trail then shows who
 * decided, when and why.
 */
@Composable
fun ModerationScreen(store: ModerationStore, onBack: () -> Unit) {
    val palette = LocalVesperPalette.current
    val scope = rememberCoroutineScope()
    var openReportId by remember { mutableStateOf<String?>(null) }
    BackHandler(onBack = onBack)

    LaunchedEffect(Unit) { store.loadQueue() }

    Column(Modifier.fillMaxSize().background(palette.ground)) {
        Column(Modifier.fillMaxWidth().background(palette.chrome)) {
            Row(
                Modifier.fillMaxWidth().height(60.dp).padding(horizontal = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(
                    modifier = Modifier
                        .size(48.dp)
                        .pressScale(LocalVesperMotion.current, onClick = onBack)
                        .semantics { contentDescription = "Back" },
                    contentAlignment = Alignment.Center,
                ) {
                    Text("\u2190", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                }
                Column(Modifier.weight(1f)) {
                    Text("Reports", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                    Text(
                        "Safety queue",
                        style = MaterialTheme.typography.bodySmall,
                        color = palette.muted,
                    )
                }
                OutlineButton(
                    "Refresh",
                    onClick = { scope.launch { store.loadQueue() } },
                )
            }
            Divider()
        }

        when (val state = store.queue) {
            QueueState.Idle, QueueState.Loading -> QueueMessage("Loading reports")
            QueueState.Denied -> QueueMessage("This account cannot review reports.")
            is QueueState.Failed -> QueueMessage(state.failure.explain())
            is QueueState.Ready -> {
                if (state.items.isEmpty()) {
                    QueueMessage("Nothing is waiting. New reports appear here.")
                } else {
                    LazyColumn(Modifier.fillMaxSize()) {
                        items(state.items, key = { it.id }) { report ->
                            QueueRow(report) { openReportId = report.id }
                            Divider()
                        }
                    }
                }
            }
        }
    }

    val detailId = openReportId
    if (detailId != null) {
        LaunchedEffect(detailId) { store.loadDetail(detailId) }
        ReportDetailSheet(
            store = store,
            scope = scope,
            onDismiss = {
                openReportId = null
                store.clearDetail()
            },
        )
    }
}

@Composable
private fun QueueMessage(text: String) {
    val palette = LocalVesperPalette.current
    Box(Modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
        Text(text, style = MaterialTheme.typography.bodySmall, color = palette.muted)
    }
}

@Composable
private fun QueueRow(report: Report, onClick: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    Row(
        Modifier
            .fillMaxWidth()
            .pressScale(motion, onClick = onClick)
            .semantics(mergeDescendants = true) {
                contentDescription = "${ModerationReason.label(report.reason)}, ${report.status}, " +
                    report.subjectType
            }
            .padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                ModerationReason.label(report.reason),
                style = MaterialTheme.typography.titleSmall,
                color = palette.ink,
            )
            SpacerH(3)
            Text(
                "${report.subjectType} · ${report.details.ifBlank { "no extra detail" }}",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            SpacerH(3)
            LabelMono(reportTime(report.createdAtMillis))
        }
        SpacerH(0)
        StatusChip(report.status)
        Text("\u2192", modifier = Modifier.padding(start = 8.dp), style = MaterialTheme.typography.bodyMedium, color = palette.muted)
    }
}

@Composable
private fun StatusChip(status: String) {
    val tone = when (status) {
        "open" -> ChipTone.Action
        "reviewing" -> ChipTone.Pending
        "actioned" -> ChipTone.Paid
        else -> ChipTone.Neutral
    }
    Chip(status.replaceFirstChar { it.uppercase() }, tone)
}

/**
 * One report with its trail and the actions available from its current state.
 *
 * A decision needs a note of at least three characters. That is not a UI nicety: the server
 * refuses the transition without one, because a closed report with no reason is unusable on
 * appeal.
 */
@Composable
fun ReportDetailSheet(store: ModerationStore, scope: kotlinx.coroutines.CoroutineScope, onDismiss: () -> Unit) {
    val palette = LocalVesperPalette.current
    val motion = LocalVesperMotion.current
    var note by remember { mutableStateOf("") }

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
                .heightIn(max = 680.dp)
                .background(palette.surface)
                .border(1.dp, palette.rule, RoundedCornerShape(topStart = 14.dp, topEnd = 14.dp))
                .imePadding()
                .verticalScroll(rememberScrollState())
                .padding(20.dp),
        ) {
            when (val state = store.detail) {
                DetailState.Idle, DetailState.Loading -> Text(
                    "Loading report",
                    style = MaterialTheme.typography.bodyMedium,
                    color = palette.muted,
                )
                is DetailState.Failed -> Column {
                    Text("That report could not be opened", style = MaterialTheme.typography.titleMedium, color = palette.ink)
                    SpacerH(6)
                    Text(state.message, style = MaterialTheme.typography.bodySmall, color = palette.muted)
                    SpacerH(12)
                    OutlineButton("Close", onClick = onDismiss)
                }
                is DetailState.Ready -> ReportBody(
                    report = state.report,
                    note = note,
                    onNote = { note = it },
                    deciding = store.deciding,
                    error = store.decideError,
                    onDecide = { action ->
                        scope.launch { store.decide(state.report.id, action, note) }
                    },
                    onDismiss = onDismiss,
                )
            }
        }
    }
}

@Composable
private fun ReportBody(
    report: Report,
    note: String,
    onNote: (String) -> Unit,
    deciding: Boolean,
    error: String?,
    onDecide: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val palette = LocalVesperPalette.current

    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            ModerationReason.label(report.reason),
            style = MaterialTheme.typography.titleMedium,
            color = palette.ink,
            modifier = Modifier.weight(1f),
        )
        StatusChip(report.status)
    }
    SpacerH(4)
    LabelMono("${report.subjectType} · ${reportTime(report.createdAtMillis)}")
    if (report.details.isNotBlank()) {
        SpacerH(10)
        Text(report.details, style = MaterialTheme.typography.bodyMedium, color = palette.inkSecondary)
    }
    SpacerH(14)
    LabelMono("History")
    SpacerH(6)
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        report.events.forEach { event ->
            val action = if (event.action.isBlank()) "submitted" else event.action.replace('_', ' ')
            Text(
                "$action · ${event.fromStatus.ifBlank { "new" }} → ${event.toStatus}" +
                    if (event.note.isNotBlank()) "\n\"${event.note}\"" else "",
                style = MaterialTheme.typography.bodySmall,
                color = palette.muted,
            )
        }
    }
    if (report.events.isEmpty()) {
        Text("No history recorded.", style = MaterialTheme.typography.bodySmall, color = palette.muted)
    }

    SpacerH(16)
    LabelMono("Decision note (required to close)")
    SpacerH(6)
    Box(
        Modifier
            .fillMaxWidth()
            .heightIn(min = 64.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(palette.surfaceRaised)
            .border(1.dp, palette.ruleStrong, RoundedCornerShape(8.dp))
            .padding(12.dp),
    ) {
        BasicTextField(
            value = note,
            onValueChange = { if (it.length <= 2000) onNote(it) },
            textStyle = MaterialTheme.typography.bodyMedium.copy(color = palette.ink),
            cursorBrush = SolidColor(palette.action),
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Default),
            modifier = Modifier
                .fillMaxWidth()
                .semantics { contentDescription = "Decision note" },
            decorationBox = { inner ->
                Box(Modifier.fillMaxWidth()) {
                    if (note.isEmpty()) {
                        Text(
                            "Say why. This is kept on the record.",
                            style = MaterialTheme.typography.bodyMedium,
                            color = palette.muted,
                        )
                    }
                    inner()
                }
            },
        )
    }

    if (error != null) {
        SpacerH(10)
        Row(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(8.dp))
                .background(palette.danger.copy(alpha = 0.14f))
                .padding(horizontal = 10.dp, vertical = 8.dp),
        ) {
            Text(error, style = MaterialTheme.typography.bodySmall, color = palette.inkSecondary)
        }
    }

    val decided = report.closed
    val noteReady = note.trim().length >= 3
    SpacerH(14)
    when {
        !decided -> {
            ActionButton(
                label = if (deciding) "Working" else "Action taken",
                price = null,
                enabled = noteReady && !deciding,
                onClick = { onDecide("action") },
            )
            SpacerH(8)
            ActionButton(
                label = "Dismiss",
                price = null,
                enabled = noteReady && !deciding,
                onClick = { onDecide("dismiss") },
            )
            if (report.status == "open") {
                SpacerH(8)
                OutlineButton(
                    if (deciding) "Working" else "Start review",
                    onClick = { if (!deciding) onDecide("start_review") },
                )
            }
        }
        else -> {
            ActionButton(
                label = if (deciding) "Working" else "Reopen for appeal",
                price = null,
                enabled = noteReady && !deciding,
                onClick = { onDecide("reopen") },
            )
        }
    }
    SpacerH(8)
    OutlineButton("Close", onClick = onDismiss)
}
